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

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

type fakeGaps struct {
	open    map[int64]gaps.Gap // conversation → gap still labelled kb-gap
	accts   []int64            // accounts asked for
	done    []int64
	listErr error
	doneErr error
}

func (f *fakeGaps) List(_ context.Context, acct int64, limit int) ([]gaps.Gap, error) {
	f.accts = append(f.accts, acct)
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []gaps.Gap
	for _, g := range f.open {
		out = append(out, g)
	}
	return out, nil
}

func (f *fakeGaps) Done(_ context.Context, acct, conv int64) error {
	if f.doneErr != nil {
		return f.doneErr
	}
	if _, ok := f.open[conv]; !ok {
		return gaps.ErrNotAGap
	}
	delete(f.open, conv)
	f.done = append(f.done, conv)
	return nil
}

func gapsEnv(t *testing.T, fg *fakeGaps) (*http.ServeMux, *memStore) {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	ms := &memStore{}
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Knowledge:   &knowledge.Service{Store: ms, Embedder: constEmbedder{"m1"}, TopK: 4, ChunkRunes: 800},
		ChatwootURL: "https://pronto.example"}
	if fg != nil {
		h.Gaps = fg
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, ms
}

func sampleGaps() *fakeGaps {
	return &fakeGaps{open: map[int64]gaps.Gap{
		41: {ConversationID: 41, InboxID: 2, NoteFound: true, At: time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC),
			Asked: "คอร์สมีใบประกาศไหม <b>", Wanted: "อยากรู้เรื่องใบประกาศ", StaffReply: "มีค่ะ ได้เมื่อเรียนจบ"},
	}}
}

func gapGet(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.SetBasicAuth(Username, pw)
	return r
}

func gapPost(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/gaps", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func TestGaps_ListShowsQuestionReplyAndChatLink(t *testing.T) {
	// Arrange
	fg := sampleGaps()
	mux, _ := gapsEnv(t, fg)

	// Act
	rec := serve(mux, gapGet("/admin/gaps?account=3"))

	// Assert
	body := rec.Body.String()
	// The page reads the list once, and the menu badge may read it too.
	if rec.Code != http.StatusOK || len(fg.accts) == 0 || fg.accts[0] != 3 || fg.accts[len(fg.accts)-1] != 3 {
		t.Fatalf("code=%d accts=%v", rec.Code, fg.accts)
	}
	for _, want := range []string{"คอร์สมีใบประกาศไหม &lt;b&gt;", "อยากรู้เรื่องใบประกาศ",
		`href="https://pronto.example/app/accounts/3/conversations/41"`, `value="done"`,
		`action="/admin/knowledge"`, `name="gap" value="41"`, `value="add_faq"`,
		`name="question" type="text" maxlength="300" required value="คอร์สมีใบประกาศไหม &lt;b&gt;"`,
		`>มีค่ะ ได้เมื่อเรียนจบ</textarea>`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "<b>") {
		t.Error("customer text must be escaped")
	}
}

func TestGaps_OffWithoutReaderToken(t *testing.T) {
	// Arrange
	mux, _ := gapsEnv(t, nil)

	// Act
	page := serve(mux, gapGet("/admin/gaps"))
	post := serve(mux, gapPost(url.Values{"account": {"1"}, "conversation": {"41"}, "action": {"done"}}))

	// Assert
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "CHATWOOT_READER_TOKEN") {
		t.Errorf("page code=%d", page.Code)
	}
	if post.Code != http.StatusServiceUnavailable {
		t.Errorf("post code=%d", post.Code)
	}
}

func TestGaps_ChatwootDownShowsAnError(t *testing.T) {
	// Arrange
	mux, _ := gapsEnv(t, &fakeGaps{listErr: errors.New("401")})

	// Act
	rec := serve(mux, gapGet("/admin/gaps?account=1"))

	// Assert
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Could not read the chats from Chatwoot") {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestGaps_NeedsLoginAndSameOrigin(t *testing.T) {
	// Arrange
	fg := sampleGaps()
	mux, _ := gapsEnv(t, fg)
	anon := httptest.NewRequest(http.MethodGet, "/admin/gaps", nil)
	cross := gapPost(url.Values{"account": {"1"}, "conversation": {"41"}, "action": {"done"}})
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	wrong := gapPost(url.Values{"account": {"1"}, "conversation": {"41"}, "action": {"done"}})
	wrong.SetBasicAuth(Username, "nope")

	// Act
	codes := []int{serve(mux, anon).Code, serve(mux, cross).Code, serve(mux, wrong).Code}

	// Assert
	if codes[0] != http.StatusUnauthorized || codes[1] != http.StatusForbidden || codes[2] != http.StatusUnauthorized {
		t.Errorf("codes = %v", codes)
	}
	if len(fg.done) != 0 || fg.accts != nil {
		t.Errorf("a refused request reached Chatwoot: done=%v lists=%v", fg.done, fg.accts)
	}
}

