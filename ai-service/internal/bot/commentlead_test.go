package bot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/commentecho"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// labelChatwoot also records labels.
type labelChatwoot struct {
	fakeChatwoot
	lmu    sync.Mutex
	labels []string
}

func (l *labelChatwoot) AddLabel(_ context.Context, _, _ int64, label string) error {
	l.lmu.Lock()
	defer l.lmu.Unlock()
	l.labels = append(l.labels, label)
	return nil
}

// pageEcho is a Messenger echo of a page message in a chat the bot owns.
func pageEcho(sourceID, text string) Job {
	j := Job{Decision: webhook.Record, Event: webhook.Event{
		Event: "message_created", ID: 30, Content: text, MessageType: "outgoing", SourceID: sourceID,
		Account: webhook.Account{ID: 1}, Conversation: webhook.Conversation{ID: 7, Status: "pending"},
	}}
	j.Event.ContentAttributes.ExternalEcho = true
	return j
}

func commentBot(echoes *commentecho.Echoes) (*Bot, *labelChatwoot, *memory.History) {
	cw := &labelChatwoot{}
	b, h := newBot(&cw.fakeChatwoot, &fakeLLM{reply: "x"})
	b.opts.Chatwoot = cw
	if echoes != nil {
		b.opts.CommentEchoes = echoes
	}
	return b, cw, h
}

func TestCommentLead_EchoOfAPrivateReplyGetsLabelAndNote(t *testing.T) {
	// Arrange: the bot has been chatting, so an unknown page message would
	// count as staff taking over.
	echoes := &commentecho.Echoes{}
	echoes.Put("m_9", commentecho.Lead{CommentID: "c1", Message: "สนใจครับ\nราคา**เท่าไหร่**", CommentedAt: time.Now()})
	b, cw, h := commentBot(echoes)
	h.Append(1, 7, memory.Turn{Role: "assistant", Content: "earlier bot reply"})

	// Act
	b.Process(context.Background(), pageEcho("m_9", "สวัสดีครับ ขอบคุณที่สนใจนะครับ"))

	// Assert
	got := cw.joined()
	if len(cw.labels) != 1 || cw.labels[0] != CommentLeadLabel {
		t.Errorf("labels = %v", cw.labels)
	}
	if !strings.Contains(got, "note:💬") || !strings.Contains(got, `"สนใจครับ ราคาเท่าไหร่"`) || !strings.Contains(got, "24 hour message window") {
		t.Errorf("note missing or not cleaned: %s", got)
	}
	if strings.Contains(got, "handoff") {
		t.Errorf("a private reply is the page's message, not staff taking over: %s", got)
	}
	turns := h.Get(1, 7)
	if last := turns[len(turns)-1]; last.Role != "assistant" || !strings.HasPrefix(last.Content, memory.PageMessagePrefix) {
		t.Errorf("history must mark it as the page's message: %+v", last)
	}
}

func TestCommentLead_OtherEchoesKeepTodaysBehaviour(t *testing.T) {
	cases := map[string]struct {
		echoes   *commentecho.Echoes
		sourceID string
	}{
		"unknown message id": {&commentecho.Echoes{}, "m_other"},
		"no source id":       {&commentecho.Echoes{}, ""},
		"feature off":        {nil, "m_9"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			if c.echoes != nil {
				c.echoes.Put("m_9", commentecho.Lead{CommentID: "c1"})
			}
			b, cw, h := commentBot(c.echoes)
			h.Append(1, 7, memory.Turn{Role: "assistant", Content: "earlier bot reply"})

			// Act
			b.Process(context.Background(), pageEcho(c.sourceID, "staff typed this in the Page inbox"))

			// Assert: staff took over, as before; no comment note.
			got := cw.joined()
			if cw.labels != nil && strings.Contains(strings.Join(cw.labels, ","), CommentLeadLabel) {
				t.Errorf("labels = %v", cw.labels)
			}
			if strings.Contains(got, "note:💬") || !strings.Contains(got, "handoff") {
				t.Errorf("got %s", got)
			}
		})
	}
}

func TestCommentLead_EchoIsUsedOnce(t *testing.T) {
	// Arrange: Meta can deliver the same echo twice.
	echoes := &commentecho.Echoes{}
	echoes.Put("m_9", commentecho.Lead{CommentID: "c1", Message: "สนใจ"})
	b, cw, _ := commentBot(echoes)

	// Act
	b.Process(context.Background(), pageEcho("m_9", "hi"))
	b.Process(context.Background(), pageEcho("m_9", "hi"))

	// Assert
	if n := strings.Count(cw.joined(), "note:💬"); n != 1 || len(cw.labels) != 1 {
		t.Errorf("notes %d labels %v", n, cw.labels)
	}
}

func TestQuoteLine_ClipsLongComments(t *testing.T) {
	// Arrange
	long := strings.Repeat("ก", maxNoteComment+50)

	// Act
	got := quoteLine(long)

	// Assert
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != maxNoteComment+1 {
		t.Errorf("got %d runes", len([]rune(got)))
	}
}
