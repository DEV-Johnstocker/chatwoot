# Ad tracking — contract

Know which Click-to-Messenger ad (or m.me link) brought each customer, which
ads lead to a sale, and let the bot use the ad as a product hint.
Issues: DEV-Johnstocker/chatwoot#1, epic #154 (children #155, #156, #157).

## Scope

| Part | Issue | State |
|---|---|---|
| Messenger pass-through in front of Chatwoot's `/bot`; save referrals (`chat_referrals`, schema v26) | #155 | done (PR #158, #159) |
| Leads **Ad** column; Sales per-ad table (first/last touch); `chat_customers` (v27) | #156 | done |
| Bot gets the latest ad (≤ 7 days) as a product hint, never prices | #157 | planned |

## How it works (#155)

```
Facebook ──► ai-service POST /webhooks/messenger ──► Chatwoot /bot (same bytes, same X-Hub-* headers)
                │ 1. size limit (1 MiB) → 413
                │ 2. X-Hub-Signature-256 with MESSENGER_APP_SECRET → 401
                │ 3. forward; Chatwoot's status and reply go back to Facebook
                │ 4. only if Chatwoot answered 2xx: parse and save referrals
                ▼
            chat_referrals (page_id, psid, source, ref, ad_id, ad_title, referred_at)
```

