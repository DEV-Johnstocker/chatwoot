package messenger

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RelayPath is where Meta sends the Messenger app's Page webhook once its
// callback URL points at ai-service instead of Chatwoot's /bot.
const RelayPath = "/webhooks/messenger"

// MaxBodyBytes caps one delivery. Meta batches events, but a batch is far
// smaller than this.
const MaxBodyBytes = 1 << 20

// Signature errors.
var (
	ErrMissingSignature = errors.New("missing X-Hub-Signature-256")
	ErrBadSignature     = errors.New("bad X-Hub-Signature-256")
)

// Relay receives Meta's Messenger webhook, keeps any referral, and passes the
// delivery to Chatwoot byte for byte with Meta's signature headers, so
// Chatwoot checks and handles it exactly as if Meta had sent it directly.
// Chatwoot's answer goes back to Meta, so a Chatwoot failure still makes Meta
// retry.
type Relay struct {
	Target    string // Chatwoot's Messenger endpoint, e.g. http://rails:3000/bot
	AppSecret string // the Messenger app's secret: checks X-Hub-Signature-256
	Pending   *Pending
	Client    *http.Client // nil: a client with a 20 s timeout and no redirects
	Logger    *slog.Logger
}

// Routes registers the relay on mux.
func (h *Relay) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+RelayPath, h.handshake)
	mux.HandleFunc("POST "+RelayPath, h.receive)
}

// handshake passes Meta's subscription check to Chatwoot, which owns the
// verify token; ai-service never sees or stores it.
func (h *Relay) handshake(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for _, k := range []string{"hub.mode", "hub.verify_token", "hub.challenge"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	target := h.Target
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "bad target", http.StatusInternalServerError)
		return
	}
	h.forward(w, req)
}

// receive checks the signature, keeps referrals, then relays.
func (h *Relay) receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := VerifySignature(h.AppSecret, r.Header.Get("X-Hub-Signature-256"), body); err != nil {
		// Debug, not Warn: anyone can POST here, and that must not flood the logs.
		h.Logger.Debug("rejected messenger webhook", "error", err, "remote_addr", r.RemoteAddr)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	for _, s := range ParseReferrals(body) {
		h.Pending.Put(s.PSID, s.Referral)
		h.Logger.Info("messenger referral kept", "page_id", s.PageID, "source", s.Referral.Source, "ad_id", s.Referral.AdID)
	}
	// Chatwoot must get the delivery even if Meta hangs up first.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.Target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "bad target", http.StatusInternalServerError)
		return
	}
	for _, k := range []string{"Content-Type", "X-Hub-Signature", "X-Hub-Signature-256"} {
		if v := r.Header.Get(k); v != "" {
			req.Header.Set(k, v)
		}
	}
	h.forward(w, req)
}

// forward sends req to Chatwoot and copies its status and a short body back.
// If Chatwoot cannot be reached, Meta gets 502 and retries later.
func (h *Relay) forward(w http.ResponseWriter, req *http.Request) {
	resp, err := h.client().Do(req)
	if err != nil {
		h.Logger.Error("messenger relay to chatwoot failed", "host", req.URL.Host, "error", err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		h.Logger.Warn("chatwoot refused relayed messenger webhook", "host", req.URL.Host, "status", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(out)
}

func (h *Relay) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// VerifySignature checks Meta's X-Hub-Signature-256 header (sha256=<hex>
// HMAC of the raw body with the app secret). An empty secret never verifies.
func VerifySignature(appSecret, header string, body []byte) error {
	if appSecret == "" || header == "" {
		return ErrMissingSignature
	}
	got, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return ErrBadSignature
	}
	gotBytes, err := hex.DecodeString(got)
	if err != nil {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	if !hmac.Equal(gotBytes, mac.Sum(nil)) {
		return ErrBadSignature
	}
	return nil
}
