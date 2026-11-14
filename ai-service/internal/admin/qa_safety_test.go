package admin

// Tests for the data-safety findings of the 2026-11-14 QA run (#1):
// F-C-06, F-C-02, F-C-01, F-A-01/F-D-05 and F-A-03.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

// F-C-06: Enter in a text box submits the form with its first submit button.
func TestImages_EnterInEditFormCanOnlySave(t *testing.T) {
	// Arrange
	mux, _, _ := imageEnv(t)
	serve(mux, imagePost(t, map[string]string{"account": "1", "action": "add", "name": "รีวิว AutoCut"}, testPNG(t)))

	// Act
	page := serve(mux, getPath("/admin/images?account=1")).Body.String()

	// Assert
	forms := regexp.MustCompile(`(?s)<form[^>]*class="img-form".*?</form>`).FindAllString(page, -1)
	if len(forms) != 1 {
		t.Fatalf("edit forms = %d, want 1", len(forms))
	}
	first := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(forms[0])
	if !strings.Contains(first, `value="save"`) || strings.Contains(forms[0], `value="remove"`) {
		t.Errorf("the edit form must hold only Save; first button %q", first)
	}
}

func TestImages_RemoveNeedsConfirmAndKeepsDetails(t *testing.T) {
	// Arrange
	ctx := context.Background()
	mux, im, ev := imageEnv(t)
	serve(mux, imagePost(t, map[string]string{"account": "1", "action": "add", "name": "รีวิว AutoCut", "when": "ขอดูรีวิว", "caption": "รีวิวจริง"}, testPNG(t)))
	list, _ := im.List(ctx, 1)
	id := strconv.FormatInt(list[0].ID, 10)

	// Act
	unconfirmed := serve(mux, imagePost(t, map[string]string{"account": "1", "id": id, "action": "remove"}, nil))
	still, _ := im.List(ctx, 1)
	confirmed := serve(mux, imagePost(t, map[string]string{"account": "1", "id": id, "action": "remove", "confirm": "yes"}, nil))
	after, _ := im.List(ctx, 1)

	// Assert
	if unconfirmed.Code != http.StatusBadRequest || len(still) != 1 {
		t.Fatalf("remove without confirm: code %d, images left %d", unconfirmed.Code, len(still))
	}
	if confirmed.Code != http.StatusSeeOther || len(after) != 0 {
		t.Fatalf("confirmed remove: code %d, images left %d", confirmed.Code, len(after))
	}
	last := ev.events[len(ev.events)-1]
	if last.Action != "bot image removed" || last.Before == nil {
		t.Errorf("history must keep the removed picture's texts: %+v", last)
	}
}

// F-A-03
func TestImages_RemovingAMissingPictureIsAnError(t *testing.T) {
	// Arrange
	mux, _, ev := imageEnv(t)

	// Act
	res := serve(mux, imagePost(t, map[string]string{"account": "1", "id": "999", "action": "remove", "confirm": "yes"}, nil))

	// Assert
	if res.Code != http.StatusUnprocessableEntity || len(ev.events) != 0 {
		t.Errorf("code %d events %d, want 422 and no history line", res.Code, len(ev.events))
	}
}

