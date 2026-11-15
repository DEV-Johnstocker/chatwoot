package chatwoot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecentChats_AllStatusesNewestFirst(t *testing.T) {
	// Arrange
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		q := r.URL.Query()
		if q.Get("status") != "all" || q.Get("sort_by") != "last_activity_at_desc" || q.Get("page") != "2" || q.Get("labels[]") != "" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"data":{"payload":[{"id":9,"inbox_id":2,"last_activity_at":50}]}}`)
	}))
	defer srv.Close()

	// Act
	chats, err := NewReader(srv.URL, "reader", nil).RecentChats(context.Background(), 1, 2)

	// Assert
	if err != nil || len(chats) != 1 || chats[0].ID != 9 || chats[0].InboxID != 2 {
		t.Fatalf("chats=%+v err=%v query=%s", chats, err, query)
	}
}

func TestLatestMessages_OnePageMapped(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("before") != "" {
			t.Error("must read only the newest page")
		}
		fmt.Fprint(w, `{"payload":[{"id":1,"message_type":0,"content":"q","created_at":5},{"id":2,"message_type":1,"content":"a","sender":{"type":"agent_bot"}}]}`)
	}))
	defer srv.Close()

	// Act
	msgs, err := NewReader(srv.URL, "reader", nil).LatestMessages(context.Background(), 1, 7)

	// Assert
	if err != nil || len(msgs) != 2 || !msgs[0].Incoming || msgs[0].At.Unix() != 5 || !msgs[1].FromBot {
		t.Fatalf("msgs=%+v err=%v", msgs, err)
	}
}
