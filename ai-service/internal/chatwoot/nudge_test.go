package chatwoot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPendingBetween_KeepsWindowAndStopsAtNewer(t *testing.T) {
	// Arrange: page 1 holds one chat older than the window and one inside;
	// page 2 starts inside and crosses the newer edge.
	from, to := time.Unix(1_000, 0), time.Unix(5_000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("status") != "pending" || q.Get("sort_by") != "last_activity_at_asc" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		switch q.Get("page") {
		case "1":
			fmt.Fprint(w, `{"data":{"payload":[
				{"id":1,"status":"pending","last_activity_at":500},
				{"id":2,"status":"pending","last_activity_at":1000,"labels":["nudged","vip"]}]}}`)
		case "2":
			fmt.Fprint(w, `{"data":{"payload":[
				{"id":3,"status":"pending","last_activity_at":5000,"muted":true},
				{"id":4,"status":"pending","last_activity_at":5001}]}}`)
		default:
			t.Errorf("asked for page %s after passing the window", q.Get("page"))
		}
	}))
	defer srv.Close()

	// Act
	convs, err := NewReader(srv.URL, "reader", nil).PendingBetween(context.Background(), 3, from, to)

	// Assert
	if err != nil || len(convs) != 2 || convs[0].ID != 2 || convs[1].ID != 3 {
		t.Fatalf("convs=%+v err=%v, want chats 2 and 3", convs, err)
	}
	if len(convs[0].Labels) != 2 || convs[0].Labels[0] != "nudged" || !convs[1].Muted {
		t.Fatalf("labels/muted not carried: %+v", convs)
	}
}

func TestPendingBetween_ErrorIsReturned(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()

	// Act
	_, err := NewReader(srv.URL, "wrong", nil).PendingBetween(context.Background(), 3, time.Unix(0, 0), time.Now())

	// Assert
	if err == nil {
		t.Fatal("err = nil, want the 401 reported")
	}
}