func TestGaps_BadInputChangesNothing(t *testing.T) {
	cases := map[string]url.Values{
		"bad account":      {"account": {"oops"}, "conversation": {"41"}, "action": {"done"}},
		"no account":       {"conversation": {"41"}, "action": {"done"}},
		"bad conversation": {"account": {"1"}, "conversation": {"-4"}, "action": {"done"}},
		"unknown action":   {"account": {"1"}, "conversation": {"41"}, "action": {"delete"}},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			fg := sampleGaps()
			mux, _ := gapsEnv(t, fg)

			// Act
			rec := serve(mux, gapPost(form))

			// Assert
			if rec.Code != http.StatusBadRequest || len(fg.done) != 0 {
				t.Errorf("code=%d done=%v", rec.Code, fg.done)
			}
		})
	}
}

func TestGaps_DoneTakesTheChatOffTheList(t *testing.T) {
	// Arrange
	fg := sampleGaps()
	mux, _ := gapsEnv(t, fg)

	// Act
	rec := serve(mux, gapPost(url.Values{"account": {"1"}, "conversation": {"41"}, "action": {"done"}}))
	again := serve(mux, gapPost(url.Values{"account": {"1"}, "conversation": {"41"}, "action": {"done"}}))

	// Assert
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/gaps?account=1&done=closed" {
		t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	if len(fg.done) != 1 || fg.done[0] != 41 {
		t.Errorf("done = %v", fg.done)
	}
	if again.Code != http.StatusConflict || !strings.Contains(again.Body.String(), "no longer on the gap list") {
		t.Errorf("second click: code=%d", again.Code)
	}
}

func TestGaps_DoneWhenChatwootFailsSaysNothingChanged(t *testing.T) {
	// Arrange
	fg := sampleGaps()
	fg.doneErr = errors.New("timeout")
	mux, _ := gapsEnv(t, fg)

	// Act
	rec := serve(mux, gapPost(url.Values{"account": {"1"}, "conversation": {"41"}, "action": {"done"}}))

	// Assert
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Nothing was changed") || len(fg.open) != 1 {
		t.Fatalf("code=%d open=%d", rec.Code, len(fg.open))
	}
}

func TestGaps_EditedAnswerFromTheCardIsAddedAndClosesTheGap(t *testing.T) {
	// Arrange
	fg := sampleGaps()
	mux, ms := gapsEnv(t, fg)

	// Act: the owner edits both fields on the card, then adds it.
	add := serve(mux, kform(url.Values{"action": {"add_faq"}, "account": {"1"}, "gap": {"41"},
		"question": {"คอร์สมีใบประกาศไหม"}, "answer": {"มีค่ะ ได้เมื่อเรียนจบ"}}))

	// Assert
	if add.Code != http.StatusSeeOther || add.Header().Get("Location") != "/admin/gaps?account=1&done=answered" {
		t.Fatalf("add code=%d loc=%q body=%s", add.Code, add.Header().Get("Location"), add.Body)
	}
	if len(ms.docs) != 1 || ms.docs[0].Title != "คอร์สมีใบประกาศไหม" || ms.docs[0].Content != "มีค่ะ ได้เมื่อเรียนจบ" || len(fg.done) != 1 {
		t.Errorf("docs=%+v done=%v", ms.docs, fg.done)
	}
}

func TestGaps_AnswerSavedEvenWhenTheLabelCannotChange(t *testing.T) {
	// Arrange
	fg := sampleGaps()
	fg.doneErr = errors.New("chatwoot down")
	mux, ms := gapsEnv(t, fg)

	// Act
	add := serve(mux, kform(url.Values{"action": {"add_faq"}, "account": {"1"}, "gap": {"41"},
		"question": {"q"}, "answer": {"a"}}))

	// Assert
	if add.Header().Get("Location") != "/admin/gaps?account=1&done=answered-kept" || len(ms.docs) != 1 {
		t.Fatalf("loc=%q docs=%d", add.Header().Get("Location"), len(ms.docs))
	}
}

func TestGaps_PlainFAQAddIsUnchanged(t *testing.T) {
	// Arrange: no gap field, a gap field that is not a number.
	fg := sampleGaps()
	mux, _ := gapsEnv(t, fg)

	// Act
	plain := serve(mux, kform(url.Values{"action": {"add_faq"}, "account": {"1"}, "question": {"q"}, "answer": {"a"}}))
	odd := serve(mux, kform(url.Values{"action": {"add_faq"}, "account": {"1"}, "gap": {"x"}, "question": {"q2"}, "answer": {"a2"}}))

	// Assert
	for _, rec := range []*httptest.ResponseRecorder{plain, odd} {
		if !strings.HasPrefix(rec.Header().Get("Location"), "/admin/knowledge?account=1&done=added") {
			t.Errorf("loc = %q", rec.Header().Get("Location"))
		}
	}
	if len(fg.done) != 0 {
		t.Errorf("done = %v", fg.done)
	}
}
