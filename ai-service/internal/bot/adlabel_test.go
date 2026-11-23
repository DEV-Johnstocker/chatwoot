package bot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/referral"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// markChatwoot records labels and attributes as calls.
type markChatwoot struct {
	fakeChatwoot
}

func (m *markChatwoot) AddLabel(_ context.Context, _, _ int64, label string) error {
	return m.record("label:" + label)
}

func (m *markChatwoot) SetConversationAttributes(_ context.Context, _, _ int64, attrs map[string]string) error {
	return m.record("attrs:" + attrs[AdNameAttribute] + "/" + attrs[AdIDAttribute] + "/" + attrs[AdTappedAttribute])
}

// adLinks is a chat linker that also finds the chat's latest ad.
type adLinks struct {
	fakeChatLinks
	mu    sync.Mutex
	ad    referral.Referral
	found bool
	err   error
	since []time.Time
}

func (a *adLinks) LatestAd(_ context.Context, _, _ int64, since time.Time) (referral.Referral, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.since = append(a.since, since)
	return a.ad, a.found, a.err
}

var tapTime = time.Date(2026, 11, 23, 6, 12, 0, 0, time.UTC)

func adBot(t *testing.T, links *adLinks) (*Bot, *markChatwoot) {
	t.Helper()
	cw := &markChatwoot{}
	b, _ := newBot(&cw.fakeChatwoot, &fakeLLM{reply: "ok"})
	b.opts.Chatwoot = cw
	b.opts.ChatLinks = links
	return b, cw
}

func messengerMsg(text string) Job {
	j := leadMsg(text)
	j.Event.Conversation.Channel = "Channel::FacebookPage"
	j.Event.Conversation.ContactInbox.SourceID = "PSID-77"
	return j
}

func adCalls(cw *markChatwoot) []string {
	var out []string
	for _, c := range strings.Split(cw.joined(), " | ") {
		if strings.HasPrefix(c, "label:") || strings.HasPrefix(c, "attrs:") {
			out = append(out, c)
		}
	}
	return out
}

func TestProcess_ChatFromAdGetsLabelAndAdNameOnce(t *testing.T) {
	// Arrange
	links := &adLinks{found: true, ad: referral.Referral{Source: "ADS", AdID: "1202", AdTitle: "Auto Cut ลดเหลือ 3,900", ReferredAt: tapTime}}
	b, cw := adBot(t, links)

	// Act
	b.Process(context.Background(), messengerMsg("สนใจครับ"))
	b.Process(context.Background(), messengerMsg("ราคาเท่าไหร่"))

	// Assert
	want := []string{"label:ad", "attrs:Auto Cut ลดเหลือ 3,900/1202/2026-11-23T06:12:00Z"}
	if got := adCalls(cw); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v (marked once)", got, want)
	}
	if len(links.since) != 2 || time.Since(links.since[0]) < AdLabelWindow-time.Minute || time.Since(links.since[0]) > AdLabelWindow+time.Minute {
		t.Fatalf("since = %v, want now - %v", links.since, AdLabelWindow)
	}
}

func TestProcess_NewAdTapMarksTheChatAgain(t *testing.T) {
	// Arrange
	links := &adLinks{found: true, ad: referral.Referral{Source: "ADS", AdID: "1202", AdTitle: "First", ReferredAt: tapTime}}
	b, cw := adBot(t, links)
	b.Process(context.Background(), messengerMsg("สวัสดี"))
	links.ad = referral.Referral{Source: "ADS", AdID: "3303", AdTitle: "Workshop", ReferredAt: tapTime.Add(time.Hour)}

	// Act
	b.Process(context.Background(), messengerMsg("สนใจ workshop"))

	// Assert
	got := adCalls(cw)
	if len(got) != 4 || !strings.HasPrefix(got[3], "attrs:Workshop/3303/") {
		t.Fatalf("calls = %v", got)
	}
}

func TestProcess_ChatWithoutAdIsNotMarked(t *testing.T) {
	// Arrange
	b, cw := adBot(t, &adLinks{})

	// Act
	b.Process(context.Background(), messengerMsg("สวัสดี"))

	// Assert
	if got := adCalls(cw); len(got) != 0 {
		t.Fatalf("calls = %v, want none", got)
	}
}

func TestProcess_ChatStaffHandleIsStillLinkedAndMarked(t *testing.T) {
	// Arrange
	links := &adLinks{found: true, ad: referral.Referral{Source: "ADS", AdID: "1202", ReferredAt: tapTime}}
	b, cw := adBot(t, links)
	j := messengerMsg("ขอเลขบัญชีครับ")
	j.Decision = webhook.Record
	j.Event.Conversation.Status = "open"

	// Act
	b.Process(context.Background(), j)

	// Assert
	if len(links.links) != 1 {
		t.Fatalf("links = %v, want the chat linked", links.links)
	}
	if got := adCalls(cw); len(got) != 2 || got[1] != "attrs:Ad 1202/1202/2026-11-23T06:12:00Z" {
		t.Fatalf("calls = %v", got)
	}
}

func TestProcess_StaffMessageIsNotLinked(t *testing.T) {
	// Arrange
	links := &adLinks{found: true, ad: referral.Referral{Source: "ADS", AdID: "1202", ReferredAt: tapTime}}
	b, cw := adBot(t, links)
	j := messengerMsg("โอนแล้วแจ้งได้เลยครับ")
	j.Decision = webhook.Record
	j.Event.MessageType = "outgoing"
	j.Event.Sender.Type = ""

	// Act
	b.Process(context.Background(), j)

	// Assert
	if len(links.links) != 0 || len(adCalls(cw)) != 0 {
		t.Fatalf("links %v calls %v, want none", links.links, adCalls(cw))
	}
}

func TestProcess_AdLookupErrorStillRepliesAndLogsNoAd(t *testing.T) {
	// Arrange
	links := &adLinks{err: errors.New("db down")}
	b, cw := adBot(t, links)
	var logs bytes.Buffer
	b.opts.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	// Act
	b.Process(context.Background(), messengerMsg("สวัสดี"))

	// Assert
	if !strings.Contains(cw.joined(), "msg:ok") || !strings.Contains(logs.String(), "ad lookup failed") || strings.Contains(logs.String(), "PSID-77") {
		t.Fatalf("calls %s logs %s", cw.joined(), logs.String())
	}
	if len(adCalls(cw)) != 0 {
		t.Fatalf("marked despite error: %v", adCalls(cw))
	}
}

func TestProcess_FailedLabelIsRetriedOnNextMessage(t *testing.T) {
	// Arrange
	links := &adLinks{found: true, ad: referral.Referral{Source: "ADS", AdID: "1202", ReferredAt: tapTime}}
	cw := &failingMark{}
	b, _ := newBot(&cw.fakeChatwoot, &fakeLLM{reply: "ok"})
	b.opts.Chatwoot, b.opts.ChatLinks = cw, links
	cw.fail = true
	b.Process(context.Background(), messengerMsg("สวัสดี"))
	cw.fail = false

	// Act
	b.Process(context.Background(), messengerMsg("สนใจครับ"))

	// Assert
	if cw.labels != 2 {
		t.Fatalf("label calls = %d, want a retry after the failure", cw.labels)
	}
}

type failingMark struct {
	fakeChatwoot
	fail   bool
	labels int
}

func (f *failingMark) AddLabel(context.Context, int64, int64, string) error {
	f.labels++
	if f.fail {
		return errors.New("chatwoot down")
	}
	return nil
}

func (f *failingMark) SetConversationAttributes(context.Context, int64, int64, map[string]string) error {
	return nil
}
