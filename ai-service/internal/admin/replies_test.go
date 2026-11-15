package admin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/ratings"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

type memRatings struct {
	saved   map[int64]ratings.Rating
	saveErr error
}

func (m *memRatings) Save(_ context.Context, r ratings.Rating) error {
	if err := r.Clean(); err != nil {
		return err
	}
	if m.saveErr != nil {
		return m.saveErr
	}
	r.RatedAt = time.Now()
	m.saved[r.MessageID] = r
	return nil
}

func (m *memRatings) Rated(_ context.Context, acct int64, ids []int64) (map[int64]ratings.Rating, error) {
	out := map[int64]ratings.Rating{}
	for _, id := range ids {
		if r, ok := m.saved[id]; ok && r.AccountID == acct {
			out[id] = r
		}
	}
	return out, nil
}

func (m *memRatings) List(_ context.Context, acct int64, good *bool, limit int) ([]ratings.Rating, error) {
	var out []ratings.Rating
	for _, r := range m.saved {
		if r.AccountID == acct && (good == nil || r.Good == *good) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memRatings) Counts(_ context.Context, acct int64) (good, bad int, err error) {
	for _, r := range m.saved {
		if r.AccountID != acct {
			continue
		}
		if r.Good {
			good++
		} else {
			bad++
		}
	}
	return good, bad, nil
}

type fakeFeed struct {
	replies []ratings.Reply
	pages   []int
	err     error
}

func (f *fakeFeed) Recent(_ context.Context, _ int64, page int) ([]ratings.Reply, error) {
	f.pages = append(f.pages, page)
	return f.replies, f.err
}

func repliesEnv(t *testing.T, withDB bool, feed *fakeFeed) (*http.ServeMux, *memRatings) {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	mr := &memRatings{saved: map[int64]ratings.Rating{}}
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), ChatwootURL: "https://pronto.example"}
	if withDB {
		h.Ratings = mr
	}
	if feed != nil {
		h.Replies = feed
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, mr
}

func sampleFeed() *fakeFeed {
	at := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	return &fakeFeed{replies: []ratings.Reply{
		{ConversationID: 41, InboxID: 1, MessageID: 501, Customer: "ราคาเท่าไหร่ <i>", Bot: "3,900 บาทค่ะ", At: at},
		{ConversationID: 42, MessageID: 502, Customer: "", Bot: "สนใจรับลิงก์ไหมคะ", At: at.Add(-time.Hour)},
	}}
}

func ratePost(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/replies", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func rateForm(rating, reason string) url.Values {
	return url.Values{"account": {"1"}, "conversation": {"41"}, "message": {"501"}, "rating": {rating},
		"reason": {reason}, "note": {"ควรเสนอคูปอง"}, "customer": {"ราคาเท่าไหร่"}, "bot": {"3,900 บาทค่ะ"},
		"replied_at": {"1794700000"}}
}

func TestReplies_ToRateShowsRecentRepliesEscaped(t *testing.T) {
	// Arrange
	feed := sampleFeed()
	mux, _ := repliesEnv(t, true, feed)

	// Act
	rec := serve(mux, gapGet("/admin/replies?account=1&page=2"))

	// Assert
	body := rec.Body.String()
	if rec.Code != http.StatusOK || len(feed.pages) != 1 || feed.pages[0] != 2 {
		t.Fatalf("code=%d pages=%v", rec.Code, feed.pages)
	}
	for _, want := range []string{"ราคาเท่าไหร่ &lt;i&gt;", "3,900 บาทค่ะ", "สนใจรับลิงก์ไหมคะ", "the bot followed up",
		`href="https://pronto.example/app/accounts/1/conversations/41"`, `name="message" value="501"`,
		`value="missed_sale"`, "Newer chats", "Older chats"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "<i>") {
		t.Error("customer text must be escaped")
	}
}

func TestReplies_RatedRepliesLeaveToRate(t *testing.T) {
	// Arrange
	mux, _ := repliesEnv(t, true, sampleFeed())
	serve(mux, ratePost(rateForm("good", "")))

	// Act
	rec := serve(mux, gapGet("/admin/replies?account=1"))

	// Assert
	body := rec.Body.String()
	if strings.Contains(body, `name="message" value="501"`) || !strings.Contains(body, `name="message" value="502"`) ||
		!strings.Contains(body, "1 replies in these chats are already rated") {
		t.Fatalf("rated reply still offered:\n%s", body)
	}
}

