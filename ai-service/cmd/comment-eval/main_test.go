package main

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/comments"
)

func TestLoadCases_TheMockSetIsValid(t *testing.T) {
	// Act
	cases, err := loadCases("../../testdata/comment-eval/cases.jsonl")

	// Assert
	if err != nil || len(cases) < 40 {
		t.Fatalf("cases %d err %v", len(cases), err)
	}
	pass := 0
	for _, c := range cases {
		if c.WantPass {
			pass++
			if c.WantIntent != comments.IntentLead && c.WantIntent != comments.IntentQuestion {
				t.Errorf("%s: only leads and questions can pass", c.ID)
			}
			if len(comments.Flags(c.Draft)) > 0 {
				t.Errorf("%s: a passing draft trips the code rules: %v", c.ID, comments.Flags(c.Draft))
			}
		}
	}
	if pass == 0 || pass == len(cases) {
		t.Fatalf("labels are one-sided: %d of %d pass", pass, len(cases))
	}
}

func TestSummarize(t *testing.T) {
	// Arrange
	lead := map[comments.Intent]float64{comments.IntentLead: 1}
	sure := map[string]float64{comments.CheckAnswersComment: 1, comments.CheckNoUnbackedOffer: 1, comments.CheckToneFits: 1, comments.CheckGenuine: 1}
	ok := comments.Review{Intent: comments.IntentLead, PIntent: lead, Checks: sure, Pass: true, InputTokens: 1000, OutputTokens: 100, Latency: time.Second}
	results := []Result{
		{Case: Case{ID: "a", WantIntent: comments.IntentLead, WantPass: true}, Review: ok},                                    // right
		{Case: Case{ID: "b", WantIntent: comments.IntentLead, WantPass: false}, Review: ok},                                   // false send
		{Case: Case{ID: "c", WantIntent: comments.IntentLead, WantPass: true}, Err: errors.New("timeout")},                    // error: missed
		{Case: Case{ID: "d", WantIntent: comments.IntentComplaint, WantPass: false}, Review: comments.Review{Intent: "lead"}}, // right decision, wrong intent
	}

	// Act
	s := Summarize("x", results, Price{In: 1, Out: 10}, true)

	// Assert
	if s.Cases != 4 || s.Errors != 1 || s.FalseSends != 1 || s.MissedSends != 1 || s.DecisionAcc != 0.5 {
		t.Fatalf("summary %+v", s)
	}
	if math.Abs(s.IntentAcc-2.0/3) > 1e-9 || s.FalseSendCases[0] != "b" {
		t.Fatalf("intent %v false %v", s.IntentAcc, s.FalseSendCases)
	}
	// avg 2000/3 in, 200/3 out tokens; $1 and $10 per M → per 1000 comments
	want := (2000.0/3*1 + 200.0/3*10) / 1e6 * 1000
	if math.Abs(s.CostPer1000-want) > 1e-9 {
		t.Fatalf("cost %v want %v", s.CostPer1000, want)
	}
}

func TestPercentile(t *testing.T) {
	// Arrange
	d := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// Act / Assert
	if percentile(d, 0.5) != 5 || percentile(d, 0.95) != 10 || percentile(nil, 0.5) != 0 {
		t.Fatal("percentile wrong")
	}
}

func TestSafeURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://api.openai.com/v1": true, "http://127.0.0.1:8080/v1": true, "http://localhost:1/v1": true,
		"http://api.example.com/v1": false, "ftp://x": false, "not a url": false,
	} {
		// Act / Assert
		if err := safeURL(raw); (err == nil) != ok {
			t.Errorf("%s: err %v", raw, err)
		}
	}
}
