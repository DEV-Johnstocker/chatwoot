package images

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory is a Store in memory (tests).
type Memory struct {
	mu      sync.Mutex
	nextID  int64
	rows    map[int64]Image
	removed []removedCopy // oldest first
	lastRm  int64
}

type removedCopy struct {
	RemovedImage
	img Image
}

// List implements Store.
func (m *Memory) List(_ context.Context, accountID int64) ([]Meta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Meta
	for _, img := range m.rows {
		if img.AccountID == accountID {
			out = append(out, img.Meta)
		}
	}
	sort.Slice(out, func(i, j int) bool { return Key(out[i].Name) < Key(out[j].Name) })
	return out, nil
}

// Get implements Store.
func (m *Memory) Get(_ context.Context, accountID, id int64) (Image, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	img, ok := m.rows[id]
	if !ok || img.AccountID != accountID {
		return Image{}, ErrNotFound
	}
	img.Data = append([]byte(nil), img.Data...)
	return img, nil
}

// Find implements Store.
func (m *Memory) Find(ctx context.Context, accountID int64, name string) (Image, bool, error) {
	m.mu.Lock()
	var id int64
	for _, img := range m.rows {
		if img.AccountID == accountID && Key(img.Name) == Key(name) {
			id = img.ID
		}
	}
	m.mu.Unlock()
	if id == 0 {
		return Image{}, false, nil
	}
	img, err := m.Get(ctx, accountID, id)
	return img, err == nil, err
}

func (m *Memory) taken(accountID, id int64, name string) bool {
	for _, img := range m.rows {
		if img.AccountID == accountID && img.ID != id && Key(img.Name) == Key(name) {
			return true
		}
	}
	return false
}

// Add implements Store.
func (m *Memory) Add(_ context.Context, img Image) (int64, error) {
	meta, err := CleanText(img.Meta)
	if err != nil {
		return 0, err
	}
	ct, data, err := Prepare(img.Data)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[int64]Image{}
	}
	n := 0
	for _, r := range m.rows {
		if r.AccountID == meta.AccountID {
			n++
		}
	}
	switch {
	case n >= MaxPerAccount:
		return 0, ErrFull
	case m.taken(meta.AccountID, 0, meta.Name):
		return 0, ErrName
	}
	m.nextID++
	meta.ID, meta.ContentType, meta.Size, meta.UpdatedAt = m.nextID, ct, len(data), time.Now()
	m.rows[meta.ID] = Image{Meta: meta, Data: data}
	return meta.ID, nil
}

// Update implements Store.
func (m *Memory) Update(_ context.Context, meta Meta, data []byte) error {
	meta, err := CleanText(meta)
	if err != nil {
		return err
	}
	var ct string
	if data != nil {
		if ct, data, err = Prepare(data); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.rows[meta.ID]
	if !ok || old.AccountID != meta.AccountID {
		return ErrNotFound
	}
	if m.taken(meta.AccountID, meta.ID, meta.Name) {
		return ErrName
	}
	old.Name, old.When, old.Caption, old.UpdatedAt = meta.Name, meta.When, meta.Caption, time.Now()
	if data != nil {
		old.ContentType, old.Data, old.Size = ct, data, len(data)
	}
	m.rows[meta.ID] = old
	return nil
}

// Delete implements Store.
func (m *Memory) Delete(_ context.Context, accountID, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	img, ok := m.rows[id]
	if !ok || img.AccountID != accountID {
		return ErrNotFound
	}
	delete(m.rows, id)
	m.lastRm++
	m.removed = append(m.removed, removedCopy{img: img, RemovedImage: RemovedImage{ID: m.lastRm, ImageID: id, AccountID: accountID,
		Name: img.Name, When: img.When, Caption: img.Caption, Size: len(img.Data), RemovedAt: time.Now()}})
	// Keep the newest MaxRemovedKept of this account, none past RemovedKeepDays.
	n, cutoff := 0, removedCutoff()
	for i := len(m.removed) - 1; i >= 0; i-- {
		if m.removed[i].AccountID != accountID {
			continue
		}
		if n++; n > MaxRemovedKept || m.removed[i].RemovedAt.Before(cutoff) {
			m.removed = append(m.removed[:i], m.removed[i+1:]...)
		}
	}
	return nil
}

// Removed implements Store.
func (m *Memory) Removed(_ context.Context, accountID int64) ([]RemovedImage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []RemovedImage
	for i := len(m.removed) - 1; i >= 0; i-- {
		if m.removed[i].AccountID == accountID && !m.removed[i].RemovedAt.Before(removedCutoff()) {
			out = append(out, m.removed[i].RemovedImage)
		}
	}
	return out, nil
}

// Restore implements Store.
func (m *Memory) Restore(ctx context.Context, accountID, removedID int64) (int64, error) {
	m.mu.Lock()
	var img Image
	found := false
	for _, r := range m.removed {
		if r.ID == removedID && r.AccountID == accountID && !r.RemovedAt.Before(removedCutoff()) {
			img, found = r.img, true
		}
	}
	m.mu.Unlock()
	if !found {
		return 0, ErrNotFound
	}
	id, err := m.Add(ctx, img)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.removed {
		if r.ID == removedID {
			m.removed = append(m.removed[:i], m.removed[i+1:]...)
			break
		}
	}
	return id, nil
}

// Forget implements Store.
func (m *Memory) Forget(_ context.Context, accountID, removedID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.removed {
		if r.ID == removedID && r.AccountID == accountID {
			m.removed = append(m.removed[:i], m.removed[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
