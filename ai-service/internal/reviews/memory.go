package reviews

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Memory is a Store in memory (tests, and no database).
type Memory struct {
	mu   sync.Mutex
	imgs map[int64]Image
}

// Get implements Store.
func (m *Memory) Get(_ context.Context, accountID int64) (Image, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	img, ok := m.imgs[accountID]
	if !ok {
		return Image{AccountID: accountID}, false, nil
	}
	img.Data = append([]byte(nil), img.Data...)
	return img, true, nil
}

// Save implements Store.
func (m *Memory) Save(_ context.Context, img Image) error {
	ct, err := Check(img.Data, img.Caption)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.imgs == nil {
		m.imgs = map[int64]Image{}
	}
	img.ContentType, img.Caption, img.UpdatedAt = ct, strings.TrimSpace(img.Caption), time.Now()
	img.Data = append([]byte(nil), img.Data...)
	m.imgs[img.AccountID] = img
	return nil
}

// Delete implements Store.
func (m *Memory) Delete(_ context.Context, accountID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.imgs, accountID)
	return nil
}

// Has implements Store.
func (m *Memory) Has(_ context.Context, accountID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.imgs[accountID]
	return ok, nil
}
