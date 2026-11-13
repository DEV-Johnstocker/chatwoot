package chatwoot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
)

// SendAttachment sends one file (an image) to the customer with optional
// text, as a multipart message (Chatwoot's attachments[] field). Messenger
// shows it as a picture. Issue: DEV-Johnstocker/chatwoot#1
func (c *Client) SendAttachment(ctx context.Context, accountID, conversationID int64, content, filename, contentType string, data []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fields := map[string]string{"content": content, "message_type": "outgoing", "private": "false"}
	for _, k := range []string{"content", "message_type", "private"} {
		if err := w.WriteField(k, fields[k]); err != nil {
			return err
		}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="attachments[]"; filename=%q`, filename))
	h.Set("Content-Type", contentType)
	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	path := c.convPath(accountID, conversationID, "/messages")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("api_access_token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("chatwoot %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &APIError{Path: path, Status: resp.StatusCode, Body: string(snippet)}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
