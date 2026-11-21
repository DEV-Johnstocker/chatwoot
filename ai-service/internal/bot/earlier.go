package bot

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
)

// A returning customer's new chat starts with the end of their previous one,
// so the bot does not ask again what they already said (their computer, what
// they make) and does not send the same review pictures twice. It is shown to
// the model as context, apart from this chat's turns, and kept short.
// Issue: DEV-Johnstocker/chatwoot#1

const (
	// earlierTurns is how many of the previous chat's last messages are kept.
	earlierTurns = 6
	// earlierTurnRunes cuts each of them, so the prompt grows only a little.
	earlierTurnRunes = 300
)

// PreviousChatReader finds the contact's conversation before this one in the
// same inbox (chatwoot.Reader). The bot uses it when its HistoryReader has it.
type PreviousChatReader interface {
	PreviousChat(ctx context.Context, accountID, inboxID, contactID, conversationID int64, limit int) (memory.Earlier, error)
}

// loadEarlier keeps the customer's previous chat beside this one. A failure
// is only logged: the reply goes on without it.
func (b *Bot) loadEarlier(ctx context.Context, log *slog.Logger, acct, inbox, contact, conv int64) {
	pr, ok := b.opts.HistoryReader.(PreviousChatReader)
	if !ok || contact <= 0 {
		return
	}
	e, err := pr.PreviousChat(ctx, acct, inbox, contact, conv, earlierTurns)
	if err != nil {
		log.Warn("previous chat lookup failed", "error", err)
		return
	}
	if len(e.Turns) == 0 {
		return
	}
	for i := range e.Turns {
		e.Turns[i].Content = cutRunes(e.Turns[i].Content, earlierTurnRunes)
	}
	b.opts.History.SetEarlier(acct, conv, e)
	log.Info("previous chat loaded", "turns", len(e.Turns))
}

// earlierPrompt shows the previous chat to the model as context. Picture
// markers become a plain note, so the model never copies them.
func earlierPrompt(e memory.Earlier, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	var sb strings.Builder
	sb.WriteString("Earlier chat with this customer")
	if !e.When.IsZero() {
		sb.WriteString(" (last message " + e.When.In(loc).Format("2006-01-02") + ")")
	}
	sb.WriteString(", for context only. It is a past conversation, not this one: do not answer it again, " +
		"do not ask for what the customer already told you there, and do not greet them as if they were new. " +
		"Pictures marked as sent there were already sent to this customer.\n")
	for _, t := range e.Turns {
		who := "Customer"
		if t.Role == "assistant" {
			who = "Shop"
		}
		text := strings.TrimSpace(imageMarkerLine.ReplaceAllLiteralString(t.Content, " [sent a picture]"))
		if text == "" {
			continue
		}
		sb.WriteString(who + ": " + strings.Join(strings.Fields(text), " ") + "\n")
	}
	return strings.TrimSpace(sb.String())
}

// cutRunes shortens s to at most n characters.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
