package admin

// The review image box on the Business profile page: the owner uploads one
// picture of real customer reviews, which the bot sends where it writes
// profile.ReviewMarker. Issue: DEV-Johnstocker/chatwoot#1

import (
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/reviews"
)

// maxReviewForm bounds an upload: the image plus the caption and form overhead.
const maxReviewForm = reviews.MaxBytes + 64<<10

// reviewBox is what the page shows about the account's review image.
type reviewBox struct {
	Enabled bool         // a database is configured
	Set     bool         // an image is saved
	Caption string       // saved caption
	Preview template.URL // small JPEG data: URI (the admin CSP allows only data: images)
	Size    string       // e.g. "412 KB"
	Done    string       // "saved" or "removed" after a change
	Error   string
	MaxMB   int
	MaxCap  int
}

// loadReviewBox fills the box for an account; errors only show a message.
func (h *Handler) loadReviewBox(r *http.Request, acct int64) reviewBox {
	box := reviewBox{Enabled: h.Reviews != nil, MaxMB: reviews.MaxBytes >> 20, MaxCap: reviews.MaxCaptionRunes}
	switch d := r.URL.Query().Get("review"); d {
	case "saved", "removed":
		box.Done = d
	}
	if h.Reviews == nil {
		return box
	}
	img, ok, err := h.Reviews.Get(r.Context(), acct)
	if err != nil {
		h.Logger.Error("load review image", "account_id", acct, "error", err)
		box.Error = tr(langOf(r), "Could not load the review image. Check the database connection.")
		return box
	}
	if ok {
		box.Set, box.Caption = true, img.Caption
		box.Preview = h.reviewPreview(img)
		box.Size = strconv.Itoa((len(img.Data)+1023)/1024) + " KB"
	}
	return box
}

// submitReviewImage saves or removes the account's review image.
func (h *Handler) submitReviewImage(w http.ResponseWriter, r *http.Request) {
	if h.Reviews == nil {
		http.Error(w, "review image disabled: set DATABASE_URL", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxReviewForm)
	if err := r.ParseMultipartForm(maxReviewForm); err != nil {
		h.reviewFailed(w, r, 0, tr(langOf(r), "The image is too large or the form is broken. Images can be up to %d MB.", reviews.MaxBytes>>20))
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	acct, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("account")), 10, 64)
	if err != nil || acct < 1 {
		http.Error(w, "account must be a positive number", http.StatusBadRequest)
		return
	}
	if r.FormValue("action") == "remove" {
		if err := h.Reviews.Delete(r.Context(), acct); err != nil {
			h.Logger.Error("remove review image", "account_id", acct, "error", err)
			h.reviewFailed(w, r, acct, tr(langOf(r), "Could not remove the image. Try again."))
			return
		}
		h.record(r, acct, "review image removed", "", nil)
		http.Redirect(w, r, "/admin/business?account="+strconv.FormatInt(acct, 10)+"&review=removed#reviews", http.StatusSeeOther)
		return
	}
	caption := strings.TrimSpace(strings.ReplaceAll(r.FormValue("caption"), "\r\n", "\n"))
	data, err := readReviewFile(r)
	switch {
	case errors.Is(err, http.ErrMissingFile):
		err = h.Reviews.SetCaption(r.Context(), acct, caption) // only the caption changed
	case err == nil:
		err = h.Reviews.Save(r.Context(), reviews.Image{AccountID: acct, Caption: caption, Data: data})
	}
	if err != nil {
		h.Logger.Warn("save review image failed", "account_id", acct, "error", err)
		h.reviewFailed(w, r, acct, reviewError(langOf(r), err))
		return
	}
	h.record(r, acct, "review image saved", strconv.Itoa(len(data))+" bytes", nil)
	http.Redirect(w, r, "/admin/business?account="+strconv.FormatInt(acct, 10)+"&review=saved#reviews", http.StatusSeeOther)
}

// readReviewFile returns the uploaded file; http.ErrMissingFile when none.
func readReviewFile(r *http.Request) ([]byte, error) {
	file, _, err := r.FormFile("image")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, reviews.MaxBytes+1))
}

// reviewError is a message the owner can act on.
func reviewError(lang string, err error) string {
	switch {
	case errors.Is(err, reviews.ErrEmpty):
		return tr(lang, "Choose an image file first.")
	case errors.Is(err, reviews.ErrTooLarge):
		return tr(lang, "The image is too large or the form is broken. Images can be up to %d MB.", reviews.MaxBytes>>20)
	case errors.Is(err, reviews.ErrType):
		return tr(lang, "The file must be a JPG or PNG image.")
	case errors.Is(err, reviews.ErrCaption):
		return tr(lang, "The caption can be at most %d characters.", reviews.MaxCaptionRunes)
	}
	return tr(lang, "Could not save the image. Try again.")
}

// reviewFailed shows the business page again with the error in the box.
func (h *Handler) reviewFailed(w http.ResponseWriter, r *http.Request, acct int64, msg string) {
	if acct < 1 {
		acct = accountParam(r.FormValue("account"))
	}
	data := businessData{Enabled: h.Profiles != nil}
	data.Profile.AccountID = acct
	if h.Profiles != nil {
		if p, err := h.Profiles.Get(r.Context(), acct); err == nil {
			data.Profile = p
		}
	}
	data.Reviews = h.loadReviewBox(r, acct)
	data.Reviews.Done, data.Reviews.Error = "", msg
	h.renderBusiness(w, r, http.StatusUnprocessableEntity, data)
}

// previewCache keeps one thumbnail per account, so a page view does not
// decode the full image every time; a new upload changes UpdatedAt.
type previewCache struct {
	at  time.Time
	uri template.URL
}

// reviewPreview is the image as a small JPEG data: URI ("" if it cannot be read).
func (h *Handler) reviewPreview(img reviews.Image) template.URL {
	if v, ok := h.previews.Load(img.AccountID); ok && v.(previewCache).at.Equal(img.UpdatedAt) {
		return v.(previewCache).uri
	}
	thumb, err := reviews.Thumbnail(img.Data, 360)
	if err != nil {
		return ""
	}
	uri := template.URL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumb)) // #nosec G203 -- our own re-encoded JPEG
	h.previews.Store(img.AccountID, previewCache{at: img.UpdatedAt, uri: uri})
	return uri
}
