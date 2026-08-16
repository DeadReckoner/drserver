# Axiom Dashboards — Query Reference

Everything below is live in the backend as of this change. The `drserver`
dataset is already receiving all of it, so every query here runs today with no
further setup.

Read the two-stream model first — it explains why some queries filter on `msg`
and others on `event_name`, and getting that backwards is the single easiest
way to build a panel that silently reads zero.

---

## 1. Two streams, one dataset

| | Operational logs | Product events |
|---|---|---|
| Question it answers | Is the server healthy? | Is the product working? |
| Filter | `msg == "request"`, `"failure"`, … | `msg == "product_event"` |
| Read by | Engineers, during an incident | The business, over months |
| Retention needed | 30 days | 24 months |

Both currently land in `drserver`. Product events are tagged
`stream == "product_event"` and carry a nested `event` object with the full
schema.

**To split them into `dr-product-prod` with long retention**, set two Fly
secrets — no code change, no deploy gap:

```bash
fly secrets set AXIOM_TOKEN=xaat-... AXIOM_PRODUCT_DATASET=dr-product-prod
```

Product events then ship directly to that dataset and stop appearing in
`drserver`. Until then everything works against `drserver`, which is why you
can start building now. If Axiom ingest ever fails, events automatically fall
back to the log stream rather than being lost.

**Change every `['drserver']` below to `['dr-product-prod']` once you cut
over**, and drop the `msg == "product_event"` filter along with the `event.`
field prefix (in the dedicated dataset the fields are top-level).

---

## 2. Field reference

### Operational: `msg == "request"` (one per HTTP request)

| Field | Notes |
|---|---|
| `status` | **Integer.** This is the field you were looking for — we do not emit `http.response.status_code`. |
| `duration_ms` | Integer, server-side handling time |
| `route` | Route *template* (`/api/v1/roadmaps/:roadmapid`), so it groups properly |
| `path` | Concrete path — high cardinality, don't group by it |
| `method`, `client_ip`, `bytes` | |
| `user_id`, `request_id` | `request_id` joins to every other line from the same request |
| `platform`, `app_version`, `build_number` | **New.** Present only on requests from the mobile app |
| `environment`, `service`, `release` | **New.** On *every* line. `service` is `app` or `scheduler`; `release` changes per deploy |

WebSocket sessions are `msg == "ws_connection"` with `session_ms`, deliberately
excluded from `msg == "request"` so they don't wreck latency percentiles.

### Operational: `msg == "failure"` (one per subsystem failure)

| Field | Values |
|---|---|
| `failure_category` | `auth`, `database`, `ai_provider`, `notification`, `billing`, `background_job`, `email`, `storage` |
| `error_code` | `timeout`, `canceled`, `unavailable`, `unauthorized`, `rate_limited`, `invalid_request`, `invalid_output`, `conflict`, `not_found`, `unknown` |
| `error` | Message text — for reading, not grouping |
| plus context | `provider`, `model`, `stage`, `job_id`, `roadmap_id`, `notification_type`, … |

Use this rather than `level == "ERROR"`. Level-based error counts sweep in
every stray error line in the codebase and become untrustworthy noise; this
stream is opt-in per call site and is safe to alert on.

### Operational: `msg == "roadmap generation finished"`

`job_id`, `roadmap_id`, `user_id`, `request_id`, `provider`, `model`,
`outcome` (`completed`/`failed`), `generation_ms`, `retry_count`, `error_code`.

### Product events: `msg == "product_event"`

Every event carries this envelope under `event`:

```
event._time              event.event_id           event.event_name
event.event_version      event.environment        event.source
event.user_id            event.anonymous_id       event.session_id
event.platform           event.app_version        event.build_number
event.request_id         event.service            event.release
event.properties.*
```

`event.source` is `server` (authoritative — observed by the backend),
`client` (best-effort — reported by the app), or `scheduler`.

**Always count users with `dcount(event.user_id)` and events with
`dcount(event.event_id)`.** Every event carries a unique `event_id` and the
client dedupes on retry, but a plain `count()` can still double-count across a
delivery edge case. `dcount` is always correct.

---

## 3. Production Health

### 5xx rate

```
['drserver']
| where msg == "request"
| summarize
    total = count(),
    errors = countif(status >= 500)
  by bin_auto(_time)
| extend error_rate = 100.0 * errors / total
```

### Latency percentiles by route

```
['drserver']
| where msg == "request"
| summarize
    p50 = percentile(duration_ms, 50),
    p95 = percentile(duration_ms, 95),
    p99 = percentile(duration_ms, 99),
    count()
  by route
| sort by p95 desc
```

### Error rate by app version — the release-regression panel