- `GET /webhooks/messenger` (Facebook's subscription check) is passed to
  Chatwoot unchanged; Chatwoot owns the verify token.
- The forward sets `X-Forwarded-Proto: https` (Chatwoot runs with FORCE_SSL in
  production) and never follows redirects.
- Referrals are read from `messaging[].referral`, `messaging[].postback.referral`
  and `messaging[].message.referral`; echoes of the Page's own messages are skipped.
- Settings: `MESSENGER_APP_SECRET` and `CHATWOOT_MESSENGER_URL`, both or neither.
  Off without a database.

## Exclusions

- Instagram Direct and WhatsApp click-to-chat ads.
- Ad spend, cost per chat and campaign names (Meta Marketing API).
- Changing Chatwoot's code.
- Purchases that never appear in a chat.
- Deleting a contact's referrals when the contact is deleted in Chatwoot: not
  in #155. The 90-day delete covers every row; a follow-up can hook the
  contact-deleted event.

## Blind spots (gates)

| Case | Required behaviour | Test |
|---|---|---|
| Saving fails (database down) | Already forwarded; WARN; Facebook gets Chatwoot's status | `TestProxy_SaveFailureStillDelivers` |
| Chatwoot slow or down | 502 after the timeout; nothing saved; Facebook retries | `TestProxy_ChatwootTimeoutReturns502AndSavesNothing` |
| Chatwoot answers an error | Its status goes back; nothing saved | `TestProxy_ChatwootErrorIsPassedOnAndNothingSaved` |
| Chatwoot redirects | Redirect passed back, not followed | `TestProxy_DoesNotFollowRedirects` |
| Same delivery retried | No second row | `TestPostgres_AddIsIdempotentAndLatestWins` |
| Forged, unsigned or malformed signature | 401, not forwarded, nothing saved | `TestProxy_RejectsBadOrMissingSignatureWithoutForwarding` |
| Oversized body | 413, not forwarded | `TestProxy_RejectsOversizedBody` |
| Forward changes the request | Body byte-identical, signature still verifies at Chatwoot | `TestProxy_ForwardsExactBytesAndSavesReferral` |
| Subscription check | Chatwoot's challenge returned unchanged | `TestProxy_HandshakePassesThrough` |
| Referral before Chatwoot has the contact | Saved by page + psid; linked when read (#156) | by design |
| Returning customer (referral without a message) | Saved | `TestParse_StandaloneShortlinkPostbackAndMessageReferral` |
| Non-ad source (`m.me?ref=`, Get Started) | Saved with its real source, never an ad | same test |
| Long or hostile headline | One line, ≤ 120 characters | `TestParse_CleansAndCapsFields` |
| Personal data in logs | No psid, message text, headline or secret logged | `TestProxy_LogsNoPersonalData` |
| Retention | Rows older than 90 days deleted daily | `TestPostgres_PruneDropsOnlyOldRows` |
| Subscription check that isn't one | 403; only `hub.mode`, `hub.verify_token`, `hub.challenge` forwarded; echo is plain text | `TestProxy_HandshakeRefusesAnythingButASubscriptionCheck`, `TestProxy_HandshakeForwardsOnlyTheCheckParameters` |
| Chatwoot down during a check | 502; the verify token never reaches the logs | `TestProxy_HandshakeFailureLogsNoVerifyToken` |
| Event without a timestamp | Skipped (would be saved as 1970) | `TestParse_SkipsEventsWithoutTimestamp` |
| Timestamp in seconds (standalone referral) or milliseconds (message) | Both read correctly | `TestParse_TimestampInSecondsOrMilliseconds` |
| Replayed signed delivery | Accepted risk: Facebook signs no timestamp. The unique key makes a replay a no-op here, and Chatwoot skips message ids it already has | `TestPostgres_AddIsIdempotentAndLatestWins` |

## Rules for readers (#156, #157)

- chat_referrals has no account column. Readers start from the account's own
  chats: `chat_customers` (schema v27) holds, per Messenger chat, the
  customer's Page-scoped id that Chatwoot's webhook carries
  (`contact_inbox.source_id`, written by the bot for `Channel::FacebookPage`
  customer messages only). Referrals are joined on that psid, so an account
  sees only referrals of customers who chatted in its own inboxes. A
  Page-scoped id belongs to one Page, so accounts do not mix. Accepted edge:
  if two accounts connect the same Page, both see that customer's referrals,
  since both have the chat. Never take a `page_id` or psid from a request.
- `chat_referrals_psid (psid, referred_at DESC, id DESC)` (v27) serves these
  joins; v26's index leads with `page_id`.
- #156 (Leads Ad column, Sales per-ad table): credit goes to the customer's
  first or last tap made before the sale was paid; taps after payment get
  none; paid chats without a tap share a "No ad" row; one customer counts once
  per ad.

## Acceptance commands

```bash
cd ai-service
gofmt -l . && go vet ./... && go test -race ./...
TEST_DATABASE_URL=postgres://…scratch… go test -p 1 ./internal/referral/ ./internal/knowledge/ ./internal/restorecheck/
```

STAGING check (after the owner approves pointing the Meta app's Messenger
callback at `https://<staging host>/webhooks/messenger`):
1. Message the test Page through `m.me/<page>?ref=adtest1`.
2. The chat appears in Chatwoot and the bot replies as before.
3. `chat_referrals` has one row with `source = SHORTLINK`, `ref = adtest1`.

## Release notes

- Schema v26 is additive (one new table, two indexes, `lock_timeout` 5 s).
  Rolling back the code is safe; the table stays unused.
- PRODUCTION: rebuild `/opt/aisvc-backup/restore-check` for v26 → backup →
  deploy (Migrate applies v26) → check `chat_referrals` exists → set the two
  env values → add the public route → move the Meta callback. Each step on the
  owner's go.
- The Page must be subscribed to `messages`, `messaging_postbacks` and
  `messaging_referrals` in the Meta app (unverified for `messaging_referrals`).

## Evidence

- 2026-11-23, local: unit and scratch-DB tests pass (PR #158).
- 2026-11-23, STAGING: Meta app callback moved to `/webhooks/messenger` (Meta's
  check passed through to Chatwoot); Page subscribed to `messaging_referrals`.
  Opening the Page's chat with `?ref=adtest1` saved one row (`SHORTLINK`,
  `adtest1`). Found: a standalone referral's timestamp is in seconds; fixed.
- Chatwoot's own `/bot` accepts any verify token
  (`config/initializers/facebook_messenger.rb` `valid_verify_token?` ignores
  its argument); the pass-through only narrows what is echoed.
