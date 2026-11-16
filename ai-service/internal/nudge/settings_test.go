package nudge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaults_For(t *testing.T) {
	cases := map[string]struct {
		d    Defaults
		acct int64
		on   bool
	}{
		"env off":            {Defaults{Text: "x"}, 3, false},
		"no text":            {Defaults{After: 20 * time.Hour}, 3, false},
		"every account":      {Defaults{After: 20 * time.Hour, Text: "x"}, 3, true},
		"listed account":     {Defaults{After: 20 * time.Hour, Text: "x", Accounts: []int64{3}}, 3, true},
		"not listed account": {Defaults{After: 20 * time.Hour, Text: "x", Accounts: []int64{1}}, 3, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			got := tc.d.For(tc.acct)
			// Assert
			if got.Active() != tc.on {
				t.Fatalf("For(%d) = %+v, want active=%v", tc.acct, got, tc.on)
			}
		})
	}
}

func TestRule_Validate(t *testing.T) {
	ok := Rule{On: true, After: 20 * time.Hour, Text: "สวัสดีครับคุณลูกค้า"}
	cases := map[string]struct {
		r       Rule
		resolve time.Duration
		bad     bool
	}{
		"fine":                  {ok, 22 * time.Hour, false},
		"fine, resolve off":     {Rule{After: 22 * time.Hour, Text: "x"}, 0, false},
		"off still needs text":  {Rule{After: 20 * time.Hour}, 0, true},
		"under an hour":         {Rule{After: 30 * time.Minute, Text: "x"}, 0, true},
		"over 22 hours":         {Rule{After: 23 * time.Hour, Text: "x"}, 0, true},
		"not whole hours":       {Rule{After: 90 * time.Minute, Text: "x"}, 0, true},
		"not before resolve":    {Rule{After: 22 * time.Hour, Text: "x"}, 22 * time.Hour, true},
		"text too long":         {Rule{After: 20 * time.Hour, Text: strings.Repeat("ก", MaxText+1)}, 0, true},
		"blank text":            {Rule{After: 20 * time.Hour, Text: " \n "}, 0, true},
		"invalid utf8 text":     {Rule{After: 20 * time.Hour, Text: "a\xffb"}, 0, true},
		"max length is allowed": {Rule{After: 20 * time.Hour, Text: strings.Repeat("ก", MaxText)}, 0, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			err := tc.r.Validate(tc.resolve)
			// Assert
			if (err != nil) != tc.bad {
				t.Fatalf("Validate = %v, want refused=%v", err, tc.bad)
			}
		})
	}
}

func TestSettings_SaveAppliesAndSurvivesRestart(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "nudge-settings.json")
	d := Defaults{After: 20 * time.Hour, Text: "env", Accounts: []int64{1}, ResolveAfter: 22 * time.Hour}
	s, err := OpenSettings(path, d)
	if err != nil {
		t.Fatal(err)
	}
	want := Rule{On: true, After: 18 * time.Hour, Text: "สวัสดีครับ\nคุณลูกค้า", Write: true}
	// Act
	err = s.Save(3, Rule{On: true, After: 18 * time.Hour, Text: "  สวัสดีครับ\r\nคุณลูกค้า  ", Write: true})
	again, openErr := OpenSettings(path, d)
	// Assert
	if err != nil || openErr != nil {
		t.Fatalf("save=%v open=%v", err, openErr)
	}
	if got := s.For(3); got != want {
		t.Fatalf("For(3) = %+v, want %+v (text cleaned)", got, want)
	}
	if got := again.For(3); got != want || !again.Saved(3) {
		t.Fatalf("after reopen For(3) = %+v, want %+v", got, want)
	}
	if got := again.For(1); got.Text != "env" || !got.Active() || again.Saved(1) {
		t.Fatalf("For(1) = %+v, want the env default", got)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v err %v, want 0600", info.Mode().Perm(), err)
	}
}

func TestSettings_RejectedSaveChangesNothing(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "nudge-settings.json")
	s, _ := OpenSettings(path, Defaults{ResolveAfter: 22 * time.Hour})
	if err := s.Save(3, Rule{On: true, After: 20 * time.Hour, Text: "ok"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	// Act
	errs := []error{
		s.Save(3, Rule{On: true, After: 22 * time.Hour, Text: "too late"}),
		s.Save(3, Rule{On: true, After: 20 * time.Hour, Text: ""}),
		s.Save(0, Rule{On: true, After: 20 * time.Hour, Text: "bad account"}),
	}
	after, _ := os.ReadFile(path)
	// Assert
	for i, err := range errs {
		if err == nil {
			t.Fatalf("save %d accepted, want refused", i)
		}
	}
	if s.For(3).Text != "ok" || string(before) != string(after) {
		t.Fatalf("a refused save changed the settings: %+v", s.For(3))
	}
}

func TestOpenSettings_DamagedFileTurnsEveryoneOff(t *testing.T) {
	// Arrange: the defaults are on for every account.
	path := filepath.Join(t.TempDir(), "nudge-settings.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	on := Defaults{After: 20 * time.Hour, Text: "env"}
	// Act
	s, err := OpenSettings(path, on)
	saveErr := s.Save(3, Rule{On: true, After: 20 * time.Hour, Text: "x"})
	raw, _ := os.ReadFile(path)
	// Assert
	if err == nil || s.For(3).Active() || s.For(1).Active() {
		t.Fatalf("err=%v rule=%+v, want an error and every account off", err, s.For(3))
	}
	if saveErr == nil || string(raw) != "{" {
		t.Fatalf("save=%v file=%q, want the save refused and the file kept", saveErr, raw)
	}
}

func TestOpenSettings_BadEntryTurnsOnlyThatAccountOff(t *testing.T) {
	cases := map[string]string{
		"bad hours":   `{"3":{"on":true,"after_hours":40,"text":"a"},"4":{"on":false,"after_hours":20,"text":"b"}}`,
		"no text":     `{"3":{"on":true,"after_hours":20,"text":""},"4":{"on":false,"after_hours":20,"text":"b"}}`,
		"bad account": `{"x":{"on":true,"after_hours":20,"text":"a"},"3":{"on":false,"after_hours":20,"text":"c"},"4":{"on":false,"after_hours":20,"text":"b"}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange: defaults on for every account; 4 was saved off.
			path := filepath.Join(t.TempDir(), "nudge-settings.json")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			// Act
			s, err := OpenSettings(path, Defaults{After: 20 * time.Hour, Text: "env"})
			// Assert
			if err == nil || s.For(3).Active() || s.For(4).Active() || !s.For(5).Active() {
				t.Fatalf("err=%v 3=%+v 4=%+v 5=%+v, want 3 and 4 off, 5 on the default", err, s.For(3), s.For(4), s.For(5))
			}
			if saveErr := s.Save(5, Rule{On: true, After: 20 * time.Hour, Text: "y"}); saveErr != nil {
				t.Fatalf("save = %v", saveErr)
			}
			again, _ := OpenSettings(path, Defaults{After: 20 * time.Hour, Text: "env"})
			if again.For(4).Active() || !again.Saved(4) {
				t.Fatal("a later save dropped account 4's saved off")
			}
		})
	}
}
