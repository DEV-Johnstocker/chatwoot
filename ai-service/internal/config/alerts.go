package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/alerts"
)

// Alerts and the per-customer reply limit.
type Alerts struct {
	// SlackWebhookURL posts the bot's own alerts (failed sends, model errors,
	// a full queue, hand-off spikes, reply limits) to Slack. Empty: alerts
	// show on /admin and in the log only. Secret.
	SlackWebhookURL string
	// Environment names this deployment in every alert, e.g. STAGING.
	Environment string
	// HandoffSpikeShare is the share of answered messages handed to staff in
	// an hour that raises the hand-off alert (0.1 to 1).
	HandoffSpikeShare float64
	// ReplyLimit caps bot replies per customer within ReplyLimitWindow; past
	// it the chat goes to staff. 0 turns the limit off.
	ReplyLimit       int
	ReplyLimitWindow time.Duration
}

// Defaults for the reply limit: generous for a real buyer (replies to a
// burst of quick messages count once), low enough that a flood stops soon.
const (
	DefaultReplyLimit       = 30
	DefaultReplyLimitWindow = time.Hour
)

func loadAlerts(cfg *Config, get func(key, def string) string, errs []error) []error {
	a := &cfg.Alerts
	a.SlackWebhookURL = get("SLACK_ALERT_WEBHOOK_URL", "")
	a.Environment = get("ALERT_ENVIRONMENT", "")
	if a.SlackWebhookURL != "" {
		if err := alerts.ValidateSlackURL(a.SlackWebhookURL); err != nil {
			errs = append(errs, fmt.Errorf("SLACK_ALERT_WEBHOOK_URL %w", err))
		}
	}
	var err error
	if a.HandoffSpikeShare, err = strconv.ParseFloat(get("ALERT_HANDOFF_SHARE", "0.5"), 64); err != nil || a.HandoffSpikeShare < 0.1 || a.HandoffSpikeShare > 1 {
		errs = append(errs, errors.New("ALERT_HANDOFF_SHARE must be a number from 0.1 to 1"))
	}
	if a.ReplyLimit, err = strconv.Atoi(get("REPLY_LIMIT", strconv.Itoa(DefaultReplyLimit))); err != nil || a.ReplyLimit < 0 || a.ReplyLimit > 1000 {
		errs = append(errs, errors.New("REPLY_LIMIT must be 0 (off) or a whole number up to 1000"))
	}
	if a.ReplyLimitWindow, err = time.ParseDuration(get("REPLY_LIMIT_WINDOW", DefaultReplyLimitWindow.String())); err != nil ||
		a.ReplyLimitWindow < time.Minute || a.ReplyLimitWindow > 24*time.Hour {
		errs = append(errs, errors.New("REPLY_LIMIT_WINDOW must be a duration from 1m to 24h"))
	}
	return errs
}
