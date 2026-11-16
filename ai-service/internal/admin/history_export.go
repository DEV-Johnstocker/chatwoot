package admin

// "Download profile and answers": the owner's own copy of the business
// profile and every knowledge document (QA F-C-03). JSON holds both; CSV
// holds the documents in the data-file importer's columns (internal/feed),
// so the sheet can be loaded again as a data file. Neither holds tokens,
// keys or other secrets: only what the owner typed or imported. A POST, so
// net/http CrossOriginProtection refuses cross-site requests. Issue:
// DEV-Johnstocker/chatwoot#1

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// exportFile is the JSON download.
type exportFile struct {
	Format     string         `json:"format"` // "pronto-export"
	Version    int            `json:"version"`
	AccountID  int64          `json:"account_id"`
	ExportedAt time.Time      `json:"exported_at"`
	Profile    *exportProfile `json:"profile,omitempty"`
	Documents  []exportRow    `json:"documents"`
}

type exportProfile struct {
	BusinessName string `json:"business_name"`
	About        string `json:"about"`
	Tone         string `json:"tone"`
	Contact      string `json:"contact"`
	Rules        string `json:"rules"`
	// BankTransfer is the account the bot sends customers as written; the
	// owner's own data, needed to set the profile up again.
	BankTransfer string `json:"bank_transfer"`
	VoiceNotes   bool   `json:"voice_notes"`
	Images       bool   `json:"images"`
	Stickers     bool   `json:"stickers"`
}

// exportRow is one document with the importer's column names (internal/feed):
// question and answer for an FAQ, otherwise title and content. Kind, the
// original key and the inbox are extra columns the importer ignores.
type exportRow struct {
	ID        string `json:"id"`
	Question  string `json:"question,omitempty"`
	Answer    string `json:"answer,omitempty"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	LastDay   string `json:"last_day,omitempty"`
	Kind      string `json:"kind"`
	SourceKey string `json:"source_key,omitempty"`
	InboxID   int64  `json:"inbox_id,omitempty"`
}

var exportCSVHeader = []string{"id", "question", "answer", "title", "content", "start_date", "last_day", "kind", "source_key", "inbox_id"}

// exportKnowledge sends the download; format is "json" or "csv".
func (h *Handler) exportKnowledge(w http.ResponseWriter, r *http.Request, acct int64, format string) {
	if h.Profiles == nil && h.Knowledge == nil {
		http.Error(w, "downloads need DATABASE_URL", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	file := exportFile{Format: "pronto-export", Version: 1, AccountID: acct, ExportedAt: time.Now().UTC(), Documents: []exportRow{}}
	if h.Profiles != nil && format == "json" {
		p, err := h.Profiles.Get(ctx, acct)
		if err != nil {
			h.exportFailed(w, r, acct, err)
			return
		}
		file.Profile = profileExport(p)
	}
	if h.Knowledge != nil {
		docs, err := h.Knowledge.List(ctx, acct)
		if err != nil {
			h.exportFailed(w, r, acct, err)
			return
		}
		file.Documents = exportRows(docs, h.loc())
	}
	var body []byte
	var ctype string
	switch format {
	case "json":
		var err error
		if body, err = json.MarshalIndent(file, "", "  "); err != nil {
			h.exportFailed(w, r, acct, err)
			return
		}
		ctype = "application/json; charset=utf-8"
	case "csv":
		body, ctype = exportCSV(file.Documents), "text/csv; charset=utf-8"
	default:
		http.Error(w, "unknown format", http.StatusBadRequest)
		return
	}
	name := "pronto-account-" + strconv.FormatInt(acct, 10) + "-" + time.Now().In(h.loc()).Format(time.DateOnly) + "." + format
	h.record(r, acct, "profile and answers downloaded", strings.ToUpper(format)+", "+strconv.Itoa(len(file.Documents))+" documents", nil)
	h.Logger.Info("profile and answers downloaded", "account_id", acct, "format", format, "documents", len(file.Documents), "remote_addr", r.RemoteAddr)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

func (h *Handler) exportFailed(w http.ResponseWriter, r *http.Request, acct int64, err error) {
	h.Logger.Error("download failed", "account_id", acct, "error", err)
	data := h.loadHistory(r, acct)
	data.Error = tr(langOf(r), "Could not make the file. Check the database connection and try again.")
	h.renderPage(w, r, http.StatusInternalServerError, "history.html", data)
}

func profileExport(p profile.Profile) *exportProfile {
	return &exportProfile{BusinessName: p.BusinessName, About: p.About, Tone: p.Tone, Contact: p.Contact, Rules: p.Rules,
		BankTransfer: p.BankTransfer, VoiceNotes: p.Features.Voice, Images: p.Features.Images, Stickers: p.Features.Stickers}
}

// exportRows turns documents into importer rows with unique ids.
func exportRows(docs []knowledge.Document, loc *time.Location) []exportRow {
	out := make([]exportRow, 0, len(docs))
	seen := map[string]bool{}
	for _, d := range docs {
		row := exportRow{ID: exportID(d), Kind: string(d.Kind), SourceKey: d.SourceKey, InboxID: d.InboxID,
			StartDate: startDate(d, loc), LastDay: lastDay(d, loc)}
		if seen[row.ID] {
			row.ID += "-" + strconv.FormatInt(d.ID, 10)
		}
		seen[row.ID] = true
		if d.Kind == knowledge.KindFAQ {
			row.Question, row.Answer = d.Title, d.Content
		} else {
			row.Title, row.Content = d.Title, d.Content
		}
		out = append(out, row)
	}
	return out
}

// exportID is a document's id for the importer: its own key (a data file's
// row id without the "feed:<n>:" prefix), else "doc-<ID>".
func exportID(d knowledge.Document) string {
	key := d.SourceKey
	if rest, ok := strings.CutPrefix(key, "feed:"); ok {
		if _, row, ok := strings.Cut(rest, ":"); ok {
			key = row
		}
	}
	if key == "" {
		return "doc-" + strconv.FormatInt(d.ID, 10)
	}
	return key
}

// exportCSV writes rows as UTF-8 CSV with a byte-order mark, so Excel shows
// Thai correctly; the importer skips the mark.
func exportCSV(rows []exportRow) []byte {
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	cw := csv.NewWriter(&b)
	_ = cw.Write(exportCSVHeader)
	for _, r := range rows {
		inbox := ""
		if r.InboxID > 0 {
			inbox = strconv.FormatInt(r.InboxID, 10)
		}
		// Every cell that can carry outside text is guarded, ids and keys too
		// (an AI tool or data file sets them). Dates, kind and inbox are
		// written by the service and cannot start with a formula character.
		rec := []string{csvSafe(r.ID), csvSafe(r.Question), csvSafe(r.Answer), csvSafe(r.Title), csvSafe(r.Content),
			r.StartDate, r.LastDay, r.Kind, csvSafe(r.SourceKey), inbox}
		_ = cw.Write(rec)
	}
	cw.Flush()
	return b.Bytes()
}

// csvSafe keeps a cell from running as a formula when the file is opened in
// a spreadsheet (OWASP "CSV Injection"): a leading = + - @ tab or carriage
// return gets a ' in front. The page tells the owner to check those cells
// before loading the file again, since the ' may be kept.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
