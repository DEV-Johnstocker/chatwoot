package chatwoot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// previousChatServer answers the contact's conversation list and one
// conversation's messages, and counts the calls to each.
func previousChatServer(t *testing.T, conversations string, calls map[string]int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path+"?"+r.URL.RawQuery]++
		switch r.URL.Path {
		case "/api/v1/accounts/1/contacts/55/conversations":
			_, _ = w.Write([]byte(conversations))
		case "/api/v1/accounts/1/conversations/11/messages":
			_, _ = w.Write([]byte(`{"payload":[
				{"id":1,"message_type":0,"private":false,"content":"ราคาเท่าไหร่"},
				{"id":2,"message_type":1,"private":false,"content":"3,900 บาทครับ","sender":{"type":"agent_bot"}},
				{"id":3,"message_type":0,"private":false,"content":"ใช้ Windows 11 ครับ"},
				{"id":4,"message_type":1,"private":false,"content":"","attachments":[{"file_type":"image","file_size":209108}],"sender":{"type":"agent_bot"}},
				{"id":5,"message_type":1,"private":true,"content":"🤖 สรุปจากบอท","sender":{"type":"agent_bot"}}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestPreviousChat_LoadsTheEndOfTheOlderChatInTheSameInbox(t *testing.T) {
	// Arrange: Chatwoot returns the older and newer neighbours of chat 12.
	calls := map[string]int{}
	srv := previousChatServer(t, `{"payload":[
		{"id":11,"inbox_id":2,"last_activity_at":1795183800},
		{"id":12,"inbox_id":2,"last_activity_at":1795260000}
	]}`, calls)
	defer srv.Close()
	r := NewReader(srv.URL, "reader-token", nil)

	// Act
	e, err := r.PreviousChat(context.Background(), 1, 2, 55, 12, 2)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if calls["/api/v1/accounts/1/contacts/55/conversations?conversation_id=12"] != 1 {
		t.Fatalf("calls = %v, want the contact's conversations around chat 12", calls)
	}
	if !e.When.Equal(time.Unix(1795183800, 0)) {
		t.Errorf("when = %v, want the older chat's last activity", e.When)
	}
	if len(e.Turns) != 2 || e.Turns[0].Content != "ใช้ Windows 11 ครับ" || e.Turns[1].Content != "[[IMAGE: ?209108]]" {
		t.Errorf("turns = %+v, want only the last 2 public turns, the picture marked with its size", e.Turns)
	}
}

func TestPreviousChat_IgnoresAnotherInbox(t *testing.T) {
	// Arrange: the older chat came through a different inbox (another page).
	calls := map[string]int{}
	srv := previousChatServer(t, `{"payload":[
		{"id":11,"inbox_id":9,"last_activity_at":1795183800},
		{"id":12,"inbox_id":2,"last_activity_at":1795260000}
	]}`, calls)
	defer srv.Close()
	r := NewReader(srv.URL, "t", nil)

	// Act
	e, err := r.PreviousChat(context.Background(), 1, 2, 55, 12, 6)

	// Assert
	if err != nil || len(e.Turns) != 0 {
		t.Fatalf("err %v earlier %+v, want nothing from another inbox", err, e)
	}
	if calls["/api/v1/accounts/1/conversations/11/messages?"] != 0 {
		t.Error("messages of another inbox's chat must not be read")
	}
}

func TestPreviousChat_FirstChatHasNoEarlier(t *testing.T) {
	// Arrange: only the current chat (and nothing older) exists.
	calls := map[string]int{}
	srv := previousChatServer(t, `{"payload":[{"id":12,"inbox_id":2,"last_activity_at":1795260000}]}`, calls)
	defer srv.Close()
	r := NewReader(srv.URL, "t", nil)

	// Act
	e, err := r.PreviousChat(context.Background(), 1, 2, 55, 12, 6)

	// Assert
	if err != nil || len(e.Turns) != 0 || !e.When.IsZero() {
		t.Fatalf("err %v earlier %+v, want none", err, e)
	}
}

func TestPreviousChat_ErrorStatus(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	r := NewReader(srv.URL, "wrong", nil)

	// Act
	_, err := r.PreviousChat(context.Background(), 1, 2, 55, 12, 6)

	// Assert
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want an APIError with status 401", err)
	}
}

func TestPreviousChat_NeedsAContact(t *testing.T) {
	// Arrange: no contact id (an event without a sender) means no lookup.
	calls := map[string]int{}
	srv := previousChatServer(t, `{"payload":[]}`, calls)
	defer srv.Close()
	r := NewReader(srv.URL, "t", nil)

	// Act
	e, err := r.PreviousChat(context.Background(), 1, 2, 0, 12, 6)

	// Assert
	if err != nil || len(e.Turns) != 0 || len(calls) != 0 {
		t.Fatalf("err %v earlier %+v calls %v, want no request", err, e, calls)
	}
}