```
['drserver']
| where msg == "request" and isnotempty(app_version)
| summarize
    total = count(),
    errors = countif(status >= 500)
  by app_version, platform
| extend error_rate = 100.0 * errors / total
| sort by error_rate desc
```

### Failures by subsystem and cause

```
['drserver']
| where msg == "failure"
| summarize count() by failure_category, error_code, bin_auto(_time)
```

### Roadmap generation success rate and latency

```
['drserver']
| where msg == "roadmap generation finished"
| summarize
    attempts = count(),
    succeeded = countif(outcome == "completed"),
    p50_ms = percentile(generation_ms, 50),
    p95_ms = percentile(generation_ms, 95)
  by bin_auto(_time), model
| extend success_rate = 100.0 * succeeded / attempts
```

### Telemetry heartbeat — the monitor that catches total failure

```
['drserver']
| where msg == "telemetry_heartbeat"
| summarize count() by bin(_time, 5m)
```

Emitted every minute by the scheduler. **Alert when this drops below ~3 in 5
minutes.** Every other metric goes quiet both when nothing is happening and
when logging is broken; this is the only one whose absence is unambiguous.

### Deploy correlation

```
['drserver']
| where msg == "request"
| summarize errors = countif(status >= 500), total = count() by release
| extend error_rate = 100.0 * errors / total
```

Crash-free users are **already in Sentry** for both server and mobile. Link
that dashboard rather than rebuilding it here — Sentry has the stack traces.

---

## 4. Product Funnel and Activation

### Funnel counts

```
['drserver']
| where msg == "product_event"
| where event.event_name in (
    "app_first_open", "signup_started", "signup_completed",
    "onboarding_completed", "roadmap_generation_started",
    "roadmap_generation_succeeded", "roadmap_activated",
    "work_item_completed", "anchor_invite_sent", "anchor_invite_accepted",
    "trial_started", "subscription_activated"
  )
| summarize users = dcount(event.user_id) by event.event_name
```

Note the top two stages count *installs*, not users — before signup there is no
`user_id`. Use `dcount(event.anonymous_id)` for those:

```
['drserver']
| where msg == "product_event"
| where event.event_name in ("app_first_open", "signup_started")
| summarize installs = dcount(event.anonymous_id) by event.event_name
```

### Setup Activation — onboarding completed within 24h of signup

```
['drserver']
| where msg == "product_event"
| where event.event_name in ("signup_completed", "onboarding_completed")
| summarize
    signed_up = minif(_time, event.event_name == "signup_completed"),
    onboarded = minif(_time, event.event_name == "onboarding_completed")
  by user = tostring(event.user_id)
| where isnotnull(signed_up)
| extend activated = isnotnull(onboarded)
    and datetime_diff('hour', onboarded, signed_up) <= 24
| summarize
    users = count(),
    activated = countif(activated)
| extend setup_activation_rate = 100.0 * activated / users
```

### Value Activation — first action completed within 72h of signup

Same shape, swapping the second event and the window:

```
['drserver']
| where msg == "product_event"
| where event.event_name in ("signup_completed", "work_item_completed")
| summarize
    signed_up = minif(_time, event.event_name == "signup_completed"),
    first_done = minif(_time, event.event_name == "work_item_completed")
  by user = tostring(event.user_id)
| where isnotnull(signed_up)
| extend activated = isnotnull(first_done)
    and datetime_diff('hour', first_done, signed_up) <= 72
| summarize users = count(), activated = countif(activated)
| extend value_activation_rate = 100.0 * activated / users
```

### Retention by signup cohort

```
['drserver']
| where msg == "product_event"
| extend user = tostring(event.user_id), name = tostring(event.event_name)
| where isnotempty(user)
| summarize
    cohort = startofweek(minif(_time, name == "signup_completed")),
    last_active = maxif(_time, name == "work_item_completed")
  by user
| where isnotnull(cohort)
| extend week = datetime_diff('week', last_active, cohort)
| summarize users = count() by cohort, week
```

---

## 5. Execution and Accountability

### Weekly Executing Users

```
['drserver']
| where msg == "product_event"
| where event.event_name == "work_item_completed"
| summarize weu = dcount(event.user_id) by bin(_time, 7d)
```

### Completions by work item type

```
['drserver']
| where msg == "product_event"
| where event.event_name == "work_item_completed"
| summarize
    completions = dcount(event.properties.occurrence_id)
  by item_type = tostring(event.properties.item_type), bin_auto(_time)
```

Counted on `occurrence_id`, not `work_item_id` — a daily habit is one work item
but many occurrences, and counting the item collapses a month of effort into 1.

### Anchor coverage and anchored-vs-unanchored execution

