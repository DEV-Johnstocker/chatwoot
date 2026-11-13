package comments

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeChats struct{ chatted bool }

func (f fakeChats) MaybeChatted(int64, string, time.Time) bool { return f.chatted }

// reviewedComment stores a waiting lead with a review and returns it.
func reviewedComment(t *testing.T, store Store, pass bool) Comment {
	t.Helper()
	c := waitingComment(t, store, now.Add(-time.Hour))
	if err := store.SaveReview(context.Background(), c.ID, Review{Pass: pass, Model: "jev"}); err != nil {
		t.Fatal(err)
	}
	return c
}

func autoQueue(store Store, sender *fakeSender) *Queue {
	return &Queue{Store: store, Sender: sender, AutoSend: true, Chats: fakeChats{}, Now: func() time.Time { return now }}
}

func TestTryAutoSend_SendsAPassedLeadOnceAndMarksIt(t *testing.T) {
	// Arrange
	ctx := context.Background()
	store, sender := &Memory{}, &fakeSender{}
	q := autoQueue(store, sender)
	c := reviewedComment(t, store, true)

	// Act
	sent, why, err := q.TryAutoSend(ctx, 2, c.ID)
	again, _, _ := q.TryAutoSend(ctx, 2, c.ID)

	// Assert
	got, _ := store.Get(ctx, 2, c.ID)
	if err != nil || !sent || why != "" || again || len(sender.calls) != 1 || sender.calls[0] != c.CommentID+"|draft" {
		t.Fatalf("sent %v why %q err %v again %v calls %v", sent, why, err, again, sender.calls)
	}
	if got.Status != StatusSent || got.DecidedBy != AutoSentBy {
		t.Fatalf("row %+v", got)
	}
}

func TestTryAutoSend_HoldsForTheOwner(t *testing.T) {
	cases := map[string]struct {
		setup func(q *Queue, store Store, c *Comment)
		want  string
	}{
		"switch off":         {func(q *Queue, _ Store, _ *Comment) { q.AutoSend = false }, HoldOff},
		"no page token":      {func(q *Queue, _ Store, _ *Comment) { q.Sender = nil }, HoldOff},
		"no chat record":     {func(q *Queue, _ Store, _ *Comment) { q.Chats = nil }, HoldOff},
		"reviewer held it":   {func(_ *Queue, s Store, c *Comment) { _ = s.SaveReview(context.Background(), c.ID, Review{Pass: false}) }, HoldReviewer},
		"may be chatting":    {func(q *Queue, _ Store, _ *Comment) { q.Chats = fakeChats{chatted: true} }, HoldMaybeChats},
		"window passed":      {func(q *Queue, _ Store, _ *Comment) { q.Now = func() time.Time { return now.Add(8 * 24 * time.Hour) } }, HoldExpired},
		"complaint":          {func(_ *Queue, s Store, c *Comment) { setIntent(s, c.ID, IntentComplaint) }, HoldIntent},
		"already answered":   {func(_ *Queue, s Store, _ *Comment) { sentEarlier(t, s) }, HoldRepeatReply},
		"owner dismissed it": {func(_ *Queue, s Store, c *Comment) { _ = s.Dismiss(context.Background(), 2, c.ID, c.Version, "owner") }, HoldNotWaiting},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			ctx := context.Background()
			store, sender := &Memory{}, &fakeSender{}
			q := autoQueue(store, sender)
			c := reviewedComment(t, store, true)
			tc.setup(q, store, &c)

			// Act
			sent, why, err := q.TryAutoSend(ctx, 2, c.ID)

			// Assert
			if err != nil || sent || why != tc.want || len(sender.calls) != 0 {
				t.Fatalf("sent %v why %q err %v calls %v, want hold %q and nothing sent", sent, why, err, sender.calls, tc.want)
			}
		})
	}
}

// setIntent changes a stored comment's intent (Memory only).
func setIntent(s Store, id int64, in Intent) {
	m := s.(*Memory)
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rows {
		if m.rows[i].ID == id {
			m.rows[i].Intent = in
		}
	}
}

// sentEarlier records an earlier private reply to the same person (FromID 9).
func sentEarlier(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	old := waitingComment(t, s, now.Add(-48*time.Hour))
	if _, err := s.Claim(ctx, 2, old.ID, old.Version); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, old.ID, StatusSent, "hi", "owner", ""); err != nil {
		t.Fatal(err)
	}
}

func TestReviewStats_AutoSentIsNotAnOwnerDecision(t *testing.T) {
	// Arrange
	ctx := context.Background()
	store := &Memory{}
	q := autoQueue(store, &fakeSender{})
	c := reviewedComment(t, store, true)
	if sent, why, err := q.TryAutoSend(ctx, 2, c.ID); !sent {
		t.Fatalf("setup: %q %v", why, err)
	}

	// Act
	s, err := store.ReviewStats(ctx, 2)

	// Assert
	if err != nil || s.AutoSent != 1 || s.Decided != 0 || s.Agreed != 0 {
		t.Fatalf("stats %+v err %v", s, err)
	}
}

type recordingAuto struct{ ids []int64 }

func (r *recordingAuto) TryAutoSend(_ context.Context, _, id int64) (bool, string, error) {
	r.ids = append(r.ids, id)
	return true, "", nil
}

func TestWorker_OffersOnlyPassedReviewsToAutoSend(t *testing.T) {
	cases := map[string]struct {
		review Review
		want   int
	}{
		"passed": {goodReview(), 1},
		"held":   {Review{Checks: map[string]float64{}}, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			ctx := context.Background()
			store := &Memory{}
			_, _ = store.Add(ctx, Comment{AccountID: 1, CommentID: "lead", CreatedAt: time.Now()})
			auto := &recordingAuto{}
			w := &Worker{Store: store, Scorer: fixedScorer{intent: IntentLead}, Reviewer: &fixedReviewer{r: c.review},
				AutoSend: auto, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Timeout: time.Second}

			// Act
			w.RunOnce(ctx)

			// Assert
			if len(auto.ids) != c.want {
				t.Fatalf("offered %v, want %d", auto.ids, c.want)
			}
		})
	}
}
