package reviews

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.White)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCheck_AcceptsRealJPEGAndPNG(t *testing.T) {
	// Arrange
	jp, pn := jpegBytes(t), pngBytes(t, 3, 3)

	// Act
	ct1, err1 := Check(jp, "รีวิว")
	ct2, err2 := Check(pn, "")

	// Assert
	if err1 != nil || ct1 != "image/jpeg" || err2 != nil || ct2 != "image/png" {
		t.Fatalf("%q %v / %q %v", ct1, err1, ct2, err2)
	}
}

func TestCheck_Refuses(t *testing.T) {
	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	html := []byte("<html><script>alert(1)</script></html>")
	fakePNG := append([]byte("\x89PNG\r\n\x1a\n"), []byte("not really a png")...)
	cases := map[string]struct {
		data    []byte
		caption string
		want    error
	}{
		"empty":           {nil, "", ErrEmpty},
		"too large":       {make([]byte, MaxBytes+1), "", ErrTooLarge},
		"gif":             {gif, "", ErrType},
		"html":            {html, "", ErrType},
		"png magic only":  {fakePNG, "", ErrType},
		"long caption":    {pngBytes(t, 1, 1), strings.Repeat("ก", MaxCaptionRunes+1), ErrCaption},
		"NUL in caption":  {pngBytes(t, 1, 1), "a\x00b", ErrCaption},
		"too many pixels": {pngBytes(t, MaxSide+1, 1), "", ErrType},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			_, err := Check(c.data, c.caption)

			// Assert
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
		})
	}
}

func openDB(t *testing.T) Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := (&knowledge.Postgres{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return &Postgres{Pool: pool}
}

func TestStore_SaveGetReplaceDelete(t *testing.T) {
	stores := map[string]func(t *testing.T) Store{
		"memory":   func(*testing.T) Store { return &Memory{} },
		"postgres": openDB,
	}
	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			// Arrange
			ctx := context.Background()
			s := open(t)
			const acct = 777001
			_ = s.Delete(ctx, acct)
			t.Cleanup(func() { _ = s.Delete(ctx, acct) })

			// Act
			_, before, _ := s.Get(ctx, acct)
			err1 := s.Save(ctx, Image{AccountID: acct, Caption: "  รีวิวจริง  ", Data: jpegBytes(t)})
			err2 := s.Save(ctx, Image{AccountID: acct, Caption: "ใหม่", Data: pngBytes(t, 2, 2)})
			got, ok, err3 := s.Get(ctx, acct)
			bad := s.Save(ctx, Image{AccountID: acct, Data: []byte("<html>")})
			still, _, _ := s.Get(ctx, acct)
			_ = s.Delete(ctx, acct)
			_, after, _ := s.Get(ctx, acct)

			// Assert
			if before || err1 != nil || err2 != nil || err3 != nil || !ok || got.Caption != "ใหม่" || got.ContentType != "image/png" || got.Filename() != "reviews.png" {
				t.Fatalf("before %v errs %v %v %v ok %v got %+v", before, err1, err2, err3, ok, got.Caption)
			}
			if !errors.Is(bad, ErrType) || still.ContentType != "image/png" || after {
				t.Fatalf("bad %v still %q after %v", bad, still.ContentType, after)
			}
		})
	}
}
