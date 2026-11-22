package referral

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/comments"
)

// WebhookPath is where Facebook sends Messenger events once the Meta app's
// callback points at ai-service instead of Chatwoot's /bot. It is reachable
// from the internet without a sign-in; the signature is the authentication.
const WebhookPath = "/webhooks/messenger"

// MaxBodyBytes caps a delivery. Messenger batches events, but a delivery is
// far below this.
const MaxBodyBytes = 1 << 20

// maxUpstreamReply caps what is copied back from Chatwoot to Facebook.
const maxUpstreamReply = 64 << 10

// saveTimeout bounds the database write after a delivery went through.
const saveTimeout = 3 * time.Second

// Store keeps referrals. Add skips ones already saved and returns how many
// were new.
type Store interface {
	Add(ctx context.Context, rs []Referral) (int, error)
}

// Proxy sits in front of Chatwoot's Messenger webhook. It checks Facebook's
// signature, forwards the request unchanged (the same bytes and signature
// headers, so Chatwoot's own check still passes), and only then saves any
// referral. Delivery comes first: a failed save is logged, never returned.
type Proxy struct {
	AppSecret string   // the Messenger app's secret (the one Chatwoot uses)
	Upstream  *url.URL // Chatwoot's Messenger endpoint, e.g. http://rails:3000/bot
	Client    *http.Client
	Timeout   time.Duration // for the forward; well under Facebook's own
	Store     Store
	Logger    *slog.Logger
}

// Routes registers the webhook on mux.
func (p *Proxy) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+WebhookPath, p.handshake)
	mux.HandleFunc("POST "+WebhookPath, p.receive)
}

// challengeRe bounds the challenge passed on and echoed back: Facebook sends a
// number, so anything else is refused before it reaches Chatwoot.
var challengeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// handshake passes Facebook's subscription check through to Chatwoot, which
// owns the verify token. Only a real check goes through, with only its three
// parameters, and the echo always goes back as plain text.
func (p *Proxy) handshake(w http.ResponseWriter, r *http.Request) {
	in := r.URL.Query()
	if in.Get("hub.mode") != "subscribe" || !challengeRe.MatchString(in.Get("hub.challenge")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	target := *p.Upstream
	target.RawQuery = url.Values{
		"hub.mode":         {"subscribe"},
		"hub.verify_token": {in.Get("hub.verify_token")},
		"hub.challenge":    {in.Get("hub.challenge")},
	}.Encode()
	ctx, cancel := context.WithTimeout(r.Context(), p.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	p.relay(w, req, "handshake")
}

func (p *Proxy) receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := comments.VerifySignature(p.AppSecret, r.Header.Get("X-Hub-Signature-256"), body); err != nil {
		// Debug, not Warn: anyone can POST here, and that must not flood the logs.
		p.Logger.Debug("rejected messenger webhook", "error", err, "remote_addr", r.RemoteAddr)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), p.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Upstream.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	for _, h := range []string{"Content-Type", "X-Hub-Signature", "X-Hub-Signature-256", "User-Agent"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	if !p.relay(w, req, "event") {
		return
	}
	p.save(r.Context(), body)
}

// relay sends req to Chatwoot and copies its status and reply back. It
// reports whether Chatwoot accepted the delivery (2xx). Chatwoot runs with
// FORCE_SSL in production, so the forward says the original was https.
func (p *Proxy) relay(w http.ResponseWriter, req *http.Request, kind string) bool {
	req.Header.Set("X-Forwarded-Proto", "https")
	res, err := p.client().Do(req)
	if err != nil {
		// Facebook retries a failed delivery; the retry saves the referral.
		// The error is unwrapped: a *url.Error carries the full URL, and a
		// handshake's URL holds the verify token.
		level := slog.LevelWarn
		if kind == "handshake" {
			level = slog.LevelDebug // anyone can send a check; no log flood
		}
		p.Logger.Log(req.Context(), level, "messenger forward failed", "kind", kind, "host", p.Upstream.Host, "error", errorWithoutURL(err))
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return false
	}
	defer res.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(res.Body, maxUpstreamReply))
	if kind == "handshake" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else if ct := res.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(reply)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		p.Logger.Warn("chatwoot refused messenger event", "kind", kind, "status", res.StatusCode)
		return false
	}
	return true
}

// save keeps the referrals in a delivery Chatwoot accepted. It runs even if
// Facebook hangs up, and never changes the reply.
func (p *Proxy) save(parent context.Context, body []byte) {
	refs, err := Parse(body)
	if err != nil {
		p.Logger.Warn("messenger event not parsed for referrals", "error", err)
		return
	}
	if len(refs) == 0 || p.Store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), saveTimeout)
	defer cancel()
	added, err := p.Store.Add(ctx, refs)
	if err != nil {
		p.Logger.Warn("save messenger referrals failed", "count", len(refs), "error", err)
		return
	}
	for _, r := range refs {
		p.Logger.Info("messenger referral", "page_id", r.PageID, "source", r.Source, "ad", r.AdID != "")
	}
	p.Logger.Debug("messenger referrals saved", "new", added, "seen", len(refs))
}

// errorWithoutURL drops the request URL from a client error.
func errorWithoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func (p *Proxy) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 5 * time.Second
}

func (p *Proxy) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return noRedirects
}

// noRedirects never follows a redirect: following one would turn the POST
// into a GET and lose the event. Chatwoot's redirect goes back to Facebook,
// which retries.
var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}}
