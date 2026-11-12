package websync

import (
	"context"
	"errors"
	"strings"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/feed"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webfetch"
)

// ErrNoEmbedder means no embeddings provider is set, so nothing can be added.
var ErrNoEmbedder = errors.New("set an embeddings provider on the AI providers page first")

// Preview is what a first check of a new source would find. It is read
// without saving anything, so a broken link or file is refused before it is
// added to the list.
type Preview struct {
	Rows   int      // data file: usable rows (at most feed.MaxRows)
	Format string   // data file: "CSV" or "JSONL"
	Title  string   // web page: its title
	Notes  []string // data file: skipped rows and why, or the row limit
}

// Preview downloads url as a web page or a data file (kind) and applies the
// same checks as a real check: the file is reachable and shared publicly, a
// data file has an id column and at least one usable row (rows past
// feed.MaxRows are dropped with a note), and a page has readable text.
// Error texts are safe to show to the owner or an AI tool.
func (s *Syncer) Preview(ctx context.Context, url, kind string) (Preview, error) {
	if s.Knowledge == nil || s.Knowledge.Embedder == nil || s.Knowledge.Embedder.Model() == "" {
		return Preview{}, ErrNoEmbedder
	}
	if kind == KindFeed {
		return s.previewFeed(ctx, url)
	}
	page, err := s.Fetcher.Fetch(ctx, url)
	if err != nil {
		return Preview{}, fetchErr(err)
	}
	title, text := webfetch.Text(page)
	if strings.TrimSpace(text) == "" {
		return Preview{}, errors.New("the page has no readable text")
	}
	return Preview{Title: title}, nil
}

func (s *Syncer) previewFeed(ctx context.Context, url string) (Preview, error) {
	page, err := s.Fetcher.FetchData(ctx, url)
	if err != nil {
		return Preview{}, fetchErr(err)
	}
	parsed, err := feed.Parse(page.Body)
	if err != nil {
		return Preview{}, err // feed errors are written for the owner
	}
	if len(parsed.Rows) == 0 {
		msg := "the file has no usable rows"
		if len(parsed.Problems) > 0 {
			msg += " (" + parsed.Problems[0] + ")"
		}
		return Preview{}, errors.New(msg)
	}
	return Preview{Rows: len(parsed.Rows), Format: parsed.Format, Notes: parsed.Problems}, nil
}

// fetchErr keeps known download errors and hides the rest (hosts, addresses).
func fetchErr(err error) error {
	if errors.Is(err, webfetch.ErrNotFound) {
		return webfetch.ErrNotFound
	}
	return errors.New(errText(err))
}
