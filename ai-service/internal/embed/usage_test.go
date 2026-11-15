package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbed_AddsProviderUsageToCounter(t *testing.T) {
	// Arrange: two batches, each reporting 7 prompt tokens
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": vec(1)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]int{"prompt_tokens": 7, "total_tokens": 7}})
	}))
	defer srv.Close()
	ctx, counter := WithCounter(context.Background())

	// Act
	_, err := New(srv.URL, "k", "text-embedding-3-large", nil).Embed(ctx, make([]string, batchSize+1))

	// Assert
	tokens, model := counter.Get()
	if err != nil || tokens != 14 || model != "text-embedding-3-large" {
		t.Fatalf("tokens=%d model=%q err=%v", tokens, model, err)
	}
}

func TestEmbed_NoCounterInContextIsFine(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vec(1)}}, "usage": map[string]int{"prompt_tokens": 3}})
	}))
	defer srv.Close()

	// Act
	_, err := New(srv.URL, "k", "m", nil).Embed(context.Background(), []string{"x"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
}

func TestCounter_NilSafe(t *testing.T) {
	// Arrange
	var c *Counter

	// Act
	c.add(5, "m")
	tokens, model := c.Get()

	// Assert
	if tokens != 0 || model != "" {
		t.Fatalf("tokens=%d model=%q", tokens, model)
	}
}
