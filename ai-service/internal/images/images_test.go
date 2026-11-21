package images

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log/slog"
	"math/rand/v2"
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
			// A random account per test: a clock-based one repeated on
			// clocks that count whole microseconds, so one test saw another's
			// removed copies.
			acct := 700000 + rand.Int64N(1_000_000_000)
			t.Cleanup(func() {
				list, _ := s.List(context.Background(), acct)
				for _, m := range list {
					_ = s.Delete(context.Background(), acct, m.ID)
				}
				if pg, ok := s.(*Postgres); ok {
					_, _ = pg.Pool.Exec(context.Background(), `DELETE FROM bot_images_removed WHERE account_id IN ($1, $2)`, acct, acct+1)
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

// A removed picture is kept (QA F-C-03): it can be listed and put back with
// the same checks as an upload, by its own account only, and only once.
func TestStore_RemoveKeepsPictureAndRestores(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()
		id, err := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "รีวิว AutoCut", When: "ขอดูรีวิว", Caption: "รีวิวจริง"}, Data: jpegBytes(t)})
		if err != nil {
			t.Fatal(err)
		}
		orig, _ := s.Get(ctx, acct, id)
		if err := s.Delete(ctx, acct, id); err != nil {
			t.Fatal(err)
		}

		// Act
		removed, listErr := s.Removed(ctx, acct)
		otherList, _ := s.Removed(ctx, acct+1)
		var rid int64
		if len(removed) == 1 {
			rid = removed[0].ID
		}
		_, otherAcct := s.Restore(ctx, acct+1, rid)
		newID, restoreErr := s.Restore(ctx, acct, rid)
		back, getErr := s.Get(ctx, acct, newID)
		_, again := s.Restore(ctx, acct, rid)
		after, _ := s.Removed(ctx, acct)

		// Assert
		if listErr != nil || len(removed) != 1 || removed[0].Name != "รีวิว AutoCut" || removed[0].ImageID != id || removed[0].Size == 0 || removed[0].RemovedAt.IsZero() {
			t.Fatalf("removed %+v err %v", removed, listErr)
		}
		if len(otherList) != 0 || !errors.Is(otherAcct, ErrNotFound) {
			t.Fatalf("other account sees %d, restore %v", len(otherList), otherAcct)
		}
		if restoreErr != nil || getErr != nil || back.Name != orig.Name || back.When != orig.When || back.Caption != orig.Caption || back.ContentType != orig.ContentType || len(back.Data) == 0 {
			t.Fatalf("restore %v get %v back %+v", restoreErr, getErr, back.Meta)
		}
		if !errors.Is(again, ErrNotFound) || len(after) != 0 {
			t.Fatalf("second restore %v, still removed %d", again, len(after))
		}
	})
}

// Putting a picture back follows the upload rules: the name must be free and
// the account under its cap; a refused restore keeps the removed copy.
func TestStore_RestoreRefusedKeepsRemovedCopy(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()
		id, _ := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "price list"}, Data: pngBytes(t, 2, 2)})
		_ = s.Delete(ctx, acct, id)
		if _, err := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "Price  List"}, Data: pngBytes(t, 2, 2)}); err != nil {
			t.Fatal(err)
		}
		removed, _ := s.Removed(ctx, acct)
		if len(removed) != 1 {
			t.Fatalf("removed %d", len(removed))
		}

		// Act
		_, err := s.Restore(ctx, acct, removed[0].ID)
		kept, _ := s.Removed(ctx, acct)

		// Assert
		if !errors.Is(err, ErrName) || len(kept) != 1 {
			t.Fatalf("err %v kept %d", err, len(kept))
		}
	})
}

