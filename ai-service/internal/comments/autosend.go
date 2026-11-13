package comments

// Auto-send: the owner chose (2026-11-14) to let Jev send the private reply
// on its own for the safest comments, instead of waiting for a click. Every
// other comment still waits on /admin/comments. Issue: DEV-Johnstocker/chatwoot#1

import (
	"context"
	"time"
)

// AutoSentBy is DecidedBy on a reply auto-send sent.
const AutoSentBy = "auto-send"

// ChatChecker reports whether a commenter may have chatted with the Page
// recently (recentchat.Chats). Unknown counts as yes.
type ChatChecker interface {
	MaybeChatted(accountID int64, id string, now time.Time) bool
}

// Why auto-send left a comment for the owner, for the log.
const (
	HoldOff         = "auto-send off"
	HoldNotWaiting  = "no longer waiting"
	HoldIntent      = "only leads and questions are sent automatically"
	HoldNoReview    = "not reviewed"
	HoldReviewer    = "the reviewer held it"
	HoldExpired     = "reply window passed"
	HoldEmptyDraft  = "no draft"
	HoldMaybeChats  = "may be chatting already"
	HoldRepeatReply = "this person already got a private reply"
	HoldIntentSplit = "the sorter and the reviewer read the comment differently"
	HoldRateLimit   = "hourly auto-send limit reached"
)

// MaxAutoSendsPerHour caps automatic replies per account. A flood of
// crafted comments then costs at most this many unreviewed messages an hour;
// the rest wait for the owner.
const MaxAutoSendsPerHour = 20

// holdReason is "" when the comment may be sent automatically, or the
// first rule that keeps it for the owner.
func (q *Queue) holdReason(ctx context.Context, c Comment) string {
	now := q.now()
	switch {
	case !q.AutoSend || q.Sender == nil || q.Chats == nil:
		return HoldOff
	case c.Status != StatusWaiting:
		return HoldNotWaiting
	case c.Intent != IntentLead && c.Intent != IntentQuestion:
		return HoldIntent
	case c.Review == nil:
		return HoldNoReview
	case !c.Review.Pass:
		return HoldReviewer
	case c.Review.Intent != "" && c.Review.Intent != c.Intent:
		// Two independent reads must agree; a comment written to steer one
		// model rarely moves both the same way.
		return HoldIntentSplit
	case c.Expired(now):
		return HoldExpired
	case c.Draft == "":
		return HoldEmptyDraft
	case q.Chats.MaybeChatted(c.AccountID, c.FromID, now):
		return HoldMaybeChats
	}
	// A second comment from someone already answered privately is better
	// read by a person than answered twice by the machine.
	last, err := q.Store.LastReplies(ctx, c.AccountID, []string{c.FromID})
	if err != nil || !last[c.FromID].IsZero() {
		return HoldRepeatReply
	}
	// The owner may be sending to the same person this second.
	if busy, err := q.Store.SendingTo(ctx, c.AccountID, c.FromID); err != nil || busy {
		return HoldRepeatReply
	}
	if !q.underHourlyLimit(c.AccountID, now) {
		return HoldRateLimit
	}
	return ""
}

// underHourlyLimit reports whether the account sent fewer than
// MaxAutoSendsPerHour automatic replies in the hour before now.
func (q *Queue) underHourlyLimit(accountID int64, now time.Time) bool {
	q.autoMu.Lock()
	defer q.autoMu.Unlock()
	kept := q.autoSent[accountID][:0]
	for _, t := range q.autoSent[accountID] {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	if q.autoSent == nil {
		q.autoSent = map[int64][]time.Time{}
	}
	q.autoSent[accountID] = kept
	return len(kept) < MaxAutoSendsPerHour
}

// countAutoSend records one automatic reply for the hourly limit.
func (q *Queue) countAutoSend(accountID int64, at time.Time) {
	q.autoMu.Lock()
	defer q.autoMu.Unlock()
	if q.autoSent == nil {
		q.autoSent = map[int64][]time.Time{}
	}
	q.autoSent[accountID] = append(q.autoSent[accountID], at)
}

// TryAutoSend sends the stored draft of a just-reviewed comment when every
// auto-send rule allows it. It returns whether it sent, and why not.
func (q *Queue) TryAutoSend(ctx context.Context, accountID, id int64) (bool, string, error) {
	c, err := q.Store.Get(ctx, accountID, id)
	if err != nil {
		return false, "", err
	}
	if why := q.holdReason(ctx, c); why != "" {
		return false, why, nil
	}
	q.countAutoSend(accountID, q.now()) // counted before the send, so a failed one still uses a slot
	if err := q.Send(ctx, accountID, id, c.Version, c.Draft, AutoSentBy); err != nil {
		return false, "", err
	}
	return true, "", nil
}

// AutoSending reports whether auto-send is on and able to send.
func (q *Queue) AutoSending() bool { return q.AutoSend && q.Sender != nil && q.Chats != nil }

// AutoSendHold is why auto-send left a waiting comment for the owner, or ""
// when auto-send is off. Shown on /admin/comments.
func (q *Queue) AutoSendHold(ctx context.Context, c Comment) string {
	if !q.AutoSending() {
		return ""
	}
	return q.holdReason(ctx, c)
}
