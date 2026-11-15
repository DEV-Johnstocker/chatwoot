package chatwoot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolve_TogglesStatusResolved(t *testing.T) {
	// Arrange
	var path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()

	// Act
	err := New(srv.URL, "bot", nil).Resolve(context.Background(), 1, 42)

	// Assert
	if err != nil || path != "/api/v1/accounts/1/conversations/42/toggle_status" || body["status"] != "resolved" {
		t.Fatalf("err=%v path=%s body=%v", err, path, body)
	}
}

func TestAccounts_ListsActiveAccounts(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":9,"accounts":[{"id":1,"status":"active"},{"id":2,"status":"suspended"},{"id":3,"status":"active"}]}`))
	}))
	defer srv.Close()

	// Act
	ids, err := NewReader(srv.URL, "reader", nil).Accounts(context.Background())

	// Assert
	if err != nil || len(ids) != 2 || ids[0] != 1 || ids[1] != 3 {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
}

func TestIdlePending_PagesOldestFirstAndStopsAtNewer(t *testing.T) {
	// Arrange: page 1 is full of old chats, page 2 crosses the cut-off.
	before := time.Unix(10_000, 0)
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		q := r.URL.Query()
		if q.Get("status") != "pending" || q.Get("sort_by") != "last_activity_at_asc" || q.Get("assignee_type") != "all" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		switch q.Get("page") {
		case "1":
			fmt.Fprint(w, `{"data":{"payload":[
				{"id":1,"status":"pending","last_activity_at":100,"waiting_since":0,"last_non_activity_message":{"message_type":1,"private":false}},
				{"id":2,"status":"pending","last_activity_at":200,"waiting_since":150,"last_non_activity_message":{"message_type":0,"private":false}}]}}`)
		case "2":
			fmt.Fprint(w, `{"data":{"payload":[
				{"id":3,"status":"pending","last_activity_at":9000,"muted":true},
				{"id":4,"status":"pending","last_activity_at":20000}]}}`)
		default:
			t.Errorf("asked for page %s after passing the cut-off", q.Get("page"))
		}
	}))
	defer srv.Close()

	// Act
	convs, err := NewReader(srv.URL, "reader", nil).IdlePending(context.Background(), 1, before)

	// Assert
	if err != nil || len(convs) != 3 {
		t.Fatalf("convs=%+v err=%v queries=%v", convs, err, queries)
	}
	if c := convs[0]; c.ID != 1 || !c.WaitingSince.IsZero() || c.LastMessage == nil || !c.LastMessage.Outgoing {
		t.Errorf("first = %+v", c)
	}
	if c := convs[1]; c.WaitingSince.IsZero() || c.LastMessage.Outgoing {
		t.Errorf("second = %+v", c)
	}
	if c := convs[2]; !c.Muted || c.LastMessage != nil {
		t.Errorf("third = %+v", c)
	}
}

func TestIdlePending_ErrorStatus(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	// Act
	_, err := NewReader(srv.URL, "reader", nil).IdlePending(context.Background(), 1, time.Now())

	// Assert
	if err == nil {
		t.Fatal("want an error on 401")
	}
}
