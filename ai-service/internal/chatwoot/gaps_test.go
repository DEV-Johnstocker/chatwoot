package chatwoot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
)

func TestGapChats_FiltersByLabelAndStopsAtLimit(t *testing.T) {
	// Arrange
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		queries = append(queries, r.URL.RawQuery)
		if q.Get("labels[]") != "kb-gap" || q.Get("status") != "all" || q.Get("assignee_type") != "all" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		var items []string
		for i := range 25 {
			items = append(items, fmt.Sprintf(`{"id":%d,"inbox_id":2,"last_activity_at":100,"labels":["kb-gap","bot-handoff"]}`, i+1))
		}
		fmt.Fprintf(w, `{"data":{"payload":[%s]}}`, strings.Join(items, ","))
	}))
	defer srv.Close()

	// Act
	chats, err := NewReader(srv.URL, "reader", nil).GapChats(context.Background(), 1, 30)

	// Assert
	if err != nil || len(chats) != 30 || len(queries) != 2 {
		t.Fatalf("chats=%d err=%v queries=%v", len(chats), err, queries)
	}
	if chats[0].ID != 1 || chats[0].InboxID != 2 || chats[0].LastActivity.Unix() != 100 {
		t.Errorf("first = %+v", chats[0])
	}
}

func TestGapChats_SkipsChatsWithoutTheLabel(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"payload":[{"id":1,"labels":["kb-gap-done"]},{"id":2,"labels":["kb-gap"]}]}}`)
	}))
	defer srv.Close()

	// Act
	chats, err := NewReader(srv.URL, "reader", nil).GapChats(context.Background(), 1, 0)

	// Assert
	if err != nil || len(chats) != 1 || chats[0].ID != 2 {
		t.Fatalf("chats=%+v err=%v", chats, err)
	}
}

func TestChatMessages_ReadsOlderPagesUntilTheNote(t *testing.T) {
	// Arrange: the newest page has 20 staff messages; the gap note is on the page before.
	var befores []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		before := r.URL.Query().Get("before")
		befores = append(befores, before)
		var items []string
		switch before {
		case "":
			for i := range 20 {
				items = append(items, fmt.Sprintf(`{"id":%d,"message_type":1,"content":"staff","sender":{"type":"user"}}`, 100+i))
			}
		case "100":
			items = []string{
				`{"id":50,"message_type":0,"content":"มีใบประกาศไหม","created_at":10}`,
				`{"id":51,"message_type":1,"private":true,"content":"📚 **` + gaps.NoteTitle + `**","sender":{"type":"agent_bot"},"created_at":11}`,
			}
			for i := len(items); i < 20; i++ {
				items = append(items, fmt.Sprintf(`{"id":%d,"message_type":2,"content":"activity"}`, 60+i))
			}
		default:
			t.Errorf("read too far back: before=%s", before)
		}
		fmt.Fprintf(w, `{"payload":[%s]}`, strings.Join(items, ","))
	}))
	defer srv.Close()

	// Act
	msgs, err := NewReader(srv.URL, "reader", nil).ChatMessages(context.Background(), 1, 9)

	// Assert
	if err != nil || len(msgs) != 40 || len(befores) != 2 {
		t.Fatalf("msgs=%d err=%v befores=%v", len(msgs), err, befores)
	}
	if !msgs[0].Incoming || msgs[0].Content != "มีใบประกาศไหม" || !msgs[1].FromBot || !msgs[1].Private {
		t.Errorf("oldest first broken: %+v %+v", msgs[0], msgs[1])
	}
	g, ok := gaps.Read(msgs)
	if !ok || g.Asked != "มีใบประกาศไหม" || g.StaffReply != "staff" {
		t.Errorf("gap = %+v", g)
	}
}

func TestReplaceLabel_SwapsAndKeepsOthers(t *testing.T) {
	// Arrange
	var posted map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/1/conversations/7/labels" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&posted)
			fmt.Fprint(w, `{"payload":[]}`)
			return
		}
		fmt.Fprint(w, `{"payload":["bot-handoff","kb-gap","vip"]}`)
	}))
	defer srv.Close()

	// Act
	err := NewReader(srv.URL, "reader", nil).ReplaceLabel(context.Background(), 1, 7, "kb-gap", "kb-gap-done")

	// Assert
	if err != nil || strings.Join(posted["labels"], ",") != "bot-handoff,vip,kb-gap-done" {
		t.Fatalf("err=%v posted=%v", err, posted)
	}
}

func TestReplaceLabel_WithoutTheLabelChangesNothing(t *testing.T) {
	// Arrange
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		fmt.Fprint(w, `{"payload":["kb-gap-done"]}`)
	}))
	defer srv.Close()

	// Act
	err := NewReader(srv.URL, "reader", nil).ReplaceLabel(context.Background(), 1, 7, "kb-gap", "kb-gap-done")

	// Assert
	if !errors.Is(err, gaps.ErrNotAGap) || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestReaderErrors_NameThePathNotTheURL(t *testing.T) {
	// Arrange: nothing listens on the port, so the request fails to connect.
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	// Act
	_, err := NewReader(base, "reader", nil).GapChats(context.Background(), 1, 0)

	// Assert
	if err == nil || strings.Contains(err.Error(), base) || strings.Contains(err.Error(), "labels") {
		t.Fatalf("err = %v", err)
	}
}
