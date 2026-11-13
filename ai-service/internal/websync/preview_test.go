package websync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webfetch"
)

const sheetURL = "https://docs.google.com/spreadsheets/d/abc/export?format=csv&gid=1"

func TestPreview_GoodSheetCountsRowsAndSavesNothing(t *testing.T) {
	// Arrange
	s, ks, ss, pg, _, embeds := env()
	pg.data = "id,question,answer\nfaq-1,Price?,3900\nfaq-2,Mac?,Yes\n"

	// Act
	p, err := s.Preview(context.Background(), sheetURL, KindFeed)

	// Assert
	if err != nil || p.Rows != 2 || p.Format != "CSV" {
		t.Fatalf("preview = %+v, %v", p, err)
	}
	if len(ks.docs) != 0 || len(ss.saved) != 0 || *embeds != 0 {
		t.Fatalf("preview wrote: docs=%d saved=%d embeds=%d", len(ks.docs), len(ss.saved), *embeds)
	}
}

func TestPreview_RefusesBadDataFiles(t *testing.T) {
	cases := map[string]struct {
		data string
		err  error
		want string
	}{
		"private sheet": {err: webfetch.ErrGoogleSignIn, want: "Anyone with the link"},
		"no id column":  {data: "question,answer\nPrice?,3900\n", want: "id"},
		"header only":   {data: "id,question,answer\n", want: "no"},
		"web page":      {data: "<!doctype html><html><body>hi</body></html>", want: "web page"},
		"not found":     {err: webfetch.ErrNotFound, want: "not found"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			s, ks, ss, pg, _, _ := env()
			pg.data, pg.err = c.data, c.err

			// Act
			_, err := s.Preview(context.Background(), sheetURL, KindFeed)

			// Assert
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
			if len(ks.docs) != 0 || len(ss.saved) != 0 {
				t.Fatal("a refused preview wrote something")
			}
		})
	}
}

func TestPreview_MoreThan500RowsKeepsFirst500WithANote(t *testing.T) {
	// Arrange
	s, _, _, pg, _, _ := env()
	var b strings.Builder
	b.WriteString("id,question,answer\n")
	for i := range 520 {
		fmt.Fprintf(&b, "faq-%d,Q%d,A%d\n", i, i, i)
	}
	pg.data = b.String()

	// Act
	p, err := s.Preview(context.Background(), sheetURL, KindFeed)

	// Assert
	if err != nil || p.Rows != 500 || !strings.Contains(strings.Join(p.Notes, " "), "first 500") {
		t.Fatalf("preview = rows %d notes %v, %v", p.Rows, p.Notes, err)
	}
}

func TestPreview_WebPageNeedsText(t *testing.T) {
	// Arrange
	s, _, _, pg, _, _ := env()
	pg.html = "<title>Refund</title><main><p>No refunds for digital goods.</p></main>"

	// Act
	ok, okErr := s.Preview(context.Background(), url, KindPage)
	pg.html = "<html><body></body></html>"
	_, emptyErr := s.Preview(context.Background(), url, KindPage)

	// Assert
	if okErr != nil || ok.Title != "Refund" {
		t.Fatalf("page preview = %+v, %v", ok, okErr)
	}
	if emptyErr == nil || !strings.Contains(emptyErr.Error(), "no readable text") {
		t.Fatalf("empty page: %v", emptyErr)
	}
}

func TestPreview_NeedsEmbeddingsProvider(t *testing.T) {
	// Arrange
	s, _, _, pg, _, _ := env()
	pg.data = "id,question,answer\nfaq-1,Price?,3900\n"
	s.Knowledge = &knowledge.Service{}

	// Act
	_, err := s.Preview(context.Background(), sheetURL, KindFeed)

	// Assert
	if !errors.Is(err, ErrNoEmbedder) {
		t.Fatalf("err = %v, want ErrNoEmbedder", err)
	}
}

func TestPreview_UnknownErrorsStayGeneric(t *testing.T) {
	// Arrange
	s, _, _, pg, _, _ := env()
	pg.err = errors.New("dial tcp 10.0.0.5:443: secret internal detail")

	// Act
	_, err := s.Preview(context.Background(), sheetURL, KindFeed)

	// Assert
	if err == nil || strings.Contains(err.Error(), "10.0.0.5") {
		t.Fatalf("err = %v, want a generic message", err)
	}
}