// Only the newest MaxRemovedKept removed pictures are kept per account.
func TestStore_RemovedIsCapped(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()
		for i := range MaxRemovedKept + 2 {
			id, err := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "img " + strconv.Itoa(i)}, Data: pngBytes(t, 1, 1)})
			if err != nil {
				t.Fatal(err)
			}
			_ = s.Delete(ctx, acct, id)
		}

		// Act
		removed, err := s.Removed(ctx, acct)

		// Assert
		if err != nil || len(removed) != MaxRemovedKept || removed[0].Name != "img "+strconv.Itoa(MaxRemovedKept+1) {
			t.Fatalf("err %v removed %d first %+v", err, len(removed), removed)
		}
	})
}

// The owner can erase a removed picture for good (it may show a customer's
// name), only their own, and it cannot be put back afterwards.
func TestStore_ForgetRemovedErasesIt(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()
		id, _ := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "รีวิวมีชื่อลูกค้า"}, Data: pngBytes(t, 2, 2)})
		_ = s.Delete(ctx, acct, id)
		removed, _ := s.Removed(ctx, acct)

		// Act
		other := s.Forget(ctx, acct+1, removed[0].ID)
		err := s.Forget(ctx, acct, removed[0].ID)
		after, _ := s.Removed(ctx, acct)
		_, restore := s.Restore(ctx, acct, removed[0].ID)
		again := s.Forget(ctx, acct, removed[0].ID)

		// Assert
		if !errors.Is(other, ErrNotFound) || err != nil || len(after) != 0 || !errors.Is(restore, ErrNotFound) || !errors.Is(again, ErrNotFound) {
			t.Fatalf("other %v forget %v left %d restore %v again %v", other, err, len(after), restore, again)
		}
	})
}

// Removed pictures older than RemovedKeepDays are no longer listed or
// restorable, and the next removal erases them.
func TestStore_RemovedExpires(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()
		id, _ := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "old"}, Data: pngBytes(t, 2, 2)})
		_ = s.Delete(ctx, acct, id)
		switch st := s.(type) {
		case *Memory:
			st.removed[0].RemovedAt = time.Now().AddDate(0, 0, -RemovedKeepDays-1)
		case *Postgres:
			if _, err := st.Pool.Exec(ctx, `UPDATE bot_images_removed SET removed_at = now() - make_interval(days => $2 + 1) WHERE account_id = $1`, acct, RemovedKeepDays); err != nil {
				t.Fatal(err)
			}
		}
		old, _ := s.Removed(ctx, acct)
		id2, _ := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "new"}, Data: pngBytes(t, 2, 2)})

		// Act
		_ = s.Delete(ctx, acct, id2)
		removed, err := s.Removed(ctx, acct)

		// Assert
		if len(old) != 0 {
			t.Fatalf("expired copy still listed: %+v", old)
		}
		if err != nil || len(removed) != 1 || removed[0].Name != "new" {
			t.Fatalf("err %v removed %+v", err, removed)
		}
		if st, ok := s.(*Postgres); ok {
			var n int
			_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM bot_images_removed WHERE account_id = $1`, acct).Scan(&n)
			if n != 1 {
				t.Fatalf("expired row not erased: %d rows", n)
			}
		}
	})
}

// Expired copies are erased for every account on a timer, not only when the
// same account removes another picture: "deleted for good after 30 days"
// must hold even for an account that never removes a picture again.
func TestStore_PurgeExpiredErasesEveryAccount(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()
		other := acct + 1
		for _, a := range []int64{acct, other} {
			id, _ := s.Add(ctx, Image{Meta: Meta{AccountID: a, Name: "old"}, Data: pngBytes(t, 2, 2)})
			_ = s.Delete(ctx, a, id)
		}
		switch st := s.(type) {
		case *Memory:
			for i := range st.removed {
				st.removed[i].RemovedAt = time.Now().AddDate(0, 0, -RemovedKeepDays-1)
			}
		case *Postgres:
			if _, err := st.Pool.Exec(ctx, `UPDATE bot_images_removed SET removed_at = now() - make_interval(days => $3 + 1) WHERE account_id IN ($1, $2)`, acct, other, RemovedKeepDays); err != nil {
				t.Fatal(err)
			}
		}
		id, _ := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "fresh"}, Data: pngBytes(t, 2, 2)})
		_ = s.Delete(ctx, acct, id) // the account's own prune also runs here
		p := s.(Purger)

		// Act
		n, err := p.PurgeExpired(ctx)

		// Assert
		if err != nil || n < 1 {
			t.Fatalf("PurgeExpired = %d, %v; want the other account's expired copy erased", n, err)
		}
		kept := 0
		switch st := s.(type) {
		case *Memory:
			for _, r := range st.removed {
				if r.AccountID == acct || r.AccountID == other {
					kept++
				}
			}
		case *Postgres:
			_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM bot_images_removed WHERE account_id IN ($1, $2)`, acct, other).Scan(&kept)
			_, _ = st.Pool.Exec(ctx, `DELETE FROM bot_images_removed WHERE account_id IN ($1, $2)`, acct, other)
		}
		if kept != 1 {
			t.Fatalf("%d removed copies left, want only the fresh one", kept)
		}
	})
}

