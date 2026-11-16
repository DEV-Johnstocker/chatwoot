package alerts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testDefaults = Defaults{SpikeShare: 0.5, ReplyLimit: Limit{Max: 30, Window: time.Hour}}

func validSettings() Settings {
	return Settings{SpikeShare: 0.5, SpikeMinTurns: 10}
}

func TestOpenSettings_NoFileUsesDefaults(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "alert-settings.json")

	// Act
	s, err := OpenSettings(path, testDefaults)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	share, turns := s.Spike()
	if share != 0.5 || turns != SpikeMinTurns || s.ReplyLimitFor(1) != testDefaults.ReplyLimit || !s.Enabled(QueueFull) {
		t.Errorf("share %v turns %d limit %+v", share, turns, s.ReplyLimitFor(1))
	}
}

func TestSave_AppliesAtOnceAndSurvivesRestart(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "alert-settings.json")
	s, _ := OpenSettings(path, testDefaults)
	next := Settings{Off: []Kind{QueueFull}, SpikeShare: 0.7, SpikeMinTurns: 20,
		ReplyLimits: map[int64]Limit{3: {Max: 10, Window: 30 * time.Minute}}}

	// Act
	err := s.Save(next)
	reopened, openErr := OpenSettings(path, testDefaults)

	// Assert
	if err != nil || openErr != nil {
		t.Fatalf("save %v, reopen %v", err, openErr)
	}
	for _, st := range []*SettingsStore{s, reopened} {
		share, turns := st.Spike()
		if share != 0.7 || turns != 20 || st.Enabled(QueueFull) || !st.Enabled(LLMError) {
			t.Errorf("spike %v/%d, queue on %v", share, turns, st.Enabled(QueueFull))
		}
		if got := st.ReplyLimitFor(3); got != (Limit{Max: 10, Window: 30 * time.Minute}) {
			t.Errorf("account 3 limit = %+v", got)
		}
		if got := st.ReplyLimitFor(1); got != testDefaults.ReplyLimit {
			t.Errorf("account 1 (not saved) = %+v, want the default", got)
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, err %v", fi.Mode().Perm(), err)
	}
}

func TestSave_RejectsOutOfRangeValuesAndKeepsTheOld(t *testing.T) {
	cases := map[string]func(*Settings){
		"share too low":      func(s *Settings) { s.SpikeShare = 0.05 },
		"share above 100%":   func(s *Settings) { s.SpikeShare = 1.5 },
		"min turns too low":  func(s *Settings) { s.SpikeMinTurns = 2 },
		"min turns too high": func(s *Settings) { s.SpikeMinTurns = 5000 },
		"negative limit":     func(s *Settings) { s.ReplyLimits = map[int64]Limit{1: {Max: -1, Window: time.Hour}} },
		"zero while on":      func(s *Settings) { s.ReplyLimits = map[int64]Limit{1: {Max: 0, Window: time.Hour}} },
		"limit too high":     func(s *Settings) { s.ReplyLimits = map[int64]Limit{1: {Max: 1001, Window: time.Hour}} },
		"window too short":   func(s *Settings) { s.ReplyLimits = map[int64]Limit{1: {Max: 5, Window: time.Second}} },
		"window over a day":  func(s *Settings) { s.ReplyLimits = map[int64]Limit{1: {Max: 5, Window: 25 * time.Hour}} },
		"account zero":       func(s *Settings) { s.ReplyLimits = map[int64]Limit{0: {Max: 5, Window: time.Hour}} },
		"unknown alert kind": func(s *Settings) { s.Off = []Kind{"rm -rf"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			path := filepath.Join(t.TempDir(), "alert-settings.json")
			s, _ := OpenSettings(path, testDefaults)
			bad := validSettings()
			mutate(&bad)

			// Act
			err := s.Save(bad)

			// Assert
			if err == nil {
				t.Fatal("saved an out-of-range value")
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Error("a rejected save wrote the file")
			}
			if share, turns := s.Spike(); share != 0.5 || turns != SpikeMinTurns {
				t.Errorf("a rejected save changed the live rule: %v/%d", share, turns)
			}
		})
	}
}

func TestOpenSettings_BrokenFileFallsBackToDefaults(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "alert-settings.json")
	if err := os.WriteFile(path, []byte(`{"spike_share": 9, "spike_min_turns": 10}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	s, err := OpenSettings(path, testDefaults)

	// Assert
	if err == nil || s == nil {
		t.Fatalf("err %v, store %v: want an error and a usable store", err, s)
	}
	if share, _ := s.Spike(); share != 0.5 {
		t.Errorf("share = %v, want the default", share)
	}
}

func TestGet_ReturnsACopy(t *testing.T) {
	// Arrange
	s, _ := OpenSettings(filepath.Join(t.TempDir(), "a.json"), testDefaults)
	_ = s.Save(Settings{SpikeShare: 0.5, SpikeMinTurns: 10, ReplyLimits: map[int64]Limit{1: {Max: 5, Window: time.Hour}}})

	// Act
	got := s.Get()
	got.ReplyLimits[1] = Limit{Max: 999, Window: time.Hour}

	// Assert
	if s.ReplyLimitFor(1).Max != 5 {
		t.Error("changing a copy changed the store")
	}
}

func TestCheck_SwitchedOffAlertNeitherFiresNorStaysActive(t *testing.T) {
	// Arrange
	n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
	rules, _ := OpenSettings(filepath.Join(t.TempDir(), "a.json"), testDefaults)
	m := New(Options{Notifier: n, Rules: rules, Now: c.now, Logger: quiet()})
	m.Record(QueueFull, 1)
	m.Check(context.Background())
	_ = rules.Save(Settings{Off: []Kind{QueueFull}, SpikeShare: 0.5, SpikeMinTurns: 10})

	// Act
	c.add(time.Minute)
	m.Record(QueueFull, 1)
	m.Check(context.Background())

	// Assert
	if len(n.sent) != 1 {
		t.Errorf("sent %q, want only the alert from before it was switched off", n.sent)
	}
	if len(m.Active()) != 0 {
		t.Errorf("active = %+v, want it gone from Home once switched off", m.Active())
	}
}

func TestCheck_SpikeUsesTheSavedRule(t *testing.T) {
	// Arrange: 4 of 5 handed off; the default needs 10 messages
	n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
	rules, _ := OpenSettings(filepath.Join(t.TempDir(), "a.json"), testDefaults)
	m := New(Options{Notifier: n, Rules: rules, Now: c.now, Logger: quiet()})
	for i := 0; i < 5; i++ {
		m.Turn(i < 4)
	}
	m.Check(context.Background())
	before := len(n.sent)

	// Act
	_ = rules.Save(Settings{SpikeShare: 0.8, SpikeMinTurns: 5})
	m.Check(context.Background())

	// Assert
	if before != 0 || len(n.sent) != 1 || !strings.Contains(n.sent[0], "4 of the last 5") {
		t.Errorf("before %d, sent %q", before, n.sent)
	}
}

func TestSave_OffKeepsTheNumbers(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "a.json")
	s, _ := OpenSettings(path, testDefaults)
	off := Limit{Off: true, Max: 12, Window: 2 * time.Hour}

	// Act
	err := s.Save(Settings{SpikeShare: 0.5, SpikeMinTurns: 10, ReplyLimits: map[int64]Limit{1: off}})
	reopened, _ := OpenSettings(path, testDefaults)

	// Assert
	if err != nil || reopened.ReplyLimitFor(1) != off || reopened.ReplyLimitFor(1).On() {
		t.Errorf("err %v, limit %+v", err, reopened.ReplyLimitFor(1))
	}
}