```
['drserver']
| where msg == "product_event"
| where event.event_name in ("anchor_invite_sent", "anchor_invite_accepted")
| summarize users = dcount(event.user_id) by event.event_name
```

### Accountability alert delivery

```
['drserver']
| where msg == "product_event"
| where event.event_name startswith "accountability_alert_"
| summarize count() by event.event_name, bin_auto(_time)
```

> ⚠️ **Completion *rate* is not yet computable, and this is the one number you
> should not fake.** We emit `work_item_completed` with a correct
> `occurrence_id`, but not yet `work_item_became_due` — so there is a numerator
> and no denominator. A "completion rate" built on completions alone divides by
> items *created*, which flatters the number badly for anyone with recurring
> habits.
>
> The occurrence id is already deterministic (`hash(item_id, scheduled_date)`),
> so the scheduled scan that emits `work_item_became_due` will join to the
> completions we are recording from today — nothing collected now is wasted.
> Accountability Recovery Rate depends on the same event.
>
> Build the execution *volume* panels now; leave the rate panels until that
> lands.

---

## 6. Subscription and Revenue

All server-authoritative, sourced from the RevenueCat webhook. Sandbox
purchases and RC test events are excluded at the source, so these numbers are
real money only.

### Subscription lifecycle

```
['drserver']
| where msg == "product_event"
| where event.event_name in (
    "trial_started", "subscription_activated", "subscription_renewed",
    "subscription_canceled", "subscription_expired",
    "subscription_payment_failed", "subscription_reactivated", "refund_issued"
  )
| summarize events = dcount(event.event_id) by event.event_name, bin_auto(_time)
```

### Trial-to-paid conversion

```
['drserver']
| where msg == "product_event"
| where event.event_name in ("trial_started", "subscription_activated")
| summarize
    trial = minif(_time, event.event_name == "trial_started"),
    paid  = minif(_time, event.event_name == "subscription_activated")
  by user = tostring(event.user_id)
| where isnotnull(trial)
| summarize trials = count(), converted = countif(isnotnull(paid))
| extend conversion_rate = 100.0 * converted / trials
```

### Revenue, plan mix, and churn reasons

```
['drserver']
| where msg == "product_event"
| where event.event_name in ("subscription_activated", "subscription_renewed")
| summarize revenue = sum(todouble(event.properties.price_amount))
  by billing_period = tostring(event.properties.billing_period), bin(_time, 30d)
```

```
['drserver']
| where msg == "product_event"
| where event.event_name in ("subscription_canceled", "subscription_expired")
| summarize count() by reason = tostring(event.properties.reason)
```

`reason` separates voluntary churn (`unsubscribe`) from involuntary
(`billing_error`) — the same row in a naive cancellation count, but they call
for completely different responses.

### Paywall conversion

```
['drserver']
| where msg == "product_event"
| where event.event_name in ("paywall_viewed", "checkout_started", "trial_started")
| summarize users = dcount(event.user_id) by event.event_name
```

---

## 7. Verification

Every server event is verifiable end-to-end with a test account:

```
['drserver']
| where msg == "product_event"
| where event.user_id == "<test-user-uuid>"
| project _time, event.event_name, event.source, event.properties
| sort by _time asc
```

That timeline is the acceptance test — walk the app as the test user and watch
the funnel appear in order. Client events (`event.source == "client"`) may
arrive up to 30 seconds late; that is the flush interval, not a bug.

---

## 8. Privacy

Event properties pass a server-side **allowlist**. Anything not explicitly
permitted is dropped before storage — not filtered on read, dropped on write.
Goal titles, notes, anchor messages, AI prompts, generated roadmap content,
notification bodies, names, emails, phone numbers, and tokens cannot reach
Axiom by construction, and there is a test asserting it.

The one known PII leak has been closed: the Supabase auth path previously
logged the full user record (email, identities, sign-in history) at debug
level, guarded only by `LOG_LEVEL=info`. It is now redacted at the call site,
so raising the log level to debug is safe again.

Users are identified only by opaque UUID. Correlating an event to a person
requires the primary database, which is the intent.

---

## 9. Not yet available

Do not build panels for these — they will read empty:

| Metric | Blocked on |
|---|---|
| Completion **rate**, missed/skipped/rescheduled counts, Accountability Recovery Rate | `work_item_became_due` (scheduled occurrence scan) |
| Acquisition source / campaign | Attribution SDK (AppsFlyer/Branch or Apple AdServices). Separate project, not a logging field |
| Experiment id / variant | No feature-flag system exists yet |
| Goal discovery funnel | Client events reserved in the catalog; screens not yet instrumented |
| Anchor invite expiry | No expiry job exists |

Everything else on the original list is live.
