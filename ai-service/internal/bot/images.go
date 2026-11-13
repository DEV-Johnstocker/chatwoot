package bot

// Images the owner lets the bot send (internal/images): where the model
// writes profile.ImageMarker(name) the bot sends that picture after its text,
// each at most once per chat and at most two per reply. The model never
// describes reviews itself. Issue: DEV-Johnstocker/chatwoot#1

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// ImageStore gives the bot each account's images (images.Store).
type ImageStore interface {
	List(ctx context.Context, accountID int64) ([]images.Meta, error)
	Find(ctx context.Context, accountID int64, name string) (images.Image, bool, error)
}

// AttachmentSender sends a file to the customer (chatwoot.Client).
type AttachmentSender interface {
	SendAttachment(ctx context.Context, accountID, conversationID int64, content, filename, contentType string, data []byte) error
}

// maxImagesPerReply bounds how many pictures one reply may send.
const maxImagesPerReply = 2

// RecalledImage is the name a chat recalled from Chatwoot gives an image the
// bot sent earlier: which one is unknown, so its caption decides.
const RecalledImage = "?"

var (
	// imageMarker matches profile.ImageMarker and near-misses ("[[ image: x ]]").
	imageMarker = regexp.MustCompile(`(?i)\[\[\s*image\s*:\s*([^\[\]\n]{1,80}?)\s*\]\]`)
	// imageMarkerLine is a marker with the break and spaces before it.
	imageMarkerLine = regexp.MustCompile(`(?i)\n?[ \t]*\[\[\s*image\s*:\s*[^\[\]\n]{1,80}?\s*\]\][ \t]*`)
)

// ImageSent stands for a sent picture in Answer's text (answer-eval), so the
// judge sees which image the customer got.
func ImageSent(name string) string { return "[ส่งรูป: " + name + "]" }

// takeImageMarkers removes every image marker and returns the names asked
// for, in order, without repeats.
func takeImageMarkers(s string) (string, []string) {
	var names []string
	seen := map[string]bool{}
	for _, m := range imageMarker.FindAllStringSubmatch(s, -1) {
		if k := images.Key(m[1]); !seen[k] {
			seen[k] = true
			names = append(names, m[1])
		}
	}
	if len(names) == 0 {
		return s, nil
	}
	return strings.TrimSpace(imageMarkerLine.ReplaceAllLiteralString(s, "")), names
}

// imageSentBefore reports whether an earlier bot reply in this chat already
// sent img: its marker, or a recalled image with the same caption.
func imageSentBefore(history []memory.Turn, img images.Meta) bool {
	for _, t := range history {
		if t.Role != "assistant" {
			continue
		}
		for _, m := range imageMarker.FindAllStringSubmatch(t.Content, -1) {
			if images.Key(m[1]) == images.Key(img.Name) ||
				(m[1] == RecalledImage && img.Caption != "" && strings.Contains(t.Content, img.Caption)) {
				return true
			}
		}
	}
	return false
}

// canSendImages reports whether this bot can send attachments at all.
func (b *Bot) canSendImages() bool {
	if b.opts.Images == nil {
		return false
	}
	_, ok := b.opts.Chatwoot.(AttachmentSender)
	return ok || b.opts.Chatwoot == nil // nil: answer-eval, which only shows the marker
}

// imageInfos lists the account's images for the prompt. A failure means none.
func (b *Bot) imageInfos(ctx context.Context, log *slog.Logger, acct int64) []profile.ImageInfo {
	if !b.canSendImages() {
		return nil
	}
	list, err := b.opts.Images.List(ctx, acct)
	if err != nil {
		log.Warn("image list lookup failed", "error", err)
		return nil
	}
	out := make([]profile.ImageInfo, len(list))
	for i, m := range list {
		out[i] = profile.ImageInfo{Name: m.Name, When: m.When}
	}
	return out
}

// imagesToSend returns the pictures a reply asked for that exist and were
// not sent in this chat before, at most maxImagesPerReply.
func (b *Bot) imagesToSend(ctx context.Context, log *slog.Logger, acct, conv int64, history []memory.Turn, names []string) []images.Image {
	if !b.canSendImages() {
		return nil
	}
	var out []images.Image
	for _, name := range names {
		if len(out) == maxImagesPerReply {
			break
		}
		img, ok, err := b.opts.Images.Find(ctx, acct, name)
		if err != nil {
			log.Warn("image lookup failed", "error", err)
			continue
		}
		if !ok || imageSentBefore(history, img.Meta) || b.imageRemembered(acct, conv, img.Name) {
			continue
		}
		out = append(out, img)
	}
	return out
}

// sendImages sends the pictures and returns the markers of those that went out.
func (b *Bot) sendImages(ctx context.Context, log *slog.Logger, acct, conv int64, imgs []images.Image) []string {
	sender, ok := b.opts.Chatwoot.(AttachmentSender)
	if !ok {
		return nil
	}
	var sent []string
	for _, img := range imgs {
		if err := sender.SendAttachment(ctx, acct, conv, img.Caption, img.Filename(), img.ContentType, img.Data); err != nil {
			log.Error("send image failed", "image_id", img.ID, "error", err)
			continue
		}
		b.rememberImage(acct, conv, img.Name)
		sent = append(sent, profile.ImageMarker(img.Name))
		log.Info("image sent", "image_id", img.ID, "bytes", len(img.Data))
	}
	return sent
}

// imagesRemembered bounds imagesSent; the oldest entries go first.
const imagesRemembered = 20000

type imageKey struct {
	acct, conv int64
	name       string
}

// imageRemembered reports whether this chat got the image, beyond the
// history's turn limit and 24 h expiry (a restart is covered by recall).
func (b *Bot) imageRemembered(acct, conv int64, name string) bool {
	b.imagesMu.Lock()
	defer b.imagesMu.Unlock()
	_, ok := b.imagesSent[imageKey{acct, conv, images.Key(name)}]
	return ok
}

func (b *Bot) rememberImage(acct, conv int64, name string) {
	b.imagesMu.Lock()
	defer b.imagesMu.Unlock()
	if b.imagesSent == nil {
		b.imagesSent = map[imageKey]time.Time{}
	}
	if len(b.imagesSent) >= imagesRemembered {
		var oldest imageKey
		var at time.Time
		for k, t := range b.imagesSent {
			if at.IsZero() || t.Before(at) {
				oldest, at = k, t
			}
		}
		delete(b.imagesSent, oldest)
	}
	b.imagesSent[imageKey{acct, conv, images.Key(name)}] = time.Now()
}
