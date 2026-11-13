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
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/reviews"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

func reviewEnv(t *testing.T) (*http.ServeMux, *reviews.Memory, *memEvents) {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	rv, ev := &reviews.Memory{}, &memEvents{}
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second, Events: ev,
		Profiles: &memProfiles{saved: map[int64]profile.Profile{}}, Reviews: rv,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, rv, ev
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// reviewPost builds an upload; file nil sends no file part.
func reviewPost(t *testing.T, fields map[string]string, file []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if file != nil {
		part, _ := w.CreateFormFile("image", "reviews.png")
		_, _ = part.Write(file)
	}
	_ = w.Close()
	r := httptest.NewRequest(http.MethodPost, "/admin/business/review-image", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func TestReviewImage_SaveShowCaptionOnlyAndRemove(t *testing.T) {
	// Arrange
	ctx := context.Background()
	mux, rv, ev := reviewEnv(t)

	// Act
	saved := serve(mux, reviewPost(t, map[string]string{"account": "1", "caption": "รีวิวจริงครับ", "action": "save"}, testPNG(t)))
	page := serve(mux, commentsGetPath("/admin/business?account=1&review=saved")).Body.String()
	captionOnly := serve(mux, reviewPost(t, map[string]string{"account": "1", "caption": "รีวิวใหม่", "action": "save"}, nil))
	afterCaption, _, _ := rv.Get(ctx, 1)
	removed := serve(mux, reviewPost(t, map[string]string{"account": "1", "action": "remove"}, nil))
	_, still, _ := rv.Get(ctx, 1)

	// Assert
	if saved.Code != http.StatusSeeOther || captionOnly.Code != http.StatusSeeOther || removed.Code != http.StatusSeeOther {
		t.Fatalf("codes %d %d %d", saved.Code, captionOnly.Code, removed.Code)
	}
	if !strings.Contains(page, `src="data:image/jpeg;base64,`) || !strings.Contains(page, `value="รีวิวจริงครับ"`) || !strings.Contains(page, "Image saved.") {
		t.Fatalf("page lacks the preview, caption or notice")
	}
	if afterCaption.Caption != "รีวิวใหม่" || afterCaption.ContentType != "image/png" || still {
		t.Fatalf("caption %q type %q still %v", afterCaption.Caption, afterCaption.ContentType, still)
	}
	actions := []string{}
	for _, e := range ev.events {
		actions = append(actions, e.Action)
	}
	if strings.Join(actions, ",") != "review image saved,review image saved,review image removed" {
		t.Errorf("history %v", actions)
	}
}

func TestReviewImage_RefusedUploadsChangeNothing(t *testing.T) {
	cases := map[string]struct {
		fields map[string]string
		file   []byte
		code   int
		msg    string
	}{
		"not an image":      {map[string]string{"account": "1", "action": "save"}, []byte("<html><script>x</script></html>"), http.StatusUnprocessableEntity, "JPG or PNG"},
		"no file, none set": {map[string]string{"account": "1", "action": "save"}, nil, http.StatusUnprocessableEntity, "Choose an image file first."},
		"too large":         {map[string]string{"account": "1", "action": "save"}, make([]byte, reviews.MaxBytes+128<<10), http.StatusUnprocessableEntity, "too large"},
		"bad account":       {map[string]string{"account": "0", "action": "save"}, nil, http.StatusBadRequest, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			mux, rv, ev := reviewEnv(t)

			// Act
			res := serve(mux, reviewPost(t, c.fields, c.file))

			// Assert
			_, set, _ := rv.Get(context.Background(), 1)
			if res.Code != c.code || set || len(ev.events) != 0 || !strings.Contains(res.Body.String(), c.msg) {
				t.Fatalf("code %d set %v events %d body has %q: %v", res.Code, set, len(ev.events), c.msg, strings.Contains(res.Body.String(), c.msg))
			}
		})
	}
}

func TestReviewImage_NeedsLoginAndSameOrigin(t *testing.T) {
	// Arrange
	mux, rv, _ := reviewEnv(t)
	noAuth := reviewPost(t, map[string]string{"account": "1", "action": "save"}, testPNG(t))
	noAuth.Header.Del("Authorization")
	crossSite := reviewPost(t, map[string]string{"account": "1", "action": "save"}, testPNG(t))
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")

	// Act
	a := serve(mux, noAuth)
	c := serve(mux, crossSite)

	// Assert
	_, set, _ := rv.Get(context.Background(), 1)
	if a.Code != http.StatusUnauthorized || c.Code != http.StatusForbidden || set {
		t.Fatalf("no auth %d cross-site %d set %v", a.Code, c.Code, set)
	}
}

func commentsGetPath(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.SetBasicAuth(Username, pw)
	return r
}
