package comments

import (
	"context"
	"fmt"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// Division of labour (owner, 2026-11-13): Jev, a calibrated classifier,
// sorts each comment; the LLM, which can write, drafts the reply; code holds
// the hard rules. See comments.contract.md, "Who does what".

// Sorting is a Sorter's answer: the most likely intent, how sure it is,
// and the model that answered.
type Sorting struct {
	Intent Intent
	P      float64
	Model  string
}

// Sorter chooses a comment's intent before the drafter sees it.
type Sorter interface {
	Sort(ctx context.Context, c Comment, p profile.Profile) (Sorting, error)
}

// SkipOtherAt is how sure the Sorter must be that a comment is "other"
// before it is skipped without a draft. Below it the drafter decides, so a
// lead is never dropped on a guess. On the 95 mock cases Jev called 7 of 9
// "other" comments at 0.90 or more and no real lead "other".
const SkipOtherAt = 0.90

// sortPause is how long sorting stops after a failed Sorter call.
const sortPause = time.Minute

// sortFirst asks the Sorter for the comment's intent. A sure "other" is
// stored as ignored (skipped, and saved unless the store failed); any other
// intent is handed to the drafter in SortedAs. An unsure "other" or a failed
// call leaves c unchanged for the drafter to sort.
func (w *Worker) sortFirst(ctx context.Context, c Comment) (out Comment, skipped, saved bool) {
	p, err := w.profileFor(ctx, c)
	if err != nil {
		w.Logger.Warn("sort page comment: load profile", "account_id", c.AccountID, "comment_id", c.CommentID, "error", err)
		return c, false, false
	}
	sctx, cancel := context.WithTimeout(ctx, w.Timeout)
	s, err := w.Sorter.Sort(sctx, c, p)
	cancel()
	if err != nil {
		w.sortPauseUntil = w.now().Add(sortPause)
		w.Logger.Warn("sort page comment: the drafter sorts until the pause ends", "account_id", c.AccountID, "comment_id", c.CommentID,
			"pause_seconds", int(sortPause.Seconds()), "error", err)
		return c, false, false
	}
	if s.Intent != IntentOther {
		c.SortedAs = s.Intent
		return c, false, false
	}
	if s.P < SkipOtherAt {
		return c, false, false
	}
	who := s.Model
	if who == "" {
		who = "the sorter"
	}
	reason := fmt.Sprintf("Sorted by %s: nothing to answer (%.0f%% sure).", who, 100*s.P)
	if err := w.Store.Score(ctx, c.ID, IntentOther, reason, ""); err != nil {
		w.Logger.Error("save comment score", "account_id", c.AccountID, "comment_id", c.CommentID, "error", err)
		return c, true, false
	}
	w.Logger.Info("page comment scored", "account_id", c.AccountID, "comment_id", c.CommentID, "intent", string(IntentOther),
		"sorted_by", who, "p", s.P)
	return c, true, true
}

// profileFor is the business profile for the comment's account, without the
// bank account (comments never quote it), or an empty one when there is no
// profile store.
func (w *Worker) profileFor(ctx context.Context, c Comment) (profile.Profile, error) {
	if w.Profiles == nil {
		return profile.Profile{}, nil
	}
	p, err := w.Profiles.Get(ctx, c.AccountID)
	return withoutBank(p), err
}
