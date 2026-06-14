# Notifications — Manual QA Script (TestFlight)

A step-by-step manual test pass for the notification system. The automated
suites (`go test ./...` and the `integration` job) cover the server logic and
real-Postgres behavior; **this script covers what they can't**: real push
delivery to a device, tap-routing into the app, real digest/anchor emails
through Resend, and the settings UI.

Run this against a TestFlight build before each release that touches
notifications. Estimated time: ~60–90 min for the full pass, ~20 min for the
smoke subset (marked 🔅).

---

## Prerequisites

- A TestFlight build with the notification settings screens wired to the API.
- **Two physical devices** (push doesn't deliver to the iOS Simulator), each
  signed into a **separate** account. Referred to as **A** (primary tester)
  and **B** (the friend/anchor/partner).
- Both devices: push permission **granted** (Settings → app → Notifications).
- Both accounts have at least one roadmap with milestones and tasks.
- A way to read each account's email inbox (digests + anchor summary go there).
- Optional but useful: access to the server logs (Fly) and the Resend
  dashboard to confirm sends, and `email_log` / `notification_delivery_log`
  rows in Supabase.

### One-time setup on device A

1. Open the app → Settings → Notifications.
2. Confirm the screen loads the user's settings (GET `/notification-settings`)
   without error and every toggle reflects the defaults (all on, quiet hours
   off, digest = none).
3. Set the device timezone correctly and confirm the app PATCHed
   `/users/:id` with the IANA TZ (e.g. `America/Los_Angeles`). Quiet hours and
   digests are all computed in this timezone — a wrong TZ invalidates those
   tests.

---

## Part 1 — Each notification delivers and routes 🔅

For each row: perform the trigger, confirm **A** receives the push within a few
seconds, and **tap it** — confirm it opens the screen in the "Routes to"
column. (Tap-routing uses the `data` payload IDs the server includes.)

| # | Notification | How to trigger | Expected push (to A) | Routes to |
|---|---|---|---|---|
| 1 | Friend request | B sends A a friend request | "New Friend Request" | Friend requests |
| 2 | Friend accepted | A requests B; B accepts | "Friend Request Accepted" | B's profile / friends |
| 3 | Friend declined | A requests B; B declines | "Friend Request Declined" | Friends |
| 4 | Anchor request | B requests A as anchor on a roadmap | "New Anchor Request" | Anchor requests |
| 5 | Anchor accepted | A requests B as anchor; B accepts | "Anchor Request Accepted" | The anchored roadmap |
| 6 | Anchor declined | A requests B as anchor; B declines | "Anchor Request Declined" | Roadmap / anchors |
| 7 | Habit anchor request | B requests A as habit anchor | "New Habit Anchor Request" | Habit anchor requests |
| 8 | Roadmap nudge | B (anchor) nudges A's roadmap | "Nudge from {B}" | The nudged roadmap |
| 9 | Habit nudge | B (anchor) nudges A's habit | "Nudge from {B}" | The nudged habit |
| 10 | New message | B sends A a message in a thread | sender name + preview | The thread |
| 11 | Roadmap generated | A starts an AI roadmap generation | "Your roadmap is ready for review!" | The new roadmap |
| 12 | Roadmap gen failed | (if reproducible) force a generation error | "Roadmap Generation Failed" | Roadmaps |
| 13 | Next milestone ready | A generates the next milestone | "Your next milestone is ready!" | The milestone |

> **Missed-task / falling-behind / at-risk / milestone-completed** are
> cron-driven and default to **digest** delivery, so they don't fire as
> immediate pushes here — they're verified in Part 4 (digests). To force an
> immediate one for routing, temporarily set that type's delivery to
> "immediate" in settings (Part 3 covers the toggle UI) and create an overdue
> task, then revert.

**Pass criteria:** every triggered push arrives, the title/body match, and the
tap opens the correct screen (no "dead" notification that just opens the home
screen).

---

## Part 2 — Global gating 🔅

Device A, in order:

1. **Push master off** — Settings → toggle **Push** off. Have B send a friend
   request. → **No push.** Toggle Push back on.
2. **Global mute** — set a global mute (mute-all) for 1 hour. Trigger any
   notification from B. → **No push.** Confirm the settings screen shows the
   active mute with its end time. Clear the mute. → notifications resume.
3. **F&A master off** — toggle **Friends & Accountability** off. B sends a
   friend request / nudge. → **No push.** Note that *My Goals* notifications
   (e.g. roadmap generated) **still fire**. Toggle F&A back on.

---

## Part 3 — Per-type toggles & overrides

1. **Per-type off** — turn off a single type (e.g. **Nudges**). Have B nudge A.
   → **No push.** Turn it back on → nudge delivers. Repeat for **Messages** and
   **Agent updates** (the three new toggles this release added).
2. **Shared toggles** — confirm there is **one** control for friend
   accept/decline (the "Friend requests" toggle), **one** for anchor
   accept/decline, and **one** for the habit-anchor request (rides "Anchor
   requests"). There should be **no** separate switches for the decline/accept
   variants.
3. **Apply-to-all OFF + per-friend override** — turn "Apply same to all
   friends" off. Open B's per-friend settings, disable **Messages** for B
   only. B messages A → **no push.** A different friend C messaging A → **push
   delivers** (override is per-target). Re-enable.
4. **Per-friend mute** — with apply-to-all off, mute B for 1 hour. Any F&A
   notification from B → **no push**; from C → delivers. (Mutes are ignored
   when apply-to-all is ON — verify that too: toggle apply-to-all on and
   confirm B's notifications come through despite the stored mute.)
5. **Anchored-goal override** — A is anchored by B on a roadmap. In the
   anchored-goal settings for that roadmap, mute it. Nudge/at-risk for that
   roadmap → suppressed; other roadmaps unaffected.

---

## Part 4 — Quiet hours

1. Set quiet hours to a window covering **now** in A's timezone (e.g.
   start = one hour ago, end = one hour from now).
2. Trigger an **immediate** F&A push from B (friend request). → **No push now.**
3. Wait until the quiet-hours window ends (or shorten it). → the **deferred**
   push arrives shortly after the window closes (it was queued, not dropped).
4. **Message exception:** while still in quiet hours, have B send A a **message**.
   Confirm it is **dropped, not deferred** — i.e. when quiet hours end you do
   **not** get a stale message push. (The message is still visible in the
   thread.)
5. Turn quiet hours off.

---

## Part 5 — Digest emails

Digests are **email**, not push. They send at the user's `digest_delivery_hour`
in their local timezone, and only if there's something queued.

1. **Daily digest** — set digest frequency = **daily**, delivery hour = the
   **next** clock hour in A's TZ. Generate digest-worthy activity (e.g. let a
   task go overdue so a `missed_task` is queued; or set `missed_task` to digest
   and miss one). At the top of the delivery hour:
   - A receives **one** digest email (check inbox; styling matches the
     lifecycle emails).
   - The email lists the queued items.
   - **No** duplicate email if you re-check a few minutes later (23h dedupe).
2. **Empty digest is silent** — clear all activity, keep digest = daily. At the
   delivery hour: **no email** is sent (no "you have nothing" email).
3. **Weekly digest** — set frequency = **weekly**, pick today's weekday and the
   next hour. Confirm it sends on that weekday/hour and not on other days.
4. Set frequency back to **none**.

---

## Part 6 — Anchor daily summary email

1. A is the **anchor** for B (accepted anchor on an active, non-paused,
   non-archived roadmap).
2. Give B some activity worth summarizing (e.g. B misses ≥3 tasks this week, or
   has overdue tasks, or completes tasks).
3. At **9am in A's local timezone**, A (the anchor) receives **one** "anchor
   daily summary" email listing B with a one-line status.
4. **Silent when quiet:** if B has nothing noteworthy (no misses, no overdue, no
   completions), A receives **no** anchor email. If A anchors multiple people
   and only some are noteworthy, the email lists only the noteworthy ones.

---

## Part 7 — Frequency cap

1. In a short window, trigger **many** (>10) immediate notifications to A from
   B across types (e.g. repeated nudges if rate-limits allow, friend/anchor
   churn). After ~10 immediate pushes in 24h, further immediate pushes are
   **suppressed** (capped) until the window rolls.
2. **Messages are exempt:** even after the cap is hit, B messaging A **still
   delivers** (an active chat must not be throttled).

> This one is awkward to trigger by hand because of per-type rate limits;
> it's primarily covered by unit tests. A light check (messages still flow
> after a burst) is enough for the manual pass.

---

## Part 8 — Regression smoke 🔅

- Badge count on A's app icon increments with unread notifications and clears
  when read.
- Killing/backgrounding the app still delivers pushes.
- No notification opens a blank/home screen (every tap routes somewhere).
- Settings screen round-trips: change several toggles, force-quit, reopen →
  changes persisted (GET reflects the PUT).

---

## Sign-off

| Area | Pass | Notes |
|---|---|---|
| Part 1 — delivery + routing | ☐ | |
| Part 2 — global gating | ☐ | |
| Part 3 — per-type / overrides | ☐ | |
| Part 4 — quiet hours | ☐ | |
| Part 5 — digest emails | ☐ | |
| Part 6 — anchor summary | ☐ | |
| Part 7 — frequency cap | ☐ | |
| Part 8 — regression smoke | ☐ | |

Tester: ______________  Build: ______________  Date: ______________

---

## Reference

- API + type→toggle map: [`notifications-api.md`](notifications-api.md)
- Server logic: `notifications/` package
- Cron jobs (digests, anchor batch, scans): `notifications/jobs/`
- Automated E2E (real Postgres): `test/integration/` (run with `-tags integration`)
