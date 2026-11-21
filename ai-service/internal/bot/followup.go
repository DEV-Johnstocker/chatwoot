package bot

import (
	"context"
	"slices"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
)

// A follow-up (internal/nudge) may end with one review picture the owner
// ticked "use in follow-ups" on /admin, one this customer has not had in
// this chat or in their previous chat (90 days, verified channels only). It
// goes out after the follow-up's text and without a caption: the text
// already speaks to the customer, and a caption would be one more message.
// Issue: DEV-Johnstocker/chatwoot#1

// followUpEarlierTurns: the previous chat's turns checked for pictures
// (Chatwoot's newest page), more than the few the model is shown.
const followUpEarlierTurns = 20

// FollowUpPicture sends the first ticked picture (by ID) this customer has
// not had. It reports whether one went out; an error means the lookup or the
// send failed and nothing was sent. verified says the channel proves who the
// customer is (customerVerified), so their previous chat may be read.
func (b *Bot) FollowUpPicture(ctx context.Context, acct, inbox, contact, conv int64, verified bool) (bool, error) {
	sender, ok := b.opts.Chatwoot.(AttachmentSender)
	if !ok || b.opts.Images == nil || b.opts.HistoryReader == nil {
		return false, nil
	}
	ticked, err := b.followUpPictures(ctx, acct)
	if err != nil || len(ticked) == 0 {
		return false, err
	}
	// Without this chat's messages there is no telling what the customer
	// already got, so nothing is sent rather than a repeat.
	history, err := b.opts.HistoryReader.RecentTurns(ctx, acct, conv, 0)
	if err != nil {
		return false, err
	}
	log := b.opts.Logger.With("account_id", acct, "conversation_id", conv)
	if verified {
		if e, ok := b.previousChat(ctx, log, acct, inbox, contact, conv, followUpEarlierTurns); ok {
			history = append(e.Turns, history...)
		}
	}
	for _, m := range ticked {
		if imageSentBefore(history, m) || b.imageRemembered(acct, conv, m.Name) {
			continue
		}
		img, found, err := b.opts.Images.Find(ctx, acct, m.Name)
		if err != nil {
			return false, err
		}
		if !found {
			continue // removed since the list was read
		}
		if err := sender.SendAttachment(ctx, acct, conv, "", img.Filename(), img.ContentType, img.Data); err != nil {
			return false, err
		}
		b.rememberImage(acct, conv, img.Name)
		log.Info("follow-up picture sent", "image_id", img.ID, "bytes", len(img.Data))
		return true, nil
	}
	return false, nil
}

// followUpPictures lists the account's ticked pictures, oldest first.
func (b *Bot) followUpPictures(ctx context.Context, acct int64) ([]images.Meta, error) {
	list, err := b.opts.Images.List(ctx, acct)
	if err != nil {
		return nil, err
	}
	var out []images.Meta
	for _, m := range list {
		if m.FollowUp {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, c images.Meta) int { return int(a.ID - c.ID) })
	return out, nil
}
