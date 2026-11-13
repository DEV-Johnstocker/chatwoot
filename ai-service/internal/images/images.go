// Package images keeps the pictures a business lets the bot send: customer
// reviews per product, examples of work, and so on. Each has a name, a line
// saying when to send it, and a caption. Where the bot writes
// profile.ImageMarker(name) the system sends that picture, so the model never
// describes or invents reviews itself. Issue: DEV-Johnstocker/chatwoot#1
package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits.
const (
	MaxPerAccount   = 10
	MaxBytes        = 5 << 20 // Messenger takes images up to 8 MB; 5 MB keeps uploads quick
	MaxNameRunes    = 60
	MaxWhenRunes    = 300
	MaxCaptionRunes = 200
	MaxSide         = 2048 // pixels; bounds decode memory (about 16 MB) and refuses decompression bombs
)

// Meta is an image without its bytes: what the prompt and the admin list need.
type Meta struct {
	ID          int64
	AccountID   int64
	Name        string // what the bot writes in the marker, e.g. "รีวิว AutoCut"
	When        string // when to send it, e.g. "ลูกค้าขอดูรีวิวคอร์ส AutoCut"
	Caption     string // sent with the image
	ContentType string // image/jpeg or image/png
	Size        int
	UpdatedAt   time.Time
}

// Image is one picture with its bytes.
type Image struct {
	Meta
	Data []byte
}

// Filename is the name the customer sees on the attachment.
func (m Meta) Filename() string {
	if m.ContentType == "image/png" {
		return "image.png"
	}
	return "image.jpg"
}

// Key is how names are compared: case and spacing do not matter.
func Key(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// Errors.
var (
	ErrEmpty    = errors.New("choose an image file")
	ErrTooLarge = fmt.Errorf("the image is over %d MB", MaxBytes>>20)
	ErrType     = errors.New("the file must be a JPG or PNG image")
	ErrText     = fmt.Errorf("name 1-%d, when to send up to %d, caption up to %d characters of text", MaxNameRunes, MaxWhenRunes, MaxCaptionRunes)
	ErrName     = errors.New("another image already has this name")
	ErrFull     = fmt.Errorf("an account can have at most %d images", MaxPerAccount)
	ErrNotFound = errors.New("image not found")
)

// CleanText trims the texts and checks their limits. The name may not hold
// brackets, so it cannot break out of a marker.
func CleanText(m Meta) (Meta, error) {
	m.Name = strings.Join(strings.Fields(m.Name), " ")
	m.When = strings.TrimSpace(strings.ReplaceAll(m.When, "\r\n", "\n"))
	m.Caption = strings.TrimSpace(strings.ReplaceAll(m.Caption, "\r\n", "\n"))
	for _, s := range []string{m.Name, m.When, m.Caption} {
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return m, ErrText
		}
	}
	n := utf8.RuneCountInString(m.Name)
	if n < 1 || n > MaxNameRunes || strings.ContainsAny(m.Name, "[]") ||
		utf8.RuneCountInString(m.When) > MaxWhenRunes || utf8.RuneCountInString(m.Caption) > MaxCaptionRunes {
		return m, ErrText
	}
	return m, nil
}

// Check validates an upload and returns its real content type, read from the
// bytes (never the browser's claim). The image must decode as JPEG or PNG.
func Check(data []byte) (string, error) {
	switch {
	case len(data) == 0:
		return "", ErrEmpty
	case len(data) > MaxBytes:
		return "", ErrTooLarge
	}
	ct := http.DetectContentType(data)
	if ct != "image/jpeg" && ct != "image/png" {
		return "", ErrType
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || "image/"+format != ct || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxSide || cfg.Height > MaxSide {
		return "", ErrType
	}
	return ct, nil
}

// Prepare checks an upload and re-encodes it: hidden data (EXIF location,
// bytes appended after the image) is dropped, and the whole image is proven
// to decode, not just its header. It returns the type and the clean bytes.
func Prepare(data []byte) (string, []byte, error) {
	ct, err := Check(data)
	if err != nil {
		return "", nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, ErrType
	}
	var out bytes.Buffer
	if ct == "image/png" {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img)
	} else {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		return "", nil, ErrType
	}
	if out.Len() > MaxBytes {
		return "", nil, ErrTooLarge
	}
	return ct, out.Bytes(), nil
}

// Store keeps each account's images.
type Store interface {
	// List returns the account's images without their bytes, by name.
	List(ctx context.Context, accountID int64) ([]Meta, error)
	// Get returns one image; ErrNotFound when it is not the account's.
	Get(ctx context.Context, accountID, id int64) (Image, error)
	// Find returns the account's image with this name (Key); ok false if none.
	Find(ctx context.Context, accountID int64, name string) (Image, bool, error)
	// Add stores a new image (Prepare and CleanText run first) and returns its ID.
	Add(ctx context.Context, img Image) (int64, error)
	// Update changes an image's texts, and its picture when data is not nil.
	Update(ctx context.Context, m Meta, data []byte) error
	// Delete removes one image; deleting nothing is not an error.
	Delete(ctx context.Context, accountID, id int64) error
}
