package chatwoot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPendingBetween_NewestFirstKeepsWindowAndStopsAtOlder(t *testing.T) {
	// Arrange: page 1 holds one chat newer than the window and one inside;
	// page 2 starts inside and crosses the older edge.
	from, to := time.Unix(1_000, 0), time.Unix(5_000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("status") != "pending" || q.Get("sort_by") != "last_activity_at_desc" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		switch q.Get("page") {
		case "1":
			fmt.Fprint(w, `{"data":{"payload":[
				{"id":4,"status":"pending","last_activity_at":5001},
				{"id":3,"status":"pending","last_activity_at":5000,"muted":true,
				 "meta":{"assignee":{"id":12},"assignee_type":"User"}}]}}`)
		case "2":
			fmt.Fprint(w, `{"data":{"payload":[
				{"id":2,"status":"pending","last_activity_at":1000,"labels":["nudged","vip"],
				 "meta":{"assignee":{"id":1},"assignee_type":"AgentBot"}},
				{"id":1,"status":"pending","last_activity_at":999}]}}`)
		default:
			t.Errorf("asked for page %s after passing the window", q.Get("page"))
		}
	}))
	defer srv.Close()

	// Act
	convs, err := NewReader(srv.URL, "reader", nil).PendingBetween(context.Background(), 3, from, to)

	// Assert
	if err != nil || len(convs) != 2 || convs[0].ID != 3 || convs[1].ID != 2 {
		t.Fatalf("convs=%+v err=%v, want chats 3 and 2", convs, err)
	}
	if !convs[0].Muted || !convs[0].Assigned || convs[1].Assigned || len(convs[1].Labels) != 2 || convs[1].Labels[0] != "nudged" {
		t.Fatalf("muted/assigned/labels not carried (the bot as assignee is not staff): %+v", convs)
	}
}

func TestAddLabelIfMissing(t *testing.T) {
	// Arrange
	var posts int
	labels := `{"payload":["vip"]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			labels = `{"payload":["vip","nudged"]}`
			return
		}
		fmt.Fprint(w, labels)
	}))
	defer srv.Close()
	c := New(srv.URL, "bot", nil)

	// Act
	first, err1 := c.AddLabelIfMissing(context.Background(), 3, 7, "nudged")
	second, err2 := c.AddLabelIfMissing(context.Background(), 3, 7, "nudged")

	// Assert
	if err1 != nil || err2 != nil || !first || second || posts != 1 {
		t.Fatalf("first=%v second=%v posts=%d errs=%v %v, want added once", first, second, posts, err1, err2)
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

func TestPendingBetween_CarriesInboxContactAndChannel(t *testing.T) {
	// Arrange: the follow-up picture needs where the chat came in and who it is.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			fmt.Fprint(w, `{"data":{"payload":[]}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"payload":[
			{"id":17,"status":"pending","last_activity_at":2000,"inbox_id":2,
			 "meta":{"sender":{"id":55},"channel":"Channel::WebWidget","hmac_verified":true}}]}}`)
	}))
	defer srv.Close()

	// Act
	convs, err := NewReader(srv.URL, "reader", nil).PendingBetween(context.Background(), 3, time.Unix(1_000, 0), time.Unix(5_000, 0))

	// Assert
	if err != nil || len(convs) != 1 {
		t.Fatalf("convs=%+v err=%v", convs, err)
	}
	if c := convs[0]; c.InboxID != 2 || c.ContactID != 55 || c.Channel != "Channel::WebWidget" || !c.HmacVerified {
		t.Fatalf("got %+v, want inbox 2, contact 55, widget, verified", c)
	}
}
