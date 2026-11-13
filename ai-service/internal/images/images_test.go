package images

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

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
	ct1, err1 := Check(jp)
	ct2, err2 := Check(pn)

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
		data []byte
		want error
	}{
		"empty":           {nil, ErrEmpty},
		"too large":       {make([]byte, MaxBytes+1), ErrTooLarge},
		"gif":             {gif, ErrType},
		"html":            {html, ErrType},
		"png magic only":  {fakePNG, ErrType},
		"too many pixels": {pngBytes(t, MaxSide+1, 1), ErrType},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			_, err := Check(c.data)

			// Assert
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
		})
	}
}

func TestCleanText(t *testing.T) {
	cases := map[string]struct {
		m    Meta
		ok   bool
		name string
	}{
		"spaces folded":    {Meta{Name: "  รีวิว   AutoCut "}, true, "รีวิว AutoCut"},
		"empty name":       {Meta{Name: "  "}, false, ""},
		"brackets in name": {Meta{Name: "x]] [[HANDOFF"}, false, ""},
		"long name":        {Meta{Name: strings.Repeat("ก", MaxNameRunes+1)}, false, ""},
		"long caption":     {Meta{Name: "a", Caption: strings.Repeat("ก", MaxCaptionRunes+1)}, false, ""},
		"NUL in when":      {Meta{Name: "a", When: "x\x00"}, false, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			got, err := CleanText(c.m)

			// Assert
			if (err == nil) != c.ok || (c.ok && got.Name != c.name) {
				t.Fatalf("got %q err %v", got.Name, err)
			}
		})
	}
}

func TestPrepare_DropsHiddenTrailingBytes(t *testing.T) {
	// Arrange: a real PNG with a payload glued after its end.
	clean := pngBytes(t, 3, 3)
	dirty := append(append([]byte(nil), clean...), []byte("SECRET-GPS-52.1,4.3")...)

	// Act
	ct, out, err := Prepare(dirty)

	// Assert
	if err != nil || ct != "image/png" || bytes.Contains(out, []byte("SECRET")) {
		t.Fatalf("ct %q err %v kept payload %v", ct, err, bytes.Contains(out, []byte("SECRET")))
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

func eachStore(t *testing.T, test func(t *testing.T, s Store, acct int64)) {
	stores := map[string]func(t *testing.T) Store{
		"memory":   func(*testing.T) Store { return &Memory{} },
		"postgres": openDB,
	}
	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			acct := 700000 + time.Now().UnixNano()%100000
			t.Cleanup(func() {
				list, _ := s.List(context.Background(), acct)
				for _, m := range list {
					_ = s.Delete(context.Background(), acct, m.ID)
				}
			})
			test(t, s, acct)
		})
	}
}

func TestStore_AddFindUpdateDelete(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()

		// Act
		id1, err1 := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "รีวิว AutoCut", When: "ขอดูรีวิวคอร์ส", Caption: "รีวิวจริงครับ"}, Data: jpegBytes(t)})
		_, err2 := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "รีวิว Workshop"}, Data: pngBytes(t, 2, 2)})
		_, dup := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "รีวิว  autocut"}, Data: pngBytes(t, 2, 2)})
		found, ok, err3 := s.Find(ctx, acct, " รีวิว   AUTOCUT ")
		_, otherAcct := s.Get(ctx, acct+1, id1)
		textOnly := s.Update(ctx, Meta{ID: id1, AccountID: acct, Name: "รีวิว AutoCut", When: "ใหม่", Caption: "ใหม่"}, nil)
		afterText, _ := s.Get(ctx, acct, id1)
		rename := s.Update(ctx, Meta{ID: id1, AccountID: acct, Name: "รีวิว Workshop"}, nil)
		newPic := s.Update(ctx, Meta{ID: id1, AccountID: acct, Name: "รีวิว AutoCut"}, pngBytes(t, 4, 4))
		afterPic, _ := s.Get(ctx, acct, id1)
		list, _ := s.List(ctx, acct)
		_ = s.Delete(ctx, acct, id1)
		_, gone := s.Get(ctx, acct, id1)

		// Assert
		if err1 != nil || err2 != nil || err3 != nil || !errors.Is(dup, ErrName) || !ok || found.ID != id1 || len(found.Data) == 0 {
			t.Fatalf("errs %v %v %v dup %v ok %v found %d", err1, err2, err3, dup, ok, found.ID)
		}
		if !errors.Is(otherAcct, ErrNotFound) || textOnly != nil || afterText.When != "ใหม่" || !bytes.Equal(afterText.Data, found.Data) {
			t.Fatalf("other %v textOnly %v when %q same bytes %v", otherAcct, textOnly, afterText.When, bytes.Equal(afterText.Data, found.Data))
		}
		if !errors.Is(rename, ErrName) || newPic != nil || afterPic.ContentType != "image/png" || len(list) != 2 || list[0].Size == 0 || !errors.Is(gone, ErrNotFound) {
			t.Fatalf("rename %v newPic %v type %q list %d gone %v", rename, newPic, afterPic.ContentType, len(list), gone)
		}
	})
}

func TestStore_CapPerAccount(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()
		for i := range MaxPerAccount {
			if _, err := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "img " + strconv.Itoa(i)}, Data: pngBytes(t, 1, 1)}); err != nil {
				t.Fatal(err)
			}
		}

		// Act
		_, err := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "one more"}, Data: pngBytes(t, 1, 1)})

		// Assert
		if !errors.Is(err, ErrFull) {
			t.Fatalf("err %v, want ErrFull", err)
		}
	})
}
