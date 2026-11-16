package alerts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
)

// The owner's alert choices from /admin/alerts: which alerts are on, when a
// hand-off spike counts (server-wide, like the alerts themselves), and the
// per-customer reply limit of each account. They live in a small JSON file
// next to the AI provider settings (the /data volume), so no database is
// needed; environment variables give the values until the owner saves.
// Issue: DEV-Johnstocker/chatwoot#1

// Ranges the page accepts (checked again on save, whatever the form sends).
const (
	MinSpikeShare, MaxSpikeShare               = 0.1, 1.0
	MinSpikeTurns, MaxSpikeTurns               = 3, 1000
	MaxReplyLimit                              = 1000
	MinReplyWindow, MaxReplyWindow             = time.Minute, 24 * time.Hour
	settingsFileMode               os.FileMode = 0o600
)

// Limit is how many bot replies one customer gets per window. Off keeps Max
// and Window, so switching the limit on again restores them; Max 0 (the
// REPLY_LIMIT=0 default) is off too.
type Limit struct {
	Off    bool
	Max    int
	Window time.Duration
}

// On reports whether the limit applies.
func (l Limit) On() bool { return !l.Off && l.Max > 0 && l.Window > 0 }

// String is the audit and log form, e.g. "30 per 1h0m0s" or "off".
func (l Limit) String() string {
	if !l.On() {
		return "off"
	}
	return fmt.Sprintf("%d per %s", l.Max, l.Window)
}

// Validate checks the limit's ranges.
func (l Limit) Validate() error {
	var errs []error
	if l.Max < 1 || l.Max > MaxReplyLimit {
		if !l.Off || l.Max != 0 {
			errs = append(errs, fmt.Errorf("replies per customer must be 1 to %d", MaxReplyLimit))
		}
	}
	if l.Window < MinReplyWindow || l.Window > MaxReplyWindow {
		errs = append(errs, errors.New("the reply limit window must be 1 minute to 24 hours"))
	}
	return errors.Join(errs...)
}

// Settings are the saved choices.
type Settings struct {
	Off           []Kind // alerts switched off
	SpikeShare    float64
	SpikeMinTurns int
	// ReplyLimits holds each account that saved its own limit.
	ReplyLimits map[int64]Limit
}

// Validate checks every value.
func (s Settings) Validate() error {
	var errs []error
	for _, k := range s.Off {
		if !slices.Contains(kinds, k) {
			errs = append(errs, fmt.Errorf("unknown alert %q", string(k)))
		}
	}
	if s.SpikeShare < MinSpikeShare || s.SpikeShare > MaxSpikeShare {
		errs = append(errs, errors.New("the hand-off share must be 10% to 100%"))
	}
	if s.SpikeMinTurns < MinSpikeTurns || s.SpikeMinTurns > MaxSpikeTurns {
		errs = append(errs, fmt.Errorf("the minimum answered messages must be %d to %d", MinSpikeTurns, MaxSpikeTurns))
	}
	for acct, l := range s.ReplyLimits {
		if acct < 1 {
			errs = append(errs, errors.New("account must be a positive number"))
		}
		errs = append(errs, l.Validate())
	}
	return errors.Join(errs...)
}

// Enabled reports whether an alert kind is on.
func (s Settings) Enabled(k Kind) bool { return !slices.Contains(s.Off, k) }

// Defaults are the values before the owner saves (environment variables).
type Defaults struct {
	SpikeShare float64
	ReplyLimit Limit
}

// SettingsStore keeps the settings in memory and in a JSON file. Reads are
// cheap, so the bot and the monitor ask it on every use: a save applies at
// once, with no restart.
type SettingsStore struct {
	mu       sync.Mutex
	path     string
	defaults Defaults
	cur      Settings
}

// fileSettings is the file format: durations in minutes, accounts as keys.
type fileSettings struct {
	Off           []Kind                    `json:"off,omitempty"`
	SpikeShare    float64                   `json:"spike_share"`
	SpikeMinTurns int                       `json:"spike_min_turns"`
	ReplyLimits   map[string]fileReplyLimit `json:"reply_limits,omitempty"`
}

