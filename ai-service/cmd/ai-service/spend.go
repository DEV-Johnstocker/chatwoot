package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/admin"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/config"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/spend"
)

// AI spend wiring: usage writer, cap monitor and the /admin/spend board.
// Issue: DEV-Johnstocker/chatwoot#1

// spendCheckEvery is how often the caps are checked for alerts.
const spendCheckEvery = 5 * time.Minute

// startSpend starts the usage writer and the cap monitor, or returns nils
// without a database.
func startSpend(cfg config.Config, db *profile.Postgres, logger *slog.Logger) (*spend.Writer, *spend.Monitor) {
	if db == nil {
		logger.Info("ai spend off: it needs DATABASE_URL")
		return nil, nil
	}
	log := logger.With("component", "spend")
	store := &spend.Postgres{Pool: db.Pool}
	w := spend.NewWriter(store, log, 2048)
	w.Start()
	m := &spend.Monitor{Store: store, Prices: spend.Prices, Loc: cfg.Location, Logger: log,
		Label: hostOf(cfg.ChatwootPublicURL)}
	if base := adminBaseURL(cfg.MCPPublicURL); base != "" {
		m.PageURL = base + "/admin/spend"
	}
	if cfg.SpendSlackWebhookURL != "" {
		slack, err := spend.NewSlack(cfg.SpendSlackWebhookURL)
		if err != nil {
			log.Warn("ai spend Slack alerts off", "error", err)
		} else {
			m.Notify = slack
		}
	}
	go m.Run(context.Background(), spendCheckEvery)
	log.Info("ai spend on", "slack_alerts", m.Notify != nil, "check_every", spendCheckEvery.String())
	return w, m
}

// spendRecorder is the writer as a Recorder, or a nil interface when off.
func spendRecorder(w *spend.Writer) spend.Recorder {
	if w == nil {
		return nil
	}
	return w
}

// metered counts c's calls for purpose; account is used when the call's
// context names none. Without a recorder c is returned as it is.
func metered(c llm.Completer, rec spend.Recorder, purpose string, account int64) llm.Completer {
	if rec == nil {
		return c
	}
	return &spend.LLM{Next: c, Purpose: purpose, Rec: rec, Account: account}
}

// adminSpend is the /admin/spend board, or nil (page off) without a database.
func adminSpend(db *profile.Postgres, m *spend.Monitor) admin.SpendBoard {
	if db == nil || m == nil {
		return nil
	}
	return &spend.Board{Store: m.Store, Monitor: m, Prices: spend.Prices, Loc: m.Loc}
}
