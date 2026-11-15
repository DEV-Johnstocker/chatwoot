package bot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/corrections"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

type fakeCorrector struct {
	got []corrections.Note
	id  int64
	err error
}

func (f *fakeCorrector) Propose(_ context.Context, n corrections.Note) (int64, error) {
	f.got = append(f.got, n)
	return f.id, f.err
}

func fixNote(content string) Job {
	return Job{Decision: webhook.Correct, Event: webhook.Event{
		Event: "message_created", ID: 900, Content: content, MessageType: "outgoing", Private: true,
		Sender:  webhook.Sender{ID: 5, Type: "user", Name: "Nok"},
		Account: webhook.Account{ID: 1}, Conversation: webhook.Conversation{ID: 7, Status: "open"},
	}}
}

func TestCorrect_ProposesAndConfirmsInTheChat(t *testing.T) {
	// Arrange
	cw := &fakeChatwoot{}
	b, h := newBot(cw, &fakeLLM{})
	fix := &fakeCorrector{id: 42}
	b.opts.Corrections = fix
	h.Append(1, 7, memory.Turn{Role: "user", Content: "ราคา?"})

	// Act
	b.Process(context.Background(), fixNote("#fix 3,900 บาท"))

	// Assert
	if len(fix.got) != 1 {
		t.Fatalf("proposals = %d", len(fix.got))
	}
	n := fix.got[0]
	if n.AccountID != 1 || n.ConversationID != 7 || n.NoteID != 900 || n.Staff != "Nok" || n.Answer != "3,900 บาท" || len(n.Memory) != 1 {
		t.Fatalf("note = %+v", n)
	}
	if got := cw.joined(); !strings.HasPrefix(got, "note:✅") || !strings.Contains(got, "#42") || strings.Contains(got, "msg:") {
		t.Fatalf("calls = %s", got)
	}
}

func TestCorrect_TellsStaffWhatWentWrong(t *testing.T) {
	cases := map[string]struct {
		corrector Corrector
		want      string
	}{
		"off":         {nil, fixOffNote},
		"no answer":   {&fakeCorrector{err: corrections.ErrNoAnswer}, fixNoAnswerNote},
		"no question": {&fakeCorrector{err: corrections.ErrNoQuestion}, fixNoQNote},
		"full":        {&fakeCorrector{err: pending.ErrFull}, fixFullNote},
		"db down":     {&fakeCorrector{err: errors.New("db down")}, fixFailNote},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			cw := &fakeChatwoot{}
			b, _ := newBot(cw, &fakeLLM{})
			b.opts.Corrections = c.corrector

			// Act
			b.Process(context.Background(), fixNote("#fix"))

			// Assert
			if got := cw.joined(); got != "note:"+c.want {
				t.Fatalf("calls = %s", got)
			}
		})
	}
}

func TestCorrect_CancelsAWaitingReply(t *testing.T) {
	// Arrange
	b, cw, l := batchingBot(t, 100*time.Millisecond)
	b.opts.Corrections = &fakeCorrector{id: 1}
	note := fixNote("#fix 3,900")
	note.Event.Conversation.Status = "pending"

	// Act
	_ = b.Enqueue(msgN(1, "ราคาเท่าไหร่"))
	time.Sleep(20 * time.Millisecond)
	_ = b.Enqueue(note)
	time.Sleep(250 * time.Millisecond)

	// Assert
	if calls, _ := l.snapshot(); calls != 0 || strings.Contains(cw.joined(), "msg:") {
		t.Fatalf("bot answered after a staff note: calls=%d %s", calls, cw.joined())
	}
}
