package comments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGraph_PostsAPublicReplyUnderTheComment(t *testing.T) {
	// Arrange
	var gotPath, gotAuth, gotQuery string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotQuery = r.URL.Path, r.Header.Get("Authorization"), r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"id":"2222_3333_4444"}`)
	}))
	defer srv.Close()
	g := &Graph{BaseURL: srv.URL + "/v24.0", PageID: testPage, PageToken: "page-token-xyz"}

	// Act
	err := g.PostPublicReply(context.Background(), "2222_3333", "สวัสดีครับ ส่งรายละเอียดทางข้อความแล้วนะครับ")

	// Assert
	if err != nil || gotPath != "/v24.0/2222_3333/comments" || gotAuth != "Bearer page-token-xyz" || gotQuery != "" {
		t.Fatalf("err %v path %q auth %q query %q", err, gotPath, gotAuth, gotQuery)
	}
	if gotBody["message"] != "สวัสดีครับ ส่งรายละเอียดทางข้อความแล้วนะครับ" {
		t.Fatalf("body %+v", gotBody)
	}
}

func TestGraph_PublicReplyRefusalHidesTheToken(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":200,"message":"(#200) Requires pages_manage_engagement permission"}}`)
	}))
	defer srv.Close()
	g := &Graph{BaseURL: srv.URL, PageID: testPage, PageToken: "page-token-xyz"}

	// Act
	err := g.PostPublicReply(context.Background(), "c", "hi")

	// Assert
	var se *SendError
	if !errors.As(err, &se) || se.Code != 200 || !strings.Contains(err.Error(), "pages_manage_engagement") || strings.Contains(err.Error(), "page-token-xyz") {
		t.Fatalf("err %v", err)
	}
}

type fakePublic struct {
	calls []string
	err   error
}

func (f *fakePublic) PostPublicReply(_ context.Context, commentID, text string) error {
	f.calls = append(f.calls, commentID+"|"+text)
	return f.err
}

func TestQueue_RepliesPubliclyOnlyAfterThePrivateReply(t *testing.T) {
	// Arrange
	ctx := context.Background()
	store, pub := &Memory{}, &fakePublic{}
	q := &Queue{Store: store, Sender: &fakeSender{}, Public: pub, PublicText: "ส่งรายละเอียดทางข้อความแล้วครับ", Now: func() time.Time { return now }}
	c := waitingComment(t, store, now)

	// Act
	before := q.ReplyPublicly(ctx, 2, c.ID)
	sendErr := q.Send(ctx, 2, c.ID, c.Version, "Hi", "owner")
	otherAccount := q.ReplyPublicly(ctx, 3, c.ID)
	after := q.ReplyPublicly(ctx, 2, c.ID)

	// Assert
	if !errors.Is(before, ErrNotSent) || sendErr != nil || !errors.Is(otherAccount, ErrNotFound) || after != nil {
		t.Fatalf("before %v send %v other %v after %v", before, sendErr, otherAccount, after)
	}
	if len(pub.calls) != 1 || pub.calls[0] != c.CommentID+"|ส่งรายละเอียดทางข้อความแล้วครับ" {
		t.Fatalf("calls %v", pub.calls)
	}
}

func TestQueue_PublicReplyIsOffWithoutTextOrSender(t *testing.T) {
	cases := map[string]*Queue{
		"no text":    {Public: &fakePublic{}},
		"no replier": {PublicText: "x"},
		"blank text": {Public: &fakePublic{}, PublicText: "   "},
	}
	for name, q := range cases {
		// Arrange
		q.Store, q.Sender = &Memory{}, &fakeSender{}

		// Act
		text, err := q.PublicReply(), q.ReplyPublicly(context.Background(), 2, 1)

		// Assert
		if text != "" || !errors.Is(err, ErrSendingOff) {
			t.Errorf("%s: text %q err %v", name, text, err)
		}
	}
}
