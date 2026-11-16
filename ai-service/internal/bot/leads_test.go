package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/leads"
)

type fakeLeads struct {
	mu        sync.Mutex
	customers []string
	replies   []string
	err       error
}

func (f *fakeLeads) Customer(_ context.Context, c leads.Contact, asked bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.customers = append(f.customers, fmt.Sprintf("%d/%d contact=%d name=%s asked=%v", c.AccountID, c.ConversationID, c.ContactID, c.Name, asked))
	return f.err
}

func (f *fakeLeads) Replied(_ context.Context, acct, conv int64, interest string, quoted bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, fmt.Sprintf("%d/%d interest=%s quoted=%v", acct, conv, interest, quoted))
	return f.err
}

func leadMsg(text string) Job {
	j := customerMsg(text)
	j.Event.Sender.ID, j.Event.Sender.Type, j.Event.Sender.Name = 55, "contact", "Somchai"
	return j
}

func TestProcess_PriceQuestionBecomesQuotedLead(t *testing.T) {
	// Arrange
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "คอร์ส AutoCut ราคา 3,900 บาทค่ะ"}
	r := &fakeRetriever{hits: []knowledge.Hit{{DocumentID: 3, Title: "AutoCut course", Score: 0.8}, {DocumentID: 4, Title: "Refunds", Score: 0.5}}}
	b, _ := ragBot(cw, l, r)
	rec := &fakeLeads{}
	b.opts.Leads = rec

	// Act
	b.Process(context.Background(), leadMsg("คอร์สนี้ราคาเท่าไหร่คะ"))

	// Assert
	if want := "1/7 contact=55 name=Somchai asked=true"; len(rec.customers) != 1 || rec.customers[0] != want {
		t.Fatalf("customers = %v, want [%s]", rec.customers, want)
	}
	if want := "1/7 interest=AutoCut course quoted=true"; len(rec.replies) != 1 || rec.replies[0] != want {
		t.Fatalf("replies = %v, want [%s]", rec.replies, want)
	}
}

func TestProcess_SmallTalkTouchesButDoesNotRecordInterest(t *testing.T) {
	// Arrange
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "ยินดีค่ะ"}
	b, _ := newBot(cw, l)
	rec := &fakeLeads{}
	b.opts.Leads = rec

	// Act
	b.Process(context.Background(), leadMsg("ขอบคุณค่ะ"))

	// Assert
	if len(rec.customers) != 1 || !strings.HasSuffix(rec.customers[0], "asked=false") {
		t.Fatalf("customers = %v", rec.customers)
	}
	if len(rec.replies) != 0 {
		t.Fatalf("replies = %v, want none", rec.replies)
	}
}

func TestProcess_LeadStoreErrorStillRepliesAndLogsNoName(t *testing.T) {
	// Arrange
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "ราคา 990 บาทค่ะ"}
	b, _ := newBot(cw, l)
	var logs bytes.Buffer
	b.opts.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	b.opts.Leads = &fakeLeads{err: errors.New("db down")}

	// Act
	b.Process(context.Background(), leadMsg("how much?"))

	// Assert
	if !strings.Contains(cw.joined(), "msg:ราคา 990 บาทค่ะ") {
		t.Fatalf("bot did not reply: %s", cw.joined())
	}
	if !strings.Contains(logs.String(), "lead not recorded") || strings.Contains(logs.String(), "Somchai") || strings.Contains(logs.String(), "how much") {
		t.Fatalf("logs: %s", logs.String())
	}
}

func TestProcess_StaffSenderNameIsNotTheLead(t *testing.T) {
	// Arrange
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "ok"}
	b, _ := newBot(cw, l)
	rec := &fakeLeads{}
	b.opts.Leads = rec
	j := leadMsg("ราคาเท่าไหร่")
	j.Event.Sender.Type = "user"

	// Act
	b.Process(context.Background(), j)

	// Assert
	if want := "1/7 contact=0 name= asked=true"; len(rec.customers) != 1 || rec.customers[0] != want {
		t.Fatalf("customers = %v, want [%s]", rec.customers, want)
	}
}
