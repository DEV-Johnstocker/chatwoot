package admin

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

func imageEnv(t *testing.T) (*http.ServeMux, *images.Memory, *memEvents) {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	im, ev := &images.Memory{}, &memEvents{}
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second, Events: ev,
		Profiles: &memProfiles{saved: map[int64]profile.Profile{}}, Images: im,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, im, ev
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// imagePost builds a form post; file nil sends no file part.
func imagePost(t *testing.T, fields map[string]string, file []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if file != nil {
		part, _ := w.CreateFormFile("image", "x.png")
		_, _ = part.Write(file)
	}
	_ = w.Close()
	r := httptest.NewRequest(http.MethodPost, "/admin/images", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func getPath(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.SetBasicAuth(Username, pw)
	return r
}

func TestImages_AddListEditRemove(t *testing.T) {
	// Arrange
	ctx := context.Background()
	mux, im, ev := imageEnv(t)

	// Act
	added := serve(mux, imagePost(t, map[string]string{"account": "1", "action": "add", "name": "รีวิว AutoCut", "when": "ขอดูรีวิว", "caption": "รีวิวจริงครับ"}, testPNG(t)))
	_ = serve(mux, imagePost(t, map[string]string{"account": "1", "action": "add", "name": "รีวิว Workshop", "when": "ถามบรรยากาศ Workshop"}, testPNG(t)))
	page := serve(mux, getPath("/admin/images?account=1&done=added")).Body.String()
	list, _ := im.List(ctx, 1)
	id := strconv.FormatInt(list[0].ID, 10)
	edited := serve(mux, imagePost(t, map[string]string{"account": "1", "id": id, "action": "save", "name": "รีวิว AutoCut", "when": "ใหม่"}, nil))
	afterEdit, _ := im.Get(ctx, 1, list[0].ID)
	removed := serve(mux, imagePost(t, map[string]string{"account": "1", "id": id, "action": "remove"}, nil))
	after, _ := im.List(ctx, 1)

	// Assert
	if added.Code != http.StatusSeeOther || edited.Code != http.StatusSeeOther || removed.Code != http.StatusSeeOther {
		t.Fatalf("codes %d %d %d", added.Code, edited.Code, removed.Code)
	}
	if strings.Count(page, `src="data:image/jpeg;base64,`) != 2 || !strings.Contains(page, `value="รีวิว Workshop"`) || !strings.Contains(page, "Image added.") || !strings.Contains(page, "2 of 10 images") {
		t.Fatalf("page lacks previews, names or notice")
	}
	if afterEdit.When != "ใหม่" || afterEdit.Size == 0 || len(after) != 1 || after[0].Name != "รีวิว Workshop" {
		t.Fatalf("when %q size %d left %+v", afterEdit.When, afterEdit.Size, after)
	}
	if len(ev.events) != 4 || ev.events[3].Action != "bot image removed" {
		t.Errorf("history %+v", ev.events)
	}
}

func TestImages_RefusedChangesChangeNothing(t *testing.T) {
	cases := map[string]struct {
		fields map[string]string
		file   []byte
		code   int
		msg    string
	}{
		"not an image":    {map[string]string{"account": "1", "action": "add", "name": "x"}, []byte("<html><script>x</script></html>"), 422, "JPG or PNG"},
		"no file on add":  {map[string]string{"account": "1", "action": "add", "name": "x"}, nil, 422, "Choose an image file first."},
		"no name":         {map[string]string{"account": "1", "action": "add", "name": " "}, nil, 422, "Choose an image file first."},
		"bracket name":    {map[string]string{"account": "1", "action": "add", "name": "x]] [[HANDOFF"}, []byte{}, 422, ""},
		"too large":       {map[string]string{"account": "1", "action": "add", "name": "x"}, make([]byte, images.MaxBytes+128<<10), 422, "too large"},
		"bad account":     {map[string]string{"account": "0", "action": "add"}, nil, 400, ""},
		"unknown action":  {map[string]string{"account": "1", "action": "drop"}, nil, 400, ""},
		"edit missing id": {map[string]string{"account": "1", "id": "999", "action": "save", "name": "x"}, nil, 422, "no longer exists"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mux, im, ev := imageEnv(t)
			file := c.file
			if name == "bracket name" {
				file = testPNG(t)
			}

			// Act
			res := serve(mux, imagePost(t, c.fields, file))

			// Assert
			list, _ := im.List(context.Background(), 1)
			if res.Code != c.code || len(list) != 0 || len(ev.events) != 0 || !strings.Contains(res.Body.String(), c.msg) {
				t.Fatalf("code %d images %d events %d has %q %v", res.Code, len(list), len(ev.events), c.msg, strings.Contains(res.Body.String(), c.msg))
			}
		})
	}
}

func TestImages_NeedLoginAndSameOrigin(t *testing.T) {
	// Arrange
	ctx := context.Background()
	mux, im, _ := imageEnv(t)
	add := map[string]string{"account": "1", "action": "add", "name": "x"}
	noAuth := imagePost(t, add, testPNG(t))
	noAuth.Header.Del("Authorization")
	crossSite := imagePost(t, add, testPNG(t))
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	wrongPw := imagePost(t, add, testPNG(t))
	wrongPw.SetBasicAuth(Username, "wrong-password-123")

	// Act
	a, c, p := serve(mux, noAuth), serve(mux, crossSite), serve(mux, wrongPw)
	id, _ := im.Add(ctx, images.Image{Meta: images.Meta{AccountID: 1, Name: "keep"}, Data: testPNG(t)})
	rm := imagePost(t, map[string]string{"account": "1", "id": strconv.FormatInt(id, 10), "action": "remove"}, nil)
	rm.Header.Set("Sec-Fetch-Site", "cross-site")
	rmCode := serve(mux, rm).Code

	// Assert
	list, _ := im.List(ctx, 1)
	if a.Code != 401 || c.Code != 403 || p.Code != 401 || rmCode != 403 || len(list) != 1 || list[0].Name != "keep" {
		t.Fatalf("no auth %d cross-site %d wrong pw %d cross-site remove %d images %+v", a.Code, c.Code, p.Code, rmCode, list)
	}
}