func TestImagesMemory_DeleteMissingIsNotFound(t *testing.T) {
	// Arrange
	m := &images.Memory{}

	// Act
	err := m.Delete(context.Background(), 1, 42)

	// Assert
	if err != images.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// F-C-02: a save from a page opened before another save is refused.
func TestBusiness_StaleTabCannotOverwrite(t *testing.T) {
	// Arrange
	mux, mp := businessEnv(t, true)
	serve(mux, bpost(url.Values{"account": {"1"}, "business_name": {"Shop"}, "about": {"old about"}}))
	opened := strconv.FormatInt(mp.saved[1].UpdatedAt.UnixMicro(), 10) // both tabs open now
	tabA := serve(mux, bpost(url.Values{"account": {"1"}, "version": {opened}, "business_name": {"Shop"}, "about": {"new about"}}))

	// Act
	tabB := serve(mux, bpost(url.Values{"account": {"1"}, "version": {opened}, "business_name": {"Shop"}, "about": {"old about"}, "rules": {"tab B rules"}}))

	// Assert
	if tabA.Code != http.StatusSeeOther {
		t.Fatalf("tab A = %d", tabA.Code)
	}
	if tabB.Code != http.StatusConflict || mp.saved[1].About != "new about" {
		t.Fatalf("tab B = %d, about now %q; want 409 and tab A's text kept", tabB.Code, mp.saved[1].About)
	}
	if body := tabB.Body.String(); !strings.Contains(body, "tab B rules") || !strings.Contains(body, "changed") {
		t.Error("the refused page must keep the typed text and say why")
	}
}

func TestBusiness_HistoryKeepsTheOldProfile(t *testing.T) {
	// Arrange
	_, mp := businessEnv(t, true)
	mp.saved[1] = profile.Profile{AccountID: 1, BusinessName: "Shop", About: "old about", BankTransfer: "SCB 9952310945"}
	ev := &memEvents{}
	m := newHandlerMux(t, &Handler{Profiles: mp, Events: ev})

	// Act
	serve(m, bpost(url.Values{"account": {"1"}, "business_name": {"Shop"}, "about": {"new about"}}))

	// Assert
	for _, e := range ev.events {
		if e.Action == "business profile saved" {
			old, ok := e.Before.(profile.Profile)
			if !ok || old.About != "old about" || old.BankTransfer != "…0945" {
				t.Errorf("before = %#v, want the old profile", e.Before)
			}
			return
		}
	}
	t.Error("no history line")
}

// F-C-01
func TestKnowledge_StaleEditIsRefused(t *testing.T) {
	// Arrange
	mux, ms := knowledgeEnv(t, true)
	serve(mux, kform(url.Values{"action": {"add_faq"}, "account": {"1"}, "question": {"ราคา?"}, "answer": {"3,900"}}))
	opened := strconv.FormatInt(ms.docs[0].UpdatedAt.UnixMicro(), 10)
	edit := func(answer string) url.Values {
		return url.Values{"action": {"update"}, "account": {"1"}, "id": {"1"}, "version": {opened}, "question": {"ราคา?"}, "answer": {answer}}
	}
	first := serve(mux, kform(edit("4,900")))

	// Act
	stale := serve(mux, kform(edit("2,900")))

	// Assert
	if first.Code != http.StatusSeeOther {
		t.Fatalf("first edit = %d %s", first.Code, first.Body.String())
	}
	if stale.Code != http.StatusUnprocessableEntity || ms.docs[0].Content != "4,900" {
		t.Errorf("stale edit = %d, answer now %q; want refused and 4,900 kept", stale.Code, ms.docs[0].Content)
	}
}

func TestKnowledge_EditPageCarriesVersion(t *testing.T) {
	// Arrange
	mux, ms := knowledgeEnv(t, true)
	serve(mux, kform(url.Values{"action": {"add_faq"}, "account": {"1"}, "question": {"q"}, "answer": {"a"}}))

	// Act
	page := serve(mux, getPath("/admin/knowledge?account=1&edit=1")).Body.String()

	// Assert
	want := `name="version" value="` + strconv.FormatInt(ms.docs[0].UpdatedAt.UnixMicro(), 10) + `"`
	if !strings.Contains(page, want) {
		t.Errorf("edit page lacks %s", want)
	}
}

// F-A-01 / F-D-05: writes with a bad account never fall back to account 1.
func TestWrites_BadAccountChangesNothing(t *testing.T) {
	for _, acct := range []string{"oops", "0", "-3", ""} {
		t.Run("knowledge "+acct, func(t *testing.T) {
			// Arrange
			mux, ms := knowledgeEnv(t, true)

			// Act
			res := serve(mux, kform(url.Values{"action": {"add_faq"}, "account": {acct}, "question": {"q"}, "answer": {"a"}}))

			// Assert
			if res.Code != http.StatusBadRequest || len(ms.docs) != 0 {
				t.Errorf("code %d docs %d, want 400 and nothing saved", res.Code, len(ms.docs))
			}
		})
		t.Run("connections "+acct, func(t *testing.T) {
			// Arrange
			e := setup(t, nil)
			mt := &memTokens{}
			e.h.Tokens = mt
			e.mux = http.NewServeMux()
			e.h.Routes(e.mux)

			// Act
			res := serve(e.mux, connPost(url.Values{"action": {"create"}, "account": {acct}, "name": {"x"}, "once": {e.h.newOnce()}}))

			// Assert
			if res.Code != http.StatusBadRequest || len(mt.tokens) != 0 {
				t.Errorf("code %d tokens %d, want 400 and no token", res.Code, len(mt.tokens))
			}
		})
	}
}

// newHandlerMux fills the test defaults into h and returns its routes.
func newHandlerMux(t *testing.T, h *Handler) *http.ServeMux {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	h.Password, h.Store, h.Switch, h.Timeout = pw, store, &llm.Switch{}, time.Second
	h.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	m := http.NewServeMux()
	h.Routes(m)
	return m
}
