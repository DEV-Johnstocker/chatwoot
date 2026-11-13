package reviews

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores images in review_images (schema v17).
type Postgres struct {
	Pool *pgxpool.Pool
}

// Get implements Store.
func (s *Postgres) Get(ctx context.Context, accountID int64) (Image, bool, error) {
	img := Image{AccountID: accountID}
	err := s.Pool.QueryRow(ctx, `SELECT caption, content_type, data, updated_at FROM review_images WHERE account_id = $1`, accountID).
		Scan(&img.Caption, &img.ContentType, &img.Data, &img.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{AccountID: accountID}, false, nil
	}
	return img, err == nil, err
}

// Save implements Store.
func (s *Postgres) Save(ctx context.Context, img Image) error {
	ct, err := Check(img.Data, img.Caption)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO review_images (account_id, caption, content_type, data, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (account_id) DO UPDATE SET caption = EXCLUDED.caption, content_type = EXCLUDED.content_type,
		data = EXCLUDED.data, updated_at = now()`, img.AccountID, strings.TrimSpace(img.Caption), ct, img.Data)
	return err
}

// Delete implements Store.
func (s *Postgres) Delete(ctx context.Context, accountID int64) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM review_images WHERE account_id = $1`, accountID)
	return err
}

// Has implements Store.
func (s *Postgres) Has(ctx context.Context, accountID int64) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM review_images WHERE account_id = $1)`, accountID).Scan(&ok)
	return ok, err
}
