package chatwoot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendAttachment_PostsMultipartImage(t *testing.T) {
	// Arrange
	var path, token, content, private, filename, ctype string
	var data []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, token = r.URL.Path, r.Header.Get("api_access_token")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
			return
		}
		content, private = r.FormValue("content"), r.FormValue("private")
		files := r.MultipartForm.File["attachments[]"]
		if len(files) == 1 {
			filename, ctype = files[0].Filename, files[0].Header.Get("Content-Type")
			f, _ := files[0].Open()
			data, _ = io.ReadAll(f)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "bot-token", nil)

	// Act
	err := c.SendAttachment(context.Background(), 1, 7, "รีวิวจากผู้เรียนจริงครับ", "reviews.jpg", "image/jpeg", []byte("JPEGDATA"))

	// Assert
	if err != nil || path != "/api/v1/accounts/1/conversations/7/messages" || token != "bot-token" {
		t.Fatalf("err %v path %q token %q", err, path, token)
	}
	if content != "รีวิวจากผู้เรียนจริงครับ" || private != "false" || filename != "reviews.jpg" || ctype != "image/jpeg" || string(data) != "JPEGDATA" {
		t.Fatalf("content %q private %q file %q %q data %q", content, private, filename, ctype, data)
	}
}

func TestSendAttachment_ReturnsAPIError(t *testing.T) {
	// Arrange
	var got captured
	srv := newServer(t, http.StatusUnprocessableEntity, &got)
	c := New(srv.URL, "t", nil)

	// Act
	err := c.SendAttachment(context.Background(), 1, 7, "", "r.png", "image/png", []byte("x"))

	// Assert
	var apiErr *APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("err %v", err)
	}
}

func asAPIError(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}
