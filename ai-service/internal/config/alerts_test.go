package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_AlertDefaults(t *testing.T) {
	// Act
	cfg, err := Load(lookupFrom(requiredEnv()))

	// Assert
	a := cfg.Alerts
	if err != nil || a.SlackWebhookURL != "" || a.ReplyLimit != 30 || a.ReplyLimitWindow != time.Hour || a.HandoffSpikeShare != 0.5 {
		t.Fatalf("alerts %+v err %v", a, err)
	}
}

func TestLoad_AlertSettings(t *testing.T) {
	// Arrange
	env := requiredEnv()
	env["SLACK_ALERT_WEBHOOK_URL"] = "https://hooks.slack.com/services/T0/B0/abc"
	env["ALERT_ENVIRONMENT"] = "STAGING"
	env["REPLY_LIMIT"] = "0"
	env["REPLY_LIMIT_WINDOW"] = "30m"
	env["ALERT_HANDOFF_SHARE"] = "0.7"

	// Act
	cfg, err := Load(lookupFrom(env))

	// Assert
	a := cfg.Alerts
	if err != nil || a.Environment != "STAGING" || a.ReplyLimit != 0 || a.ReplyLimitWindow != 30*time.Minute || a.HandoffSpikeShare != 0.7 {
		t.Fatalf("alerts %+v err %v", a, err)
	}
}

func TestLoad_AlertSettingsRejected(t *testing.T) {
	cases := map[string]struct{ key, val, want string }{
		"webhook not slack":  {"SLACK_ALERT_WEBHOOK_URL", "https://evil.example/services/T0/B0/zzsecret", "SLACK_ALERT_WEBHOOK_URL"},
		"webhook plain http": {"SLACK_ALERT_WEBHOOK_URL", "http://hooks.slack.com/services/T0/B0/zzsecret", "SLACK_ALERT_WEBHOOK_URL"},
		"negative limit":     {"REPLY_LIMIT", "-1", "REPLY_LIMIT"},
		"limit not a number": {"REPLY_LIMIT", "lots", "REPLY_LIMIT"},
		"window too short":   {"REPLY_LIMIT_WINDOW", "10s", "REPLY_LIMIT_WINDOW"},
		"share too low":      {"ALERT_HANDOFF_SHARE", "0.01", "ALERT_HANDOFF_SHARE"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			env := requiredEnv()
			env[c.key] = c.val

			// Act
			_, err := Load(lookupFrom(env))

			// Assert
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to name %s", err, c.want)
			}
			if strings.Contains(err.Error(), "zzsecret") {
				t.Errorf("error echoes the webhook URL: %v", err)
			}
		})
	}
}
