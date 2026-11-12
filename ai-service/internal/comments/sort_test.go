package comments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// Owner, 2026-11-13: "Jev is used for reviewing intent and the LLM writes the
// message." Jev sorts each comment first; the LLM only writes.

type fixedSorter struct {
	s     Sorting
	err   error
	calls int
}

func (f *fixedSorter) Sort(_ context.Context, _ Comment, _ profile.Profile) (Sorting, error) {
	f.calls++
	return f.s, f.err
}

// recordingScorer answers "question" with a draft and remembers what it got.
type recordingScorer struct {
	calls int
	got   Comment
}

func (r *recordingScorer) Score(_ context.Context, c Comment) (Intent, string, string, error) {
	r.calls++
	r.got = c
	intent := IntentQuestion
	if c.SortedAs != "" {
		intent = c.SortedAs
	}
	return intent, "why", "draft", nil
}

func sortingWorker(store Store, sorter Sorter, scorer *recordingScorer) *Worker {
	return &Worker{Store: store, Scorer: scorer, Sorter: sorter,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Timeout: time.Second}
}

func TestWorker_SureOtherIsSkippedWithoutTheLLM(t *testing.T) {
	// Arrange
	ctx := context.Background()
	store := &Memory{}
	_, _ = store.Add(ctx, Comment{AccountID: 1, CommentID: "c1", Message: "555", CreatedAt: time.Now()})
	sorter := &fixedSorter{s: Sorting{Intent: IntentOther, P: 0.97, Model: "jev-1.14.0"}}
	scorer := &recordingScorer{}

	// Act
	n := sortingWorker(store, sorter, scorer).RunOnce(ctx)

	// Assert
	recent, _ := store.Recent(ctx, 1, 10)
	if n != 1 || scorer.calls != 0 || len(recent) != 1 || recent[0].Status != StatusIgnored || recent[0].Intent != IntentOther {
		t.Fatalf("n %d llm calls %d recent %+v", n, scorer.calls, recent)
	}
	if !strings.Contains(recent[0].Reason, "jev-1.14.0") || !strings.Contains(recent[0].Reason, "97%") {
		t.Fatalf("the reason must say who sorted it and how sure: %q", recent[0].Reason)
	}
}

func TestWorker_LLMWritesForTheIntentTheSorterChose(t *testing.T) {
	// Arrange
	ctx := context.Background()
	store := &Memory{}
	_, _ = store.Add(ctx, Comment{AccountID: 1, CommentID: "c1", Message: "สนใจครับ", CreatedAt: time.Now()})
	scorer := &recordingScorer{}

	// Act
	sortingWorker(store, &fixedSorter{s: Sorting{Intent: IntentLead, P: 0.9}}, scorer).RunOnce(ctx)

	// Assert
	waiting, _ := store.Waiting(ctx, 1)
	if scorer.calls != 1 || scorer.got.SortedAs != IntentLead || len(waiting) != 1 || waiting[0].Intent != IntentLead || waiting[0].Draft != "draft" {
		t.Fatalf("llm got %+v waiting %+v", scorer.got, waiting)
	}
}

func TestWorker_UnsureOrFailedSortingLeavesItToTheLLM(t *testing.T) {
	cases := map[string]*fixedSorter{
		"unsure other": {s: Sorting{Intent: IntentOther, P: 0.80}},
		"sorter down":  {err: errors.New("jev answered 503")},
	}
	for name, sorter := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			ctx := context.Background()
			store := &Memory{}
			_, _ = store.Add(ctx, Comment{AccountID: 1, CommentID: "c1", Message: "ราคา?", CreatedAt: time.Now()})
			scorer := &recordingScorer{}

			// Act
			sortingWorker(store, sorter, scorer).RunOnce(ctx)

			// Assert
			waiting, _ := store.Waiting(ctx, 1)
			if scorer.calls != 1 || scorer.got.SortedAs != "" || len(waiting) != 1 || waiting[0].Intent != IntentQuestion {
				t.Fatalf("the LLM must sort it itself: got %+v waiting %+v", scorer.got, waiting)
			}
		})
	}
}

func TestScorer_KeepsTheSortedIntent(t *testing.T) {
	// Arrange: the LLM disagrees and says "other"
	model := &stubLLM{content: `{"intent":"other","reason":"chat","reply":"สวัสดีครับ ขอบคุณที่สนใจครับ"}`}
	s := &Scorer{LLM: model}

	// Act
	intent, _, draft, err := s.Score(context.Background(), Comment{Message: "สนใจ", SortedAs: IntentLead})

	// Assert
	if err != nil || intent != IntentLead || draft != "สวัสดีครับ ขอบคุณที่สนใจครับ" {
		t.Fatalf("intent %q draft %q err %v", intent, draft, err)
	}
	if !strings.Contains(model.got[0].Content, `already sorted as "lead"`) {
		t.Fatalf("the LLM must be told the intent: %q", model.got[0].Content)
	}
}

func TestJevReviewer_SortAsksOnlyTheIntent(t *testing.T) {
	// Arrange
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"model":"jev-1.14.0","answers":{"intent":{"type":"choice","choice":"other","probabilities":{"lead":0.02,"question":0.01,"complaint":0,"other":0.97}}}}`)
	}))
	defer srv.Close()
	j := &JevReviewer{URL: srv.URL, APIKey: "k"}

	// Act
	s, err := j.Sort(context.Background(), Comment{Message: "555", PostKnown: true, PostText: "post"}, profile.Profile{})

	// Assert
	if err != nil || s.Intent != IntentOther || s.P < 0.96 || s.Model != "jev-1.14.0" {
		t.Fatalf("sorting %+v err %v", s, err)
	}
	qs, _ := got["questions"].(map[string]any)
	state, _ := got["state"].(map[string]any)
	if len(qs) != 1 || qs["intent"] == nil || state["draft"] != nil || state["post"] == nil {
		t.Fatalf("request questions %v state %v", qs, state)
	}
}
