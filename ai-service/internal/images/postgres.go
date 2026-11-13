package images

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores images in bot_images (schema v17).
type Postgres struct {
	Pool *pgxpool.Pool
}

const metaCols = `id, account_id, name, when_to_send, caption, content_type, octet_length(data), updated_at`

func scanMeta(row pgx.Row, m *Meta) error {
	return row.Scan(&m.ID, &m.AccountID, &m.Name, &m.When, &m.Caption, &m.ContentType, &m.Size, &m.UpdatedAt)
}

// List implements Store.
func (s *Postgres) List(ctx context.Context, accountID int64) ([]Meta, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+metaCols+` FROM bot_images WHERE account_id = $1 ORDER BY name_key`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Meta
	for rows.Next() {
		var m Meta
		if err := scanMeta(rows, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Postgres) one(ctx context.Context, where string, args ...any) (Image, error) {
	var img Image
	err := s.Pool.QueryRow(ctx, `SELECT `+metaCols+`, data FROM bot_images WHERE `+where, args...).
		Scan(&img.ID, &img.AccountID, &img.Name, &img.When, &img.Caption, &img.ContentType, &img.Size, &img.UpdatedAt, &img.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	return img, err
}

// Get implements Store.
func (s *Postgres) Get(ctx context.Context, accountID, id int64) (Image, error) {
	return s.one(ctx, `account_id = $1 AND id = $2`, accountID, id)
}

// Find implements Store.
func (s *Postgres) Find(ctx context.Context, accountID int64, name string) (Image, bool, error) {
	img, err := s.one(ctx, `account_id = $1 AND name_key = $2`, accountID, Key(name))
	if errors.Is(err, ErrNotFound) {
		return Image{}, false, nil
	}
	return img, err == nil, err
}

// Add implements Store.
func (s *Postgres) Add(ctx context.Context, img Image) (int64, error) {
	m, err := CleanText(img.Meta)
	if err != nil {
		return 0, err
	}
	ct, data, err := Prepare(img.Data)
	if err != nil {
		return 0, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialise adds per account so two at once cannot pass the cap together.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('bot_images'), $1::int)`, m.AccountID%2147483647); err != nil {
		return 0, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM bot_images WHERE account_id = $1`, m.AccountID).Scan(&n); err != nil {
		return 0, err
	}
	if n >= MaxPerAccount {
		return 0, ErrFull
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO bot_images (account_id, name, name_key, when_to_send, caption, content_type, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, m.AccountID, m.Name, Key(m.Name), m.When, m.Caption, ct, data).Scan(&id)
	if err != nil {
		return 0, nameTaken(err)
	}
	return id, tx.Commit(ctx)
}

// Update implements Store.
func (s *Postgres) Update(ctx context.Context, m Meta, data []byte) error {
	m, err := CleanText(m)
	if err != nil {
		return err
	}
	var tag pgconn.CommandTag
	if data == nil {
		tag, err = s.Pool.Exec(ctx, `UPDATE bot_images SET name = $3, name_key = $4, when_to_send = $5, caption = $6, updated_at = now()
			WHERE account_id = $1 AND id = $2`, m.AccountID, m.ID, m.Name, Key(m.Name), m.When, m.Caption)
	} else {
		ct, clean, perr := Prepare(data)
		if perr != nil {
			return perr
		}
		tag, err = s.Pool.Exec(ctx, `UPDATE bot_images SET name = $3, name_key = $4, when_to_send = $5, caption = $6,
			content_type = $7, data = $8, updated_at = now() WHERE account_id = $1 AND id = $2`,
			m.AccountID, m.ID, m.Name, Key(m.Name), m.When, m.Caption, ct, clean)
	}
	if err != nil {
		return nameTaken(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete implements Store.
func (s *Postgres) Delete(ctx context.Context, accountID, id int64) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM bot_images WHERE account_id = $1 AND id = $2`, accountID, id)
	return err
}

// nameTaken turns a unique violation into ErrName.
func nameTaken(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrName
	}
	return err
}