func TestRunPurge_PurgesAtStartAndStops(t *testing.T) {
	// Arrange
	m := &Memory{}
	ctx, cancel := context.WithCancel(context.Background())
	id, _ := m.Add(ctx, Image{Meta: Meta{AccountID: 1, Name: "old"}, Data: pngBytes(t, 2, 2)})
	_ = m.Delete(ctx, 1, id)
	m.removed[0].RemovedAt = time.Now().AddDate(0, 0, -RemovedKeepDays-1)
	done := make(chan struct{})

	// Act
	go func() { RunPurge(ctx, m, time.Hour, slog.New(slog.DiscardHandler)); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.mu.Lock()
		left := len(m.removed)
		m.mu.Unlock()
		if left == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()

	// Assert
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPurge did not stop when its context ended")
	}
	if len(m.removed) != 0 {
		t.Fatalf("expired copy not purged at start: %d left", len(m.removed))
	}
}

// The "use in follow-ups" tick is saved, changed, listed, and kept when a
// picture is removed and put back from History.
func TestStore_FollowUpTickSurvivesUpdateAndRestore(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store, acct int64) {
		// Arrange
		ctx := context.Background()
		id, err := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "รีวิว Workshop", FollowUp: true}, Data: jpegBytes(t)})
		if err != nil {
			t.Fatal(err)
		}
		other, err := s.Add(ctx, Image{Meta: Meta{AccountID: acct, Name: "รีวิว AutoCut"}, Data: pngBytes(t, 2, 2)})
		if err != nil {
			t.Fatal(err)
		}

		// Act
		added, _ := s.Get(ctx, acct, id)
		untick := s.Update(ctx, Meta{ID: id, AccountID: acct, Name: "รีวิว Workshop", FollowUp: false}, nil)
		afterUntick, _ := s.Get(ctx, acct, id)
		tick := s.Update(ctx, Meta{ID: other, AccountID: acct, Name: "รีวิว AutoCut", FollowUp: true}, nil)
		list, _ := s.List(ctx, acct)
		_ = s.Delete(ctx, acct, other)
		removed, _ := s.Removed(ctx, acct)
		var newID int64
		var restoreErr error
		if len(removed) == 1 {
			newID, restoreErr = s.Restore(ctx, acct, removed[0].ID)
		}
		back, _ := s.Get(ctx, acct, newID)

		// Assert
		if !added.FollowUp || untick != nil || afterUntick.FollowUp || tick != nil {
			t.Fatalf("added %v untick %v after %v tick %v", added.FollowUp, untick, afterUntick.FollowUp, tick)
		}
		ticked := 0
		for _, m := range list {
			if m.FollowUp {
				ticked++
			}
		}
		if ticked != 1 || len(removed) != 1 || !removed[0].FollowUp {
			t.Fatalf("ticked in list %d, removed %+v", ticked, removed)
		}
		if restoreErr != nil || !back.FollowUp {
			t.Fatalf("restore %v, tick after restore %v", restoreErr, back.FollowUp)
		}
	})
}