func TestReplies_SaveBadWithReasonThenShownUnderRated(t *testing.T) {
	// Arrange
	mux, mr := repliesEnv(t, true, sampleFeed())

	// Act
	rec := serve(mux, ratePost(rateForm("bad", "missed_sale")))
	rated := serve(mux, gapGet("/admin/replies?account=1&view=rated&filter=bad"))

	// Assert
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/replies?account=1&done=bad" {
		t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	r := mr.saved[501]
	if r.Good || r.Reason != "missed_sale" || r.Note != "ควรเสนอคูปอง" || r.Bot != "3,900 บาทค่ะ" || r.RatedBy != "admin password" ||
		r.RepliedAt.Unix() != 1794700000 {
		t.Errorf("saved = %+v", r)
	}
	if body := rated.Body.String(); !strings.Contains(body, "Missed the sale") || !strings.Contains(body, `<option value="missed_sale" selected>`) {
		t.Errorf("rated view lacks the reason")
	}
}

func TestReplies_BadWithoutReasonIsNotSaved(t *testing.T) {
	// Arrange
	mux, mr := repliesEnv(t, true, sampleFeed())

	// Act
	rec := serve(mux, ratePost(rateForm("bad", "")))

	// Assert
	if rec.Code != http.StatusUnprocessableEntity || len(mr.saved) != 0 || !strings.Contains(rec.Body.String(), "pick what was wrong") {
		t.Fatalf("code=%d saved=%d", rec.Code, len(mr.saved))
	}
}

func TestReplies_BadInputChangesNothing(t *testing.T) {
	cases := map[string]func(url.Values){
		"bad account":   func(v url.Values) { v.Set("account", "oops") },
		"no message":    func(v url.Values) { v.Del("message") },
		"zero chat":     func(v url.Values) { v.Set("conversation", "0") },
		"odd rating":    func(v url.Values) { v.Set("rating", "meh") },
		"forged reason": func(v url.Values) { v.Set("reason", "x'); DROP TABLE reply_ratings;--") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mux, mr := repliesEnv(t, true, sampleFeed())
			form := rateForm("bad", "tone")
			change(form)

			// Act
			rec := serve(mux, ratePost(form))

			// Assert
			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity || len(mr.saved) != 0 {
				t.Errorf("code=%d saved=%d", rec.Code, len(mr.saved))
			}
		})
	}
}

func TestReplies_NeedsLoginAndSameOrigin(t *testing.T) {
	// Arrange
	mux, mr := repliesEnv(t, true, sampleFeed())
	anon := httptest.NewRequest(http.MethodGet, "/admin/replies", nil)
	cross := ratePost(rateForm("good", ""))
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	wrong := ratePost(rateForm("good", ""))
	wrong.SetBasicAuth(Username, "nope")

	// Act
	codes := []int{serve(mux, anon).Code, serve(mux, cross).Code, serve(mux, wrong).Code}

	// Assert
	if codes[0] != http.StatusUnauthorized || codes[1] != http.StatusForbidden || codes[2] != http.StatusUnauthorized || len(mr.saved) != 0 {
		t.Errorf("codes=%v saved=%d", codes, len(mr.saved))
	}
}

func TestReplies_OffWithoutDatabaseAndWithoutReader(t *testing.T) {
	// Arrange
	noDB, _ := repliesEnv(t, false, sampleFeed())
	noReader, _ := repliesEnv(t, true, nil)

	// Act
	page := serve(noDB, gapGet("/admin/replies"))
	post := serve(noDB, ratePost(rateForm("good", "")))
	reader := serve(noReader, gapGet("/admin/replies"))

	// Assert
	if !strings.Contains(page.Body.String(), "DATABASE_URL") || post.Code != http.StatusServiceUnavailable {
		t.Errorf("no database: page ok=%v post=%d", strings.Contains(page.Body.String(), "DATABASE_URL"), post.Code)
	}
	if !strings.Contains(reader.Body.String(), "CHATWOOT_READER_TOKEN") {
		t.Error("without the reader token the page should say how to turn it on")
	}
}

func TestReplies_ChatwootDownShowsAnError(t *testing.T) {
	// Arrange
	mux, _ := repliesEnv(t, true, &fakeFeed{err: errors.New("401")})

	// Act
	rec := serve(mux, gapGet("/admin/replies?account=1"))

	// Assert
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Could not read the chats from Chatwoot") {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestReplies_SaveFailureSaysSo(t *testing.T) {
	// Arrange
	mux, mr := repliesEnv(t, true, sampleFeed())
	mr.saveErr = errors.New("db down")

	// Act
	rec := serve(mux, ratePost(rateForm("good", "")))

	// Assert
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "Could not save the rating") {
		t.Fatalf("code=%d", rec.Code)
	}
}