type fileReplyLimit struct {
	Off           bool `json:"off,omitempty"`
	Max           int  `json:"max"`
	WindowMinutes int  `json:"window_minutes"`
}

// OpenSettings loads path, or starts from defaults when there is no file. A
// file that cannot be read or fails the checks is reported, and the defaults
// are used so the bot keeps running; the next save replaces the file.
func OpenSettings(path string, d Defaults) (*SettingsStore, error) {
	if d.SpikeShare < MinSpikeShare || d.SpikeShare > MaxSpikeShare {
		d.SpikeShare = DefaultSpikeShare
	}
	s := &SettingsStore{path: path, defaults: d, cur: Settings{SpikeShare: d.SpikeShare, SpikeMinTurns: SpikeMinTurns}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read alert settings: %w", err)
	}
	var f fileSettings
	if err := json.Unmarshal(raw, &f); err != nil {
		return s, fmt.Errorf("parse alert settings %s: %w", path, err)
	}
	loaded, err := fromFile(f)
	if err == nil {
		err = loaded.Validate()
	}
	if err != nil {
		return s, fmt.Errorf("alert settings %s: %w", path, err)
	}
	s.cur = loaded
	return s, nil
}

func fromFile(f fileSettings) (Settings, error) {
	s := Settings{Off: f.Off, SpikeShare: f.SpikeShare, SpikeMinTurns: f.SpikeMinTurns}
	for key, l := range f.ReplyLimits {
		acct, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return s, fmt.Errorf("account %q is not a number", key)
		}
		if s.ReplyLimits == nil {
			s.ReplyLimits = map[int64]Limit{}
		}
		s.ReplyLimits[acct] = Limit{Off: l.Off, Max: l.Max, Window: time.Duration(l.WindowMinutes) * time.Minute}
	}
	return s, nil
}

func toFile(s Settings) fileSettings {
	f := fileSettings{Off: s.Off, SpikeShare: s.SpikeShare, SpikeMinTurns: s.SpikeMinTurns}
	for acct, l := range s.ReplyLimits {
		if f.ReplyLimits == nil {
			f.ReplyLimits = map[string]fileReplyLimit{}
		}
		f.ReplyLimits[strconv.FormatInt(acct, 10)] = fileReplyLimit{Off: l.Off, Max: l.Max, WindowMinutes: int(l.Window / time.Minute)}
	}
	return f
}

// Get returns a copy of the current settings.
func (s *SettingsStore) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.cur)
}

// ReplyLimitFor returns the account's saved limit, or the default.
func (s *SettingsStore) ReplyLimitFor(acct int64) Limit {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.cur.ReplyLimits[acct]; ok {
		return l
	}
	return s.defaults.ReplyLimit
}

// DefaultReplyLimit is the limit of an account that saved none.
func (s *SettingsStore) DefaultReplyLimit() Limit { return s.defaults.ReplyLimit }

// Enabled reports whether an alert kind is on.
func (s *SettingsStore) Enabled(k Kind) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur.Enabled(k)
}

// Spike returns the hand-off spike rule: the share and the minimum turns.
func (s *SettingsStore) Spike() (float64, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur.SpikeShare, s.cur.SpikeMinTurns
}

// Save validates, writes atomically and makes the settings current.
func (s *SettingsStore) Save(next Settings) error {
	next = clone(next)
	if err := next.Validate(); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(toFile(next), "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeAtomic(s.path, buf); err != nil {
		return err
	}
	s.cur = next
	return nil
}

func writeAtomic(path string, buf []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create alert settings dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".alert-settings-*.json")
	if err != nil {
		return fmt.Errorf("write alert settings: %w", err)
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
		return fmt.Errorf("write alert settings: %w", err)
	}
	return nil
}

func clone(s Settings) Settings {
	out := s
	out.Off = slices.Clone(s.Off)
	if s.ReplyLimits != nil {
		out.ReplyLimits = make(map[int64]Limit, len(s.ReplyLimits))
		for k, v := range s.ReplyLimits {
			out.ReplyLimits[k] = v
		}
	}
	return out
}
