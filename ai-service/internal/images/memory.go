package images

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory is a Store in memory (tests).
type Memory struct {
	mu     sync.Mutex
	nextID int64
	rows   map[int64]Image
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
	if img, ok := m.rows[id]; ok && img.AccountID == accountID {
		delete(m.rows, id)
	}
	return nil
}
