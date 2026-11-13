package admin

// The images box on the Business profile page: up to images.MaxPerAccount
// named pictures (reviews per product, examples...) the bot may send, each
// with a line saying when. Issue: DEV-Johnstocker/chatwoot#1

import (
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
)

// maxImageForm bounds an upload: the image plus the texts and form overhead.
const maxImageForm = images.MaxBytes + 64<<10

type imageItem struct {
	ID      int64
	Name    string
	When    string
	Caption string
	Preview template.URL // small JPEG data: URI (the admin CSP allows only data: images)
	Size    string
}

// imageBox is what the page shows about the account's images.
type imageBox struct {
	Enabled bool
	Items   []imageItem
	CanAdd  bool
	Done    string // "added", "saved" or "removed" after a change
	Error   string
	Limits  struct{ Count, MB, Name, When, Caption int }
}

// loadImageBox fills the box for an account; errors only show a message.
func (h *Handler) loadImageBox(r *http.Request, acct int64) imageBox {
	box := imageBox{Enabled: h.Images != nil}
	box.Limits.Count, box.Limits.MB = images.MaxPerAccount, images.MaxBytes>>20
	box.Limits.Name, box.Limits.When, box.Limits.Caption = images.MaxNameRunes, images.MaxWhenRunes, images.MaxCaptionRunes
	switch d := r.URL.Query().Get("image"); d {
	case "added", "saved", "removed":
		box.Done = d
	}
	if h.Images == nil {
		return box
	}
	list, err := h.Images.List(r.Context(), acct)
	if err != nil {
		h.Logger.Error("load images", "account_id", acct, "error", err)
		box.Error = tr(langOf(r), "Could not load the images. Check the database connection.")
		return box
	}
	for _, m := range list {
		box.Items = append(box.Items, imageItem{ID: m.ID, Name: m.Name, When: m.When, Caption: m.Caption,
			Preview: h.imagePreview(r, m), Size: strconv.Itoa((m.Size+1023)/1024) + " KB"})
	}
	box.CanAdd = len(list) < images.MaxPerAccount
	return box
}

// submitImage adds, saves or removes one image.
func (h *Handler) submitImage(w http.ResponseWriter, r *http.Request) {
	if h.Images == nil {
		http.Error(w, "images disabled: set DATABASE_URL", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImageForm)
	if err := r.ParseMultipartForm(maxImageForm); err != nil {
		h.imageFailed(w, r, 0, tr(langOf(r), "The image is too large or the form is broken. Images can be up to %d MB.", images.MaxBytes>>20))
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	acct, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("account")), 10, 64)
	if err != nil || acct < 1 {
		http.Error(w, "account must be a positive number", http.StatusBadRequest)
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	meta := images.Meta{ID: id, AccountID: acct, Name: r.FormValue("name"), When: r.FormValue("when"), Caption: r.FormValue("caption")}
	data, ferr := readImageFile(r)
	done, action := "", r.FormValue("action")
	switch {
	case action == "remove" && id > 0:
		err, done = h.Images.Delete(r.Context(), acct, id), "removed"
	case action == "add":
		if errors.Is(ferr, http.ErrMissingFile) {
			err = images.ErrEmpty
		} else if err = ferr; err == nil {
			_, err = h.Images.Add(r.Context(), images.Image{Meta: meta, Data: data})
		}
		done = "added"
	case action == "save" && id > 0:
		if errors.Is(ferr, http.ErrMissingFile) {
			data, ferr = nil, nil // only the texts changed
		}
		if err = ferr; err == nil {
			err = h.Images.Update(r.Context(), meta, data)
		}
		done = "saved"
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err != nil {
		h.Logger.Warn("image change failed", "account_id", acct, "action", action, "error", err)
		h.imageFailed(w, r, acct, imageError(langOf(r), err))
		return
	}
	h.record(r, acct, "bot image "+done, strings.TrimSpace(meta.Name), nil)
	http.Redirect(w, r, "/admin/business?account="+strconv.FormatInt(acct, 10)+"&image="+done+"#images", http.StatusSeeOther)
}

// readImageFile returns the uploaded file; http.ErrMissingFile when none.
func readImageFile(r *http.Request) ([]byte, error) {
	file, _, err := r.FormFile("image")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, images.MaxBytes+1))
}

// imageError is a message the owner can act on.
func imageError(lang string, err error) string {
	switch {
	case errors.Is(err, images.ErrEmpty):
		return tr(lang, "Choose an image file first.")
	case errors.Is(err, images.ErrTooLarge):
		return tr(lang, "The image is too large or the form is broken. Images can be up to %d MB.", images.MaxBytes>>20)
	case errors.Is(err, images.ErrType):
		return tr(lang, "The file must be a JPG or PNG image.")
	case errors.Is(err, images.ErrText):
		return tr(lang, "Give the image a name (up to %d characters, no brackets); when to send up to %d and caption up to %d characters.",
			images.MaxNameRunes, images.MaxWhenRunes, images.MaxCaptionRunes)
	case errors.Is(err, images.ErrName):
		return tr(lang, "Another image already has this name. Use a different name.")
	case errors.Is(err, images.ErrFull):
		return tr(lang, "An account can have at most %d images. Remove one first.", images.MaxPerAccount)
	case errors.Is(err, images.ErrNotFound):
		return tr(lang, "That image no longer exists. Reload the page.")
	}
	return tr(lang, "Could not save the image. Try again.")
}

// imageFailed shows the business page again with the error in the box.
func (h *Handler) imageFailed(w http.ResponseWriter, r *http.Request, acct int64, msg string) {
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
	data.Images = h.loadImageBox(r, acct)
	data.Images.Done, data.Images.Error = "", msg
	h.renderBusiness(w, r, http.StatusUnprocessableEntity, data)
}

// previewCache keeps one thumbnail per image, so a page view does not decode
// every picture each time; a change bumps UpdatedAt.
type previewCache struct {
	at  time.Time
	uri template.URL
}

// imagePreview is the picture as a small JPEG data: URI ("" if unreadable).
func (h *Handler) imagePreview(r *http.Request, m images.Meta) template.URL {
	if v, ok := h.previews.Load(m.ID); ok && v.(previewCache).at.Equal(m.UpdatedAt) {
		return v.(previewCache).uri
	}
	img, err := h.Images.Get(r.Context(), m.AccountID, m.ID)
	if err != nil {
		return ""
	}
	thumb, err := images.Thumbnail(img.Data, 240)
	if err != nil {
		return ""
	}
	uri := template.URL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumb)) // #nosec G203 -- our own re-encoded JPEG
	h.previews.Store(m.ID, previewCache{at: m.UpdatedAt, uri: uri})
	return uri
}
