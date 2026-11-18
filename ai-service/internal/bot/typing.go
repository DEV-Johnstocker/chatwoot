package bot

import (
	"context"
	"time"
	"unicode/utf8"
)

// The bot keeps "typing…" on for about as long as a person would take to type
// its reply, so a long answer does not appear the instant a short question
// was sent. Issue: DEV-Johnstocker/chatwoot#1

// typingTime is how long typing reply takes at speed characters a second,
// at most limit. Speed 0 turns the wait off.
func typingTime(reply string, speed int, limit time.Duration) time.Duration {
	if speed <= 0 {
		return 0
	}
	d := time.Duration(utf8.RuneCountInString(reply)) * time.Second / time.Duration(speed)
	return min(d, limit)
}

// typingPause waits until the reply's typing time has passed since started,
// when the typing indicator came on: the time the model spent writing the
// answer counts, so a slow answer is sent at once. on is the account's
// "human-like typing time" switch (business profile); off, the reply goes
// out at once, REPLY_DELAY after the customer's last message.
func (b *Bot) typingPause(ctx context.Context, on bool, started time.Time, reply string) {
	if !on {
		return
	}
	left := typingTime(reply, b.opts.TypingSpeed, b.opts.TypingMax) - time.Since(started)
	if left <= 0 {
		return
	}
	b.wait(ctx, left)
}

// sleepCtx waits d, or less if ctx ends first.
func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
