# Contract: Facebook Page comment leads

Issue: DEV-Johnstocker/chatwoot#1. Owner request (2026-11-09): read comments on
our Facebook Page and open a Messenger conversation with people likely to
become customers.

## Scope

- `GET/POST /webhooks/meta`: Meta's Page `feed` webhook.
  - The `GET` subscription check echoes only a short `[A-Za-z0-9_-]` challenge, and only for the right verify token.
  - `POST` bodies must carry a valid `X-Hub-Signature-256`, the HMAC-SHA256 of the raw body with the app secret.
  - Only `feed` / `comment` / `add` events for `META_PAGE_ID` by someone other than the Page are stored. `remove` deletes an unanswered comment.
- Storage on schema v13 (`fb_comments`):
  - one row per Meta comment ID, so retries and replays add nothing;
  - row level security on, `anon`/`authenticated` revoked;
  - rows deleted 30 days after they arrive.
- A background worker (every 20 s) asks the main LLM to classify each new comment as `lead`, `question`, `complaint` or `other`, give a one-line reason, and draft a reply in the business's tone (`profile.Prompt`).
  - `other` is ignored.
  - A failed model call leaves the comment for the next pass.
- `/admin/comments` (same auth and cross-origin protection as every admin page):
  - waiting comments, newest first, each with the time left in Meta's 7-day window;
  - an editable draft, **Send private reply** and **Dismiss**;
  - a history of the last 20.
- Sending:
  - Graph `POST /{page-id}/messages` with `recipient.comment_id`, the token in the `Authorization` header (never in the URL).
  - The comment is claimed at the version the owner saw, so double clicks and two tabs send once.
  - Meta's permanent refusals (10900 already replied, 10903 cannot reply, 551 unavailable) move it to history.
  - Other failures leave it waiting with the error shown.
- Off unless `META_APP_SECRET`, `META_VERIFY_TOKEN`, `META_PAGE_ID` and `DATABASE_URL` are set. Without `META_PAGE_TOKEN` the queue fills but Send is refused.

## Not in scope (later, each needs the owner's decision)

- Automatic sending without approval.
- Public replies under the comment, which need `pages_manage_engagement`.
- Hiding spam comments.
- Instagram comments.
- More than one Page per service.
- Knowledge-base lookups in drafts. Drafts use the business profile only, and the owner reviews every one.
- The Meta app itself, App Review and the public route (phase 2, below).

## Acceptance gates

Run from `ai-service/`:

```bash
gofmt -l . && go vet ./... && go test -race ./...
TEST_DATABASE_URL=postgres://… go test -p 1 ./internal/comments/ ./internal/knowledge/ ./internal/admin/
```

1. A signed comment is stored once. A replay, Meta's retry, or a signature that is missing, wrong or for another body changes nothing; the bad signatures get 401 (`webhook_test.go`).
2. The subscription check refuses a wrong token, a wrong mode and a non-token challenge (`meta_test.go`, `webhook_test.go`).
3. The Page's own comments, edits, other Pages and non-comment events are skipped (`meta_test.go`).
4. Send refuses, without calling Meta and leaving the comment waiting, when any of these holds (`queue_test.go`, `comments_test.go`):
   - the reply is empty or longer than 2,000 characters;
   - the comment is older than 7 days;
   - the version is stale;
   - it belongs to another account;
   - no Page token is set.
5. One Send per comment, even when clicked twice (`queue_test.go`, `store_test.go` on Memory and Postgres).
6. `/admin/comments` needs the admin password, refuses cross-site POSTs, escapes comment text and shows only the chosen account (`comments_test.go`).
7. Migrating v12 to v13 on a Supabase-like database leaves `fb_comments` with RLS on and no `anon`/`authenticated` access, and its checks refuse a duplicate comment ID and an unknown status (`knowledge/postgres_test.go`).
8. Logs carry account and comment IDs only: never comment text, names, drafts or tokens.

## Phase 2: going live (owner + Meta, after this merges)

1. **Meta app** (owner, via the prompt in the session handoff):
   - a separate Business app with Webhooks and Messenger;
   - Business Verification;
   - privacy policy and data deletion URLs;
   - app roles.
2. **Public route** for `POST/GET /webhooks/meta` only, the way `/webhooks/chatwoot` is routed: Traefik path rule on the production domain, or a tunnel route with an Access bypass. `/admin` stays behind Access.
3. **Dokploy env**:
   - `META_APP_SECRET`, `META_VERIFY_TOKEN` (random, ≥16 characters), `META_PAGE_ID`, `META_ACCOUNT_ID`;
   - later `META_PAGE_TOKEN`, from a system user with the MESSAGING task.
   - Entered through the secret box, never chat.
4. **Release**:
   - backup;
   - deploy, which applies v13 on start;
   - check the required tables, then health.
   - **Rebuild and reinstall `/opt/aisvc-backup/restore-check`** from the deployed commit: it now requires `fb_comments` and schema 13. A stale binary fails the nightly drill.
5. **Subscribe**:
   - Meta webhook: Page object, `feed` field, the callback URL and the verify token;
   - `POST /{page-id}/subscribed_apps?subscribed_fields=feed`.
6. **Test** in development mode with app-role users:
   - comment → shows on `/admin/comments` → Send → private reply arrives in Messenger → their reply lands in Chatwoot.
   - Record the screencast for App Review.
7. **App Review** for `pages_messaging`, `pages_manage_metadata`, `pages_show_list`, `pages_read_engagement` and `pages_read_user_content`, then switch the app to Live.

## Evidence

- 2026-11-10, local:
  - `gofmt -l .` clean, `go vet ./...` clean, `go test -race ./...` all pass.
  - The full suite passes against a throwaway local `pgvector/pgvector:pg17` container (`go test -p 1 ./...` with `TEST_DATABASE_URL`).
