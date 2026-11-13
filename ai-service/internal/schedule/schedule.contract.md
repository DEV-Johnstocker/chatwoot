# Bot switch contract (issue #1)

## Scope
One switch per Chatwoot account for everything that writes to customers on
its own: **On**, **Off**, or **On during set hours** (weekly hours per day, one
IANA time zone, overnight windows, 00:00–00:00 or 00:00–23:59 = whole day).
Set on `/admin/hours`; stored in `bot_schedules` (schema v18).

While the switch says off:
- Chat bot: a customer message in a pending chat gets no reply and no model
  call. The chat is handed to staff (pending → open, routed) with one private
  note `ai-handoff:bot-off`, once per chat per 10 minutes. A reply already
  waiting out the reply delay is checked again and handed off instead.
- Comment auto-send: holds every comment (`HoldBotOff`); the owner can still
  send by hand on `/admin/comments`.

## Failure behaviour (owner decision 2026-11-14)
- Switch unreadable → the chat bot stays **on** (nobody left unanswered).
- Switch unreadable → comment auto-send **holds** (a click, not a lost lead).
- No row for an account → on (behaviour before v18).

## Not in scope
Chatwoot's own inbox settings (never changed); comment scoring and drafts
(owner-facing only); Test bot and MCP (no switch there yet); holidays.

## Acceptance gates
```bash
go test -race ./internal/schedule ./internal/bot ./internal/admin ./internal/comments
TEST_DATABASE_URL=<scratch> go test -p 1 ./internal/schedule ./internal/knowledge -run 'Postgres|V18'
```

## Evidence
- 2026-11-14 platform-qa run (LOCAL, standard depth): 8 findings, all fixed
  in this branch with regression tests (`qa_switch_test.go` in bot, admin,
  schedule; auto-send cases in `comments/autosend_test.go`).
- Migration and security reviews: RLS and API-role revoke added; no other
  blockers.
