package nudge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The owner's follow-up choices from /admin/nudge, per account: on or off,
// how long after the customer's last message, the fixed text, and whether
// the AI writes the follow-up (the fixed text stays its fallback). They live
// in a small JSON file next to the AI provider settings (the /data volume),
// so no database is needed; the BOT_NUDGE_* variables give the values until
// the owner saves an account. Issue: DEV-Johnstocker/chatwoot#1

// Ranges the page accepts (checked again on save, whatever the form sends).
const (
	MinAfter = time.Hour
	MaxAfter = 22 * time.Hour
	MaxText  = 1000 // characters, like BOT_NUDGE_TEXT

	settingsFileMode os.FileMode = 0o600
)

// SuggestedText is the owner-approved fixed follow-up (option A, 2026-11-16),
// offered on the page while an account has no text yet.
const SuggestedText = "สวัสดีครับคุณลูกค้า ขอบคุณที่สนใจนะครับ 🙏 ถ้าอยากให้ช่วยเลือกแพ็กเกจที่เหมาะ หรือมีคำถามเพิ่มเติม พิมพ์บอกได้เลยครับ"

// Rule is one account's follow-up setting.
type Rule struct {
	On    bool
	After time.Duration // after the customer's last message
	Text  string        // the fixed follow-up; also the fallback when Write is on
	Write bool          // the AI writes each follow-up from the chat
}

// Active reports whether follow-ups go out under this rule.
func (r Rule) Active() bool {
	return r.On && r.After > 0 && strings.TrimSpace(r.Text) != ""
}

// String is the audit and log form, e.g. "on, 20h, AI-written" or "off".
func (r Rule) String() string {
	if !r.Active() {
		return "off"
	}
	mode := "fixed text"
	if r.Write {
		mode = "AI-written"
	}
	return fmt.Sprintf("on, %dh, %s", int(r.After/time.Hour), mode)
}

// Validate checks the rule's ranges. resolveAfter is the auto-resolve time
// (0 = off): a follow-up must go out before the chat is resolved. The text
// is required even when off, so switching on never sends an empty message.
func (r Rule) Validate(resolveAfter time.Duration) error {
	var errs []error
	if r.After < MinAfter || r.After > MaxAfter || r.After%time.Hour != 0 {
		errs = append(errs, errors.New("the wait must be a whole number of hours from 1 to 22"))
	} else if resolveAfter > 0 && r.After >= resolveAfter {
		errs = append(errs, fmt.Errorf("the wait must be shorter than the auto-resolve time (%s)", hours(resolveAfter)))
	}
	text := strings.TrimSpace(r.Text)
	switch {
	case text == "":
		errs = append(errs, errors.New("write the follow-up text (it is also sent when the AI draft is refused)"))
	case utf8.RuneCountInString(text) > MaxText:
		errs = append(errs, fmt.Errorf("the follow-up text must be at most %d characters", MaxText))
	case !utf8.ValidString(text):
		errs = append(errs, errors.New("the follow-up text is not valid text"))
	}
	return errors.Join(errs...)
}

func hours(d time.Duration) string {
	if d%time.Hour == 0 {
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return d.String()
}

// CleanText trims the text and makes line ends plain "\n".
func CleanText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

// Defaults are the values before the owner saves an account (environment).
type Defaults struct {
	After    time.Duration
	Text     string
	Write    bool
	Accounts []int64 // accounts on by default; empty = every account
	// ResolveAfter is BOT_RESOLVE_AFTER; follow-ups must come before it.
	ResolveAfter time.Duration
}

// For is an account's rule when it saved none.
func (d Defaults) For(acct int64) Rule {
	on := d.After > 0 && strings.TrimSpace(d.Text) != ""
	if on && len(d.Accounts) > 0 {
		on = false
		for _, a := range d.Accounts {
			if a == acct {
				on = true
			}
		}
	}
	return Rule{On: on, After: d.After, Text: d.Text, Write: d.Write}
}

// SettingsStore keeps the per-account rules in memory and in a JSON file.
// Reads are cheap, so the sweeper asks on every sweep: a save applies at the
// next sweep, with no restart.
type SettingsStore struct {
	mu       sync.Mutex
	path     string
	defaults Defaults
	saved    map[int64]Rule
}

type fileRule struct {
	On         bool   `json:"on"`
	AfterHours int    `json:"after_hours"`
	Text       string `json:"text"`
	Write      bool   `json:"write"`
}

// OpenSettings loads path, or starts from the defaults when there is no
// file. A file that cannot be read or fails the checks is reported, and the
// defaults are used so nothing unexpected is sent; the next save replaces it.
func OpenSettings(path string, d Defaults) (*SettingsStore, error) {
	s := &SettingsStore{path: path, defaults: d, saved: map[int64]Rule{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read nudge settings: %w", err)
	}
	var f map[string]fileRule
	if err := json.Unmarshal(raw, &f); err != nil {
		return s, fmt.Errorf("parse nudge settings %s: %w", path, err)
	}
	loaded := map[int64]Rule{}
	for key, fr := range f {
		acct, err := strconv.ParseInt(key, 10, 64)
		if err != nil || acct < 1 {
			return s, fmt.Errorf("nudge settings %s: account %q is not a positive number", path, key)
		}
		r := Rule{On: fr.On, After: time.Duration(fr.AfterHours) * time.Hour, Text: fr.Text, Write: fr.Write}
		// The auto-resolve time may have changed since the save: check the
		// ranges only, and let the sweep send what the owner saved.
		if err := r.Validate(0); err != nil {
			return s, fmt.Errorf("nudge settings %s, account %d: %w", path, acct, err)
		}
		loaded[acct] = r
	}
	s.saved = loaded
	return s, nil
}

// For returns the account's saved rule, or the default.
func (s *SettingsStore) For(acct int64) Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.saved[acct]; ok {
		return r
	}
	return s.defaults.For(acct)
}

// Saved reports whether the account saved its own rule.
func (s *SettingsStore) Saved(acct int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.saved[acct]
	return ok
}

// Defaults returns the environment defaults.
func (s *SettingsStore) Defaults() Defaults { return s.defaults }

// Save validates the account's rule, writes the file atomically and makes it
// current.
func (s *SettingsStore) Save(acct int64, r Rule) error {
	if acct < 1 {
		return errors.New("account must be a positive number")
	}
	r.Text = CleanText(r.Text)
	if err := r.Validate(s.defaults.ResolveAfter); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := maps.Clone(s.saved)
	next[acct] = r
	f := make(map[string]fileRule, len(next))
	for a, x := range next {
		f[strconv.FormatInt(a, 10)] = fileRule{On: x.On, AfterHours: int(x.After / time.Hour), Text: x.Text, Write: x.Write}
	}
	buf, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(s.path, buf); err != nil {
		return err
	}
	s.saved = next
	return nil
}

func writeAtomic(path string, buf []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create nudge settings dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".nudge-settings-*.json")
	if err != nil {
		return fmt.Errorf("write nudge settings: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(settingsFileMode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write nudge settings: %w", err)
	}
	return nil
}
