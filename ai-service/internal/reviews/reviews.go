// Package reviews keeps each business's review image: one picture of real
// customer reviews the owner uploads on /admin. Where the bot writes profile.ReviewMarker,
// the system sends this image to the customer, so the model never describes
// or invents reviews itself. Issue: DEV-Johnstocker/chatwoot#1
package reviews

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
	MaxBytes        = 5 << 20 // Messenger takes images up to 8 MB; 5 MB keeps uploads quick
	MaxCaptionRunes = 200
	MaxSide         = 2048 // pixels; bounds decode memory (about 16 MB) and refuses decompression bombs
)

// Image is one account's review image.
type Image struct {
	AccountID   int64
	Caption     string // sent with the image, e.g. "รีวิวจากผู้เรียนจริงครับ"
	ContentType string // image/jpeg or image/png
	Data        []byte
	UpdatedAt   time.Time
}

// Filename is the name the customer sees on the attachment.
func (i Image) Filename() string {
	if i.ContentType == "image/png" {
		return "reviews.png"
	}
	return "reviews.jpg"
}

// Errors from Check.
var (
	ErrEmpty    = errors.New("choose an image file")
	ErrTooLarge = fmt.Errorf("the image is over %d MB", MaxBytes>>20)
	ErrType     = errors.New("the file must be a JPG or PNG image")
	ErrCaption  = fmt.Errorf("the caption must be at most %d characters of text", MaxCaptionRunes)
)

// Check validates an upload and returns its real content type, read from the
// bytes (never the browser's claim). The image must decode as JPEG or PNG.
func Check(data []byte, caption string) (string, error) {
	switch {
	case len(data) == 0:
		return "", ErrEmpty
	case len(data) > MaxBytes:
		return "", ErrTooLarge
	case !utf8.ValidString(caption) || strings.ContainsRune(caption, 0) || utf8.RuneCountInString(caption) > MaxCaptionRunes:
		return "", ErrCaption
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

// Store keeps one review image per account.
type Store interface {
	// Get returns the account's image; ok is false when none is set.
	Get(ctx context.Context, accountID int64) (img Image, ok bool, err error)
	// Save replaces the account's image after Check.
	Save(ctx context.Context, img Image) error
	// Delete removes it; deleting nothing is not an error.
	Delete(ctx context.Context, accountID int64) error
	// SetCaption changes only the caption; ErrEmpty when no image is set.
	SetCaption(ctx context.Context, accountID int64, caption string) error
	// Has reports whether the account has an image, without loading it.
	Has(ctx context.Context, accountID int64) (bool, error)
}

// Prepare checks an upload and re-encodes it: hidden data (EXIF location,
// bytes appended after the image) is dropped, and the whole image is proven
// to decode, not just its header. It returns the type and the clean bytes.
func Prepare(data []byte, caption string) (string, []byte, error) {
	ct, err := Check(data, caption)
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
