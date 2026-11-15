package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SlackHost is the only host a Slack incoming webhook may point at, so a
// mistyped setting cannot send alerts (or probe) anywhere else.
const SlackHost = "hooks.slack.com"

// ValidateSlackURL checks an incoming-webhook URL without echoing it: the
// URL itself is the secret.
func ValidateSlackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != SlackHost || u.User != nil || len(u.Path) < len("/services/") {
		return errors.New("must be a Slack incoming webhook (https://" + SlackHost + "/services/...)")
	}
	return nil
}

// Slack posts alerts to a Slack incoming webhook (the channel the provider
// check and backup alerts use).
type Slack struct {
	URL    string // secret: never logged
	Client *http.Client
}

// Notify posts text as one Slack message.
func (s Slack) Notify(ctx context.Context, text string) error {
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return errors.New("slack: bad webhook url") // the error would hold the URL
	}
	req.Header.Set("Content-Type", "application/json")
	c := s.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	// No redirects: the webhook answers directly, and a redirect could carry
	// the post elsewhere.
	cc := *c
	cc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := cc.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) { // drop the URL (the secret) from the error
			return fmt.Errorf("slack: %w", uerr.Err)
		}
		return errors.New("slack: request failed")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("slack: HTTP %d", res.StatusCode)
	}
	return nil
}
