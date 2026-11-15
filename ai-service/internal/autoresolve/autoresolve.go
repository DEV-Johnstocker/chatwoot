// Package autoresolve closes conversations the bot finished: the bot owns
// them (Chatwoot status "pending"), its reply was the last message, the
// customer is not waiting for anything, and nothing happened for a while.
//
// Resolving a bot-only conversation makes Chatwoot record a "bot resolved"
// event (its Bot report) and send the inbox's CSAT survey when that is
// switched on. A new customer message reopens the conversation as pending,
// so the bot simply carries on.
package autoresolve

import (
	"context"
	"log/slog"
	"time"
)

// Conversation is the part of a Chatwoot conversation the decision needs.
type Conversation struct {
	ID           int64
	Status       string
	Muted        bool
	LastActivity time.Time
	WaitingSince time.Time // zero when no customer message waits for a reply
	LastMessage  *Message  // latest non-activity message; nil when there is none
}

// Message is the latest non-activity message of a conversation.
type Message struct {
	Outgoing bool
	Private  bool
}

// Finished reports whether the bot is done with c at now: the bot owns it,
// the last message is a public reply, the customer is not waiting, and it
// has been quiet for at least after.
func Finished(c Conversation, now time.Time, after time.Duration) bool {
	if c.Status != "pending" || c.Muted || !c.WaitingSince.IsZero() {
		return false
	}
	if c.LastMessage == nil || !c.LastMessage.Outgoing || c.LastMessage.Private {
		return false
	}
	return !c.LastActivity.After(now.Add(-after))
}

// Lister finds the accounts and their quiet bot-owned conversations.
type Lister interface {
	Accounts(ctx context.Context) ([]int64, error)
	// IdlePending returns pending conversations with no activity after before.
	IdlePending(ctx context.Context, accountID int64, before time.Time) ([]Conversation, error)
}

// Chat reads a conversation's live status and resolves it.
type Chat interface {
	ConversationStatus(ctx context.Context, accountID, conversationID int64) (string, error)
	Resolve(ctx context.Context, accountID, conversationID int64) error
}

// Sweeper resolves finished conversations on every Sweep.
type Sweeper struct {
	List   Lister
	Chat   Chat
	After  time.Duration // quiet time before a chat counts as finished; 0 = off
	Now    func() time.Time
	Logger *slog.Logger
}

// Sweep resolves every finished conversation once and returns how many it
// resolved. Errors are logged; the next sweep tries again.
func (s *Sweeper) Sweep(ctx context.Context) int {
	if s.After <= 0 {
		return 0
	}
	now := s.Now()
	accounts, err := s.List.Accounts(ctx)
	if err != nil {
		s.Logger.Warn("auto-resolve: list accounts", "error", err)
		return 0
	}
	total := 0
	for _, acct := range accounts {
		total += s.sweepAccount(ctx, acct, now)
	}
	if total > 0 {
		s.Logger.Info("auto-resolve: resolved finished chats", "count", total)
	}
	return total
}

func (s *Sweeper) sweepAccount(ctx context.Context, acct int64, now time.Time) int {
	convs, err := s.List.IdlePending(ctx, acct, now.Add(-s.After))
	if err != nil {
		s.Logger.Warn("auto-resolve: list conversations", "account_id", acct, "error", err)
		return 0
	}
	n := 0
	for _, c := range convs {
		if ctx.Err() != nil {
			return n
		}
		if !Finished(c, now, s.After) {
			continue
		}
		if s.resolve(ctx, acct, c.ID) {
			n++
		}
	}
	return n
}

// resolve re-reads the status first: staff may have taken the chat since it
// was listed.
func (s *Sweeper) resolve(ctx context.Context, acct, conv int64) bool {
	status, err := s.Chat.ConversationStatus(ctx, acct, conv)
	if err != nil || status != "pending" {
		if err != nil {
			s.Logger.Warn("auto-resolve: read status", "account_id", acct, "conversation_id", conv, "error", err)
		}
		return false
	}
	if err := s.Chat.Resolve(ctx, acct, conv); err != nil {
		s.Logger.Warn("auto-resolve: resolve", "account_id", acct, "conversation_id", conv, "error", err)
		return false
	}
	return true
}

// Run sweeps every interval until ctx ends.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}
