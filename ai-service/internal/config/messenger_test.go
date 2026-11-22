package config

import (
	"strings"
	"testing"
)

func TestLoad_MessengerReferralsOffByDefault(t *testing.T) {
	// Act
	cfg, err := Load(lookupFrom(requiredEnv()))

	// Assert
	if err != nil || cfg.MessengerProxyEnabled() {
		t.Fatalf("proxy on by default: %v", err)
	}
}

func TestLoad_MessengerReferralsOn(t *testing.T) {
	// Arrange
	env := requiredEnv()
	env["MESSENGER_APP_SECRET"] = "app-secret"
	env["CHATWOOT_MESSENGER_URL"] = "http://rails:3000/bot"

	// Act
	cfg, err := Load(lookupFrom(env))

	// Assert
	if err != nil || !cfg.MessengerProxyEnabled() || cfg.ChatwootMessengerURL.Host != "rails:3000" || cfg.ChatwootMessengerURL.Path != "/bot" {
		t.Fatalf("cfg url %v err %v", cfg.ChatwootMessengerURL, err)
	}
}

func TestLoad_MessengerReferralsRejectsPartialOrBadSettings(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"secret without url": {map[string]string{"MESSENGER_APP_SECRET": "s"}, "required together"},
		"url without secret": {map[string]string{"CHATWOOT_MESSENGER_URL": "http://rails:3000/bot"}, "required together"},
		"relative url":       {map[string]string{"MESSENGER_APP_SECRET": "s", "CHATWOOT_MESSENGER_URL": "/bot"}, "http or https"},
		"other scheme":       {map[string]string{"MESSENGER_APP_SECRET": "s", "CHATWOOT_MESSENGER_URL": "ftp://rails/bot"}, "http or https"},
		"url with userinfo":  {map[string]string{"MESSENGER_APP_SECRET": "s", "CHATWOOT_MESSENGER_URL": "http://u:p@rails/bot"}, "http or https"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			env := requiredEnv()
			for k, v := range c.env {
				env[k] = v
			}

			// Act
			_, err := Load(lookupFrom(env))

			// Assert
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
		})
	}
}

func TestLoad_MessengerSecretIsTrimmed(t *testing.T) {
	// Arrange
	env := requiredEnv()
	env["MESSENGER_APP_SECRET"] = "  app-secret\n"
	env["CHATWOOT_MESSENGER_URL"] = "http://rails:3000/bot"

	// Act
	cfg, err := Load(lookupFrom(env))

	// Assert
	if err != nil || cfg.MessengerAppSecret != "app-secret" {
		t.Fatalf("secret %q err %v", cfg.MessengerAppSecret, err)
	}
}
