package bot

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/referral"
)

// Chats that came from a Messenger ad get the AdLabel label and the ad's
// name, id and tap date as conversation attributes, so staff see it in the
// Chatwoot inbox and can filter by it. Issues: DEV-Johnstocker/chatwoot#1, #154.
const (
	AdLabel           = "ad"
	AdNameAttribute   = "ad_name"
	AdIDAttribute     = "ad_id"
	AdTappedAttribute = "ad_tapped_at"
)

// AdLabelWindow is how recent a tap must be for the chat to count as coming
// from that ad.
const AdLabelWindow = 7 * 24 * time.Hour

// adMarkedCap bounds the memory of chats already marked; past it the memory
// starts over, which costs one repeat mark per chat.
const adMarkedCap = 10000

// AdFinder finds the newest ad a chat's customer tapped (referral.Postgres).
type AdFinder interface {
	LatestAd(ctx context.Context, accountID, conversationID int64, since time.Time) (referral.Referral, bool, error)
}

// adMarker is the part of the Chatwoot client that marks a chat.
type adMarker interface {
	Labeler
	AttributeSetter
}

// markAd labels a Messenger chat whose customer tapped an ad in the last
// AdLabelWindow. Each tap marks a chat once; failures are logged and never
// stop the reply.
func (b *Bot) markAd(ctx context.Context, log *slog.Logger, acct, conv int64) {
	finder, ok := b.opts.ChatLinks.(AdFinder)
	if !ok {
		return
	}
	marker, ok := b.opts.Chatwoot.(adMarker)
	if !ok {
		return
	}
	ad, found, err := finder.LatestAd(ctx, acct, conv, time.Now().Add(-AdLabelWindow))
	if err != nil {
		log.Warn("ad lookup failed", "error", err)
		return
	}
	if !found {
		return
	}
	key := convKey{acct, conv}
	seen := ad.AdID + "@" + strconv.FormatInt(ad.ReferredAt.UnixMilli(), 10)
	if b.adMarkedAs(key) == seen {
		return
	}
	if err := marker.AddLabel(ctx, acct, conv, AdLabel); err != nil {
		log.Warn("ad label not added", "error", err)
		return
	}
	if err := marker.SetConversationAttributes(ctx, acct, conv, adAttributes(ad)); err != nil {
		log.Warn("ad attributes not saved", "error", err)
		return
	}
	b.rememberAdMark(key, seen)
	log.Info("chat marked as from an ad", "ad_id", ad.AdID)
}

// adAttributes are the conversation attributes for one ad tap.
func adAttributes(ad referral.Referral) map[string]string {
	name := ad.AdTitle
	if name == "" {
		name = "Ad " + ad.AdID
	}
	return map[string]string{
		AdNameAttribute:   name,
		AdIDAttribute:     ad.AdID,
		AdTappedAttribute: ad.ReferredAt.UTC().Format(time.RFC3339),
	}
}

func (b *Bot) adMarkedAs(k convKey) string {
	b.adMu.Lock()
	defer b.adMu.Unlock()
	return b.adMarked[k]
}

func (b *Bot) rememberAdMark(k convKey, seen string) {
	b.adMu.Lock()
	defer b.adMu.Unlock()
	if b.adMarked == nil || len(b.adMarked) >= adMarkedCap {
		b.adMarked = map[convKey]string{}
	}
	b.adMarked[k] = seen
}
