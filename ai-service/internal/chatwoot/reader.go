package chatwoot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
)

// Reader reads a conversation's recent messages. Chatwoot does not let Agent
// Bot tokens list messages, so it uses an agent account's token (the "AI
// Reader" user, with access only to the bot's inboxes).
type Reader struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewReader returns a Reader. baseURL has no trailing slash.
func NewReader(baseURL, token string, httpClient *http.Client) *Reader {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Reader{baseURL: baseURL, token: token, http: httpClient}
}

type listedMessage struct {
	ID          int64             `json:"id"`
	MessageType int               `json:"message_type"` // 0 incoming, 1 outgoing, 2 activity, 3 template
	Private     bool              `json:"private"`
	Content     string            `json:"content"`
	Attachments []json.RawMessage `json:"attachments"`
}

// RecentTurns returns the conversation's latest messages (Chatwoot's newest
// page, oldest first) as chat turns: customer messages as "user", public
// replies from the bot or staff (Messenger echoes included) as "assistant".
// Private notes, activity messages and the message skipID are left out.
func (r *Reader) RecentTurns(ctx context.Context, accountID, conversationID, skipID int64) ([]memory.Turn, error) {
	path := fmt.Sprintf("/api/v1/accounts/%d/conversations/%d/messages", accountID, conversationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api_access_token", r.token)
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatwoot %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, &APIError{Path: path, Status: resp.StatusCode, Body: string(snippet)}
	}
	var page struct {
		Payload []listedMessage `json:"payload"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&page); err != nil {
		return nil, fmt.Errorf("chatwoot %s: %w", path, err)
	}
	var turns []memory.Turn
	for _, m := range page.Payload {
		if t, ok := toTurn(m, skipID); ok {
			turns = append(turns, t)
		}
	}
	return turns, nil
}

func toTurn(m listedMessage, skipID int64) (memory.Turn, bool) {
	if m.Private || m.ID == skipID || (m.MessageType != 0 && m.MessageType != 1) {
		return memory.Turn{}, false
	}
	role := "user"
	if m.MessageType == 1 {
		role = "assistant"
	}
	content := m.Content
	if content == "" {
		if role == "assistant" || len(m.Attachments) == 0 {
			return memory.Turn{}, false
		}
		content = "[The customer sent a file]"
	}
	return memory.Turn{Role: role, Content: content}, true
}
