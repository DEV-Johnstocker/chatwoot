# Lessons from testing the Pronto bot

Newest first. Each entry: what was tested, the numbers, and the rule it
implies. The test sets are the owner's 95 rated chats ("john-95") and 49 real
customer chats ("real-49"), 3 runs each, on a local copy of the database,
judged by answer-eval plus a "closer" sales judge. Pass counts are means over
the 3 runs.

## 2026-11-12: after-promotion price rule (k11, on top of h11)

Changed: one profile rule. "Price after 30 Nov" moved from "hand off, admin
checks the price" to "answer yourself: promo ended, 9,900 baht, no coupon,
send the course link"; hand off only if the customer insists on 3,900. The
test chat E39 (dated 2 Dec) now expects that answer (owner decision).

| | h11 | k11 |
|---|---|---|
| john-95 pass | 54.3 | 55.7 (54.7 without E39) |
| real-49 pass | 26.0 | 27.0 |
| Fact accuracy | 96% / 98% | 95% / 99% |
| E39 | hand-off | 3 of 3 pass, correct price, link, no coupon |
| Hand-offs where an answer was expected | 0 | 3 (install problem x2, what is included x1) |

Kept. The three extra hand-offs are on chats the rule doesn't touch, with
the same install-problem rule as h11, so they read as run-to-run noise.
Rules implied:
- Owner decisions go in as rules; the test checks they cause no harm.
- When a decision changes what a test chat should get, update that chat's
  expectation in the same test and say so.

## 2026-11-12: round 6 bot code only, old profile kept (h11)

Changed: only the round 6 bot code (prompt and reply rules). Profile, tone,
contact and sheet stayed as in the baseline.

| | Baseline (g10) | h11 |
|---|---|---|
| john-95 pass | 53.3 | 54.3 |
| real-49 pass | 25.0 | 26.0 |
| Fact accuracy | 95% / 98% | 96% / 98% |
| Hand-offs where an answer was expected | 7 | 0 |
| "I don't know" replies | 16 | 3 |
| Next step fits (john-95) | 79% | 72% |

Kept: it wins on both sets with no loss of fact accuracy. The margin is
small (about 1 pass, ranges overlap). Rules implied:
- The code change alone helps; the earlier losses came from the profile
  edits. Test profile changes one at a time on top of this.
- Still missed: "1 licence = 1 computer" (N19 2 → 0 of 3), "the team
  continues in this chat" (E48 3 → 1). Add these to the profile rules as
  the next single-change tests.
- "Next step fits" dropped; watch it in the next test.

## 2026-11-12: core sales block in the profile, shortened rules (j10)

Changed (all at once): round 6 bot rules; profile "about" with a core sales
block (course link, price, coupon, end date, after-price, tax total, buying
steps); profile rules shortened from about 7,700 to about 3,700 characters to
fit the MCP limit; new tone; contact relabelled the AutoCut site "info and
example clips, not the place to buy".

| | Baseline (g10) | j10 |
|---|---|---|
| john-95 pass | 53.3 | 41.3 |
| real-49 pass | 25.0 | 23.7 |
| Fact accuracy | 95-98% | 96-99% |
| Replies with the course buy link | 42 | 48 |

Discarded. Rules implied:
- The core sales block works: buy links came back, and fact accuracy rose.
- Shortening the rules lost details the bot needed. Gone were "the team
  continues in this chat", "2,900 is the old price from the ad, apologise",
  and "1 licence = 1 computer". Don't shorten working rules; add to them.
- Calling a website "info and examples" in the contact field made the bot
  send its home page as the example link (39 of 432 replies).
- Changing four things in one test hid which one hurt. Change one per test.

## 2026-11-11: all product facts moved to the sheet (i10)

Changed: profile "about" with no product facts (everything in the sheet),
plus round 6 rules, new tone and updated sheet.

| | Baseline (g10) | i10 |
|---|---|---|
| john-95 pass | 53.3 | 36.3 |
| real-49 pass | 25.0 | 20.3 |
| "I don't know" replies | 16 | 2 |

Discarded. Rules implied:
- Search does not reliably return the buy link, price, coupon or tax total.
  Keep them in the profile.
- With no buy link in the profile, the bot took the sales website from the
  contact field (16 of 432 replies), with no coupon or price.
- The rule "never say ไม่ทราบ" works (16 → 2) and is kept.

## 2026-11-11: embedding model name (eval setup)

The knowledge base stores chunks under the plain model name
(`text-embedding-3-large`). An eval env using `openai/text-embedding-3-large`
made every search return nothing. Results from before this fix measured the
bot without its knowledge base. Production was not affected.

## 2026-11-11: owner decisions on content

- After 30 Nov 2026 the price returns to 9,900 baht with no coupon.
- Refunds: "no" in principle; if the customer insists, hand off and
  compromise.
- Promotion text names both dates, since the bot knows today's date.

## 2026-11-13: the closer reply rules were never shipped (PR #72)

Every rule test since 2026-11-11 (h11, k11, the refund rule) ran on a build of
the local branch `feat/question-cap`. That branch's reply rules (question cap,
top-closer rounds 3-6, the owner's round-6 ratings) were never merged, so
production answered with the older rules all along. Found when a build of
today's develop scored far below the 11 Nov build on the same chats.

Same chats, rules and grader (LOCAL, quick screen):

| | develop + #89 | PR #72 rebased |
|---|---|---|
| john sample | 21/57 | 32/57 |
| real sample | 8/30 | 18/30 |
| refund chats (real) | 0/6 | 5/6 |
| hand-offs where an answer was expected | 7 | 0 |

Rules implied:
- Build eval binaries from a pushed commit (`go version -m <binary>` must show
  `vcs.modified=false`) and record the commit next to the result.
- A rule change tested on top of unmerged code is not a result for production:
  rerun it on the shipped build before applying it.
- Owner decision, same day: a test question ("1+1") is answered, then gets one
  next step (usually a short question about their work), never a pitch.
