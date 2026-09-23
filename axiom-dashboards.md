# Axiom Dashboards — Query Reference

Everything below is live in the backend as of this change. The `drserver`
dataset is already receiving all of it, so every query here runs today with no
further setup.

Section 3 is the operational/server dashboard (endpoints, latency, error
rates); sections 4-6 are the product and revenue dashboards.

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

## 3. Server Metrics Dashboard

This is the operational dashboard, panel by panel. Everything here reads the
`msg == "request"` access-log stream emitted by `middleware.RequestLogger`
(`middleware/middleware.go:394`) — one line per completed HTTP request.

### 3.0 The base filter — put this on every panel

```
| where msg == "request" and environment == "production"
```

Three reasons it matters:

- **`msg == "request"`** excludes `ws_connection` (long-lived sessions whose
  duration is the session, not a response time — they would destroy your p95),
  `failure`, `panic recovered`, and the scheduler's own lines.
- **`environment == "production"`** keeps laptops and staging out. Defaults to
  `production` on Fly, `development` otherwise, so this is safe.
- Add **`and service == "app"`** if you ever want to exclude the `scheduler`
  process group. Scheduler serves no HTTP, so this is belt-and-braces.

**Exclude the health probes from aggregates.** Fly's `http_service` check
hits `/healthz` continuously (`fly.toml`), and `/health` answers liveness — on
a low-traffic app these two can be the large majority of all requests. Left in,
they drag every percentile toward ~1 ms and dilute every error-rate denominator
until a real spike is invisible. Add this as its own line, immediately after
the base filter and before the first `summarize`:

```
| where route !in ("/health", "/healthz")
```

Order matters only for readability — APL applies the filters together — but
keeping it directly under the base filter means you can see at a glance whether
a panel is probe-filtered.

The rule this dashboard follows, applied panel by panel below:

| Panel kind | Probes | Why |
|---|---|---|
| **Aggregates** — counts, rates, percentiles | **Out** | Probe volume distorts the number |
| **Listings** — slowest requests, 5xx detail, 404s | **In** | A row-per-request list isn't skewed by volume, and a slow or failing probe in the list is real signal |
| **Uptime panel** (§3.6) | **In, exclusively** | The probes *are* the subject |

Two panels need no filter because they already exclude probes as a side effect:
anything guarded by `isnotempty(app_version)` or `isnotempty(user_id)`. Probes
carry no client headers and no auth, so they drop out on their own. Those are
called out where they appear.

Every query below is already written with the correct treatment — paste them
as-is and you're done.

If you use Axiom dashboard variables, define `environment` and `service` once
and reference them as `$environment` / `$service` instead of hardcoding.

---

### 3.1 Which endpoints are being called

**Panel: Request volume by endpoint** (table — the core "what is being used")

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    requests = count(),
    users = dcount(user_id),
    p95_ms = percentile(duration_ms, 95),
    errors = countif(status >= 500)
  by method, route
| extend error_rate = round(100.0 * errors / requests, 2)
| sort by requests desc
```

One row per endpoint with volume, reach, speed, and health together. If you
build only one panel, build this one.

`route` is the *template* (`/api/v1/roadmaps/:roadmapid`), not the concrete
path — the middleware deliberately uses `c.FullPath()` so ids don't shatter
one endpoint into thousands of rows. Never group by `path`.

**Panel: Traffic over time, top endpoints** (stacked area)

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize requests = count() by route, bin_auto(_time)
| sort by requests desc
```

Set the panel's series limit to ~10 in Axiom's chart options, otherwise the
long tail turns it into spaghetti.

**Panel: Total request rate** (timechart — the top-left "is anything happening")

```
['drserver']
| where msg == "request" and environment == "production"
| summarize requests = count()
  by traffic = iff(route in ("/health", "/healthz"), "probe", "app"), bin_auto(_time)
```

Two series rather than an exclusion: real traffic and probe traffic on the same
axis. The `app` line is the one you read; the `probe` line going flat is an
independent signal that Fly stopped checking you. Excluding probes outright
here would throw that away, and plotting them merged would bury the `app` line
under a constant that has nothing to do with your users.

**Panel: Traffic by method**

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize requests = count() by method, bin_auto(_time)
```

---

### 3.2 Latency

**Panel: Latency percentiles, all endpoints** (timechart, 3 series)

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    p50 = percentile(duration_ms, 50),
    p95 = percentile(duration_ms, 95),
    p99 = percentile(duration_ms, 99)
  by bin_auto(_time)
```

Plot p50/p95/p99 together, never the average. The mean hides exactly the
tail you care about.

**Axiom renders one chart per aggregation, so this query comes back as three
separate charts.** That is a display default, not a problem with the query —
combine them with **View options → Merge charts**. No query change needed.

To read the numbers without hovering, pair the chart with a **Table** element
using the same filters and no time bin, which collapses the window to a single
row:

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    requests = count(),
    p50 = percentile(duration_ms, 50),
    p95 = percentile(duration_ms, 95),
    p99 = percentile(duration_ms, 99),
    max_ms = max(duration_ms)
```

Dropping `by bin_auto(_time)` is what turns the time series into one summary
row for the dashboard's selected range. Keep the bin if you would rather have a
row per bucket — useful for reading exact values off a spike, but it is a long
table rather than a glanceable one.

For a per-endpoint breakdown in that table slot, use the next panel instead.

**Panel: Latency by endpoint** (table, sorted worst-first)

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    requests = count(),
    p50 = percentile(duration_ms, 50),
    p95 = percentile(duration_ms, 95),
    p99 = percentile(duration_ms, 99),
    max_ms = max(duration_ms)
  by method, route
| where requests > 20
| sort by p95 desc
```

The `requests > 20` guard is the difference between a useful panel and one
permanently topped by an endpoint called twice last Tuesday. Percentiles over
tiny samples are noise. Tune the threshold to your traffic.

**Panel: Where the server actually spends its time** (bar)

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    total_seconds = round(sum(duration_ms) / 1000.0, 1),
    requests = count(),
    p95 = percentile(duration_ms, 95)
  by route
| sort by total_seconds desc
| limit 20
```

Ranks by aggregate cost rather than per-call cost. A 4-second endpoint called
ten times a day matters less than a 250 ms endpoint called a million times —
this panel tells you which optimization is worth doing, and it routinely
disagrees with the p95 table above.

**Panel: Latency SLO — share of requests under target** (timechart)

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    total = count(),
    fast = countif(duration_ms <= 500),
    ok = countif(duration_ms > 500 and duration_ms <= 2000)
  by bin_auto(_time)
| extend
    pct_under_500ms = round(100.0 * fast / total, 2),
    pct_500ms_to_2s = round(100.0 * ok / total, 2),
    pct_over_2s = round(100.0 * (total - fast - ok) / total, 2)
| project _time, pct_under_500ms, pct_500ms_to_2s, pct_over_2s
```

A single number you can alert on and put in front of non-engineers, which raw
percentiles are not.

**Panel: Slowest individual requests** (table — for chasing one bad request)

```
['drserver']
| where msg == "request" and environment == "production"
| where duration_ms > 3000
| project _time, method, route, status, duration_ms, user_id, request_id, app_version, platform
| sort by duration_ms desc
| limit 100
```

Probes are deliberately **not** excluded here: this is a listing, so their
volume can't skew it, and a `/healthz` above 3 s means the database is
struggling — exactly what you want surfaced.

`request_id` is the join key — every other log line from that same request
carries it, so paste one into the search bar to get the full story:

```
['drserver']
| where request_id == "<paste-request-id>"
| sort by _time asc
```

---

### 3.3 Errors

**Panel: Error rate over time** (timechart — 5xx and 4xx as separate series)

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    total = count(),
    server_errors = countif(status >= 500),
    client_errors = countif(status >= 400 and status < 500)
  by bin_auto(_time)
| extend
    error_rate_5xx = round(100.0 * server_errors / total, 3),
    error_rate_4xx = round(100.0 * client_errors / total, 3)
| project _time, error_rate_5xx, error_rate_4xx, total
```

Keep 4xx and 5xx apart. 5xx is your bug; 4xx is usually the client's — except
when a deploy breaks a contract and 400s spike, which is why it is on the
chart rather than hidden.

**Panel: Error rate by endpoint** (table — the alerting panel)

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    requests = count(),
    errors_5xx = countif(status >= 500),
    errors_4xx = countif(status >= 400 and status < 500),
    unauthorized = countif(status == 401 or status == 403),
    rate_limited = countif(status == 429),
    not_found = countif(status == 404)
  by method, route
| extend error_rate_5xx = round(100.0 * errors_5xx / requests, 2)
| where requests > 20
| sort by errors_5xx desc, error_rate_5xx desc
```

Sorted by absolute 5xx count first so a high-traffic endpoint at 2% outranks a
low-traffic one at 100%. Flip the sort keys if you would rather hunt the
proportionally-worst endpoint.

**Panel: Status code distribution** (stacked area)

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize requests = count() by status_class = bin(status, 100), bin_auto(_time)
```

`bin(status, 100)` buckets into 200/300/400/500. For the exact codes:

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize requests = count() by status, bin_auto(_time)
```

**Panel: Every 5xx, in detail** (table)

```
['drserver']
| where msg == "request" and environment == "production"
| where status >= 500
| project _time, method, route, status, duration_ms, user_id, request_id, gin_errors, app_version, platform
| sort by _time desc
| limit 200
```

Probes stay in for the same reason as the slow-request list — a 503 from
`/healthz` belongs in your 5xx detail view even though it must stay out of the
5xx *rate*.

`gin_errors` is present only when a handler attached an error to the gin
context, so it is often empty — it is a bonus, not the diagnosis. For the
diagnosis, take the `request_id` and pull the whole request, or check the
`failure` stream below.

**Panel: Auth failures by endpoint** (table)

```
['drserver']
| where msg == "request" and environment == "production"
| where status == 401 or status == 403
| summarize requests = count(), ips = dcount(client_ip) by route, status
| sort by requests desc
```

A high `ips` count on a login route is credential stuffing. A single IP with
thousands of 401s is a broken client or an expired token loop.

**401 lines carry no `user_id`, and their `request_id` is empty.** Both are
consequences of where auth sits in the middleware chain: `AuthMiddleware`
aborts the request, so the middleware that assigns `requestID` and resolves
`user_id` — registered after it on the `/api/v1` group — never runs. The
matching `supabase auth: token rejected` line comes from a package-level
logger and carries the literal `request_id: "auth"`, not a correlation id.

So the "join by `request_id`" advice in §3.2 **does not work for auth
failures**. Group by `client_ip` instead — it is the only identifier a 401
carries. You cannot tell from these logs alone which user is affected.

**Panel: Clients stuck in a token-retry loop**

```
['drserver']
| where msg == "request" and environment == "production"
| where status == 401
| summarize
    requests = count(),
    routes = dcount(route),
    first = min(_time),
    last = max(_time)
  by client_ip
| where requests > 50
| extend minutes = round(datetime_diff('second', last, first) / 60.0, 1)
| extend per_minute = round(requests / minutes, 1)
| sort by requests desc
```

**For incident forensics, pin the window in the query.** These panels inherit
the dashboard's range picker, which is right for a live dashboard and wrong
when you are looking at something that happened yesterday:

```
| where _time >= datetime(2026-09-22T19:00:00Z) and _time <= datetime(2026-09-22T21:00:00Z)
```

APL has no `between` operator — use `>=` and `<=`. And note that Axiom
*renders* timestamps in your local timezone while the `time` field in the raw
event is UTC with a `Z` suffix. A log row displayed as `16:07` in US Eastern is
`20:07Z`. Building an absolute window from the displayed time rather than the
`Z` time puts you four hours off and makes an incident look like it never
happened.

A client in a healthy state produces a handful of 401s — token expires, app
refreshes, done. A client looping produces hundreds against many distinct
routes over a sustained window. `routes` is the tell: an expired token during
an app-wide refetch 401s every mounted screen at once, so a loop shows a high
route count and a steady per-minute rate rather than a spike and a recovery.

#### Finding the user behind a 401 storm

**The `supabase auth: token rejected` line now carries `user_id`** for expired
tokens, which makes this a single query:

```
['drserver']
| where msg == "supabase auth: token rejected"
| where environment == "production"
| where isnotempty(user_id)
| summarize rejections = count(), first = min(_time), last = max(_time)
  by user_id
| sort by rejections desc
```

The subject is attached only when the token is *expired*, never when it is
merely unparseable — claims from a token that failed signature verification are
attacker-controlled, and logging one would let anyone write an arbitrary
`user_id` into your logs. Signature failures and malformed tokens therefore
still have no user attached, by design.

The access line for the same request still has no `user_id`; this field is on
the auth line, so filter on `msg == "supabase auth: token rejected"` rather
than on `status == 401`.

**This only applies to logs written after that change deployed.** For anything
older, the two fallbacks below are what you have.

#### Fallbacks for logs predating the `user_id` field

401 lines carry no `user_id`, but the device almost always made *successful*
authenticated requests from the same IP before its token expired — and those
lines do carry one:

```
['drserver']
| where msg == "request" and environment == "production"
| where client_ip == "<paste the ip>"
| where isnotempty(user_id)
| summarize requests = count(), first = min(_time), last = max(_time)
  by user_id
| sort by last desc
```

The `user_id` whose `last` sits just before the 401s began is your affected
user. Treat it as strong evidence rather than proof: mobile clients move
between carrier IPs, and CGNAT can put several users behind one address, so
check that `last` actually abuts the start of the storm before acting on it.

**If every row comes back with an empty `user_id`**, that device has made no
successful authenticated request inside retention — it has been looping since
before the window. Fall back to the analytics ingest, which survives an expired
token: `/api/v1/events` uses `OptionalAuthMiddleware`, so it returns 200 rather
than 401 and the app keeps flushing telemetry throughout the outage. Those
access lines also carry a real `request_id`, because the middleware that
assigns one runs on that route.

```
['drserver']
| where msg == "request" and environment == "production"
| where client_ip == "<paste the ip>"
| where route == "/api/v1/events"
| project _time, status, request_id
| sort by _time desc
| limit 20
```

Take a `request_id` from that and read the payload the device sent:

```
['drserver']
| where msg == "product_event"
| where event.request_id == "<paste the request_id>"
| project _time, event.event_name, event.user_id, event.anonymous_id, event.session_id
```

`event.user_id` is client-reported, so it is present even though the server
could not authenticate the token. If it is empty too, `event.anonymous_id` and
`event.session_id` will identify the device, and those same ids appear on
earlier events from when the user *was* authenticated — one more hop, same
answer.

**Panel: Why tokens are being rejected**

```
['drserver']
| where msg == "supabase auth: token rejected"
| where environment == "production"
| summarize count() by detail, bin_auto(_time)
```

`detail` separates routine expiry (`token has invalid claims: token is
expired`) from signature failures and malformed tokens, which mean something
very different.

**Panel: Rate limiting** (timechart)

```
['drserver']
| where msg == "request" and environment == "production"
| where status == 429
| summarize throttled = count() by route, bin_auto(_time)
```

**Panel: Panics** (table — should be permanently empty)

```
['drserver']
| where msg == "panic recovered"
| project _time, panic, request_id, user_id, stack
| sort by _time desc
```

Recovery turns panics into 500s, so they also appear in the 5xx panel — but
only here do you get the stack. Alert on `count() > 0`. Sentry has these too,
with better grouping; this panel is for correlating with the access log.

**Panel: Failures by subsystem** (stacked area)

```
['drserver']
| where msg == "failure" and environment == "production"
| summarize failures = count() by failure_category, error_code, bin_auto(_time)
```

This is the *cause* stream, where the 5xx panel is the *symptom* stream. A 500
on `/api/v1/agent/generate-async` tells you the request failed; a
`failure_category == "ai_provider"` with `error_code == "timeout"` tells you
why. Filter on `msg == "failure"`, never on `level == "ERROR"` — the level
sweeps in every stray error line in the codebase and is not alertable.

---

### 3.4 Clients, releases, and users

**Panel: Error rate by release — the deploy-regression panel**

```
['drserver']
| where msg == "request" and environment == "production"
| where route !in ("/health", "/healthz")
| summarize
    requests = count(),
    errors = countif(status >= 500),
    p95 = percentile(duration_ms, 95)
  by release
| extend error_rate = round(100.0 * errors / requests, 3)
| sort by error_rate desc
```

`release` is the Fly machine version and changes every deploy, so this answers
"did this start with the last deploy?" — the first question in every incident.
Add `, bin_auto(_time)` to the `by` clause to see the changeover as a chart.

**Panel: Error rate and latency by app version** (table)

```
['drserver']
| where msg == "request" and environment == "production"
| where isnotempty(app_version)
| summarize
    requests = count(),
    users = dcount(user_id),
    errors = countif(status >= 500),
    p95 = percentile(duration_ms, 95)
  by platform, app_version, build_number
| extend error_rate = round(100.0 * errors / requests, 2)
| sort by app_version desc
```

No probe filter needed here. `platform` / `app_version` / `build_number` are
emitted only for requests carrying mobile client headers — webhooks, probes and curl have no version and
are deliberately omitted rather than piled into a blank bucket. Hence the
`isnotempty` guard; without it you get one meaningless giant row.

**Panel: Heaviest users** (table — abuse and runaway-client detection)

```
['drserver']
| where msg == "request" and environment == "production"
| where isnotempty(user_id)
| summarize requests = count(), errors = countif(status >= 500), routes = dcount(route)
  by user_id
| sort by requests desc
| limit 50
```

`isnotempty(user_id)` does the probe filtering for free — health checks are
unauthenticated, so they carry no `user_id` and drop out.

**Panel: Unmatched routes — scanners and broken clients** (table)

```
['drserver']
| where msg == "request" and environment == "production"
| where status == 404
| summarize hits = count(), ips = dcount(client_ip) by path
| sort by hits desc
| limit 50
```

This is the one place to group by `path` instead of `route`: unmatched
requests have no route template, so the middleware falls back to the raw path.
Expect `/wp-admin` and friends. A real app path showing up here is a client
calling an endpoint you removed.

---

### 3.5 Active users

**Panel: Active users** (single-value metric — follows the dashboard's range)

```
['drserver']
| where msg == "request" and environment == "production"
| where isnotempty(user_id)
| summarize active_users = dcount(user_id)
```

With no `bin(_time, ...)` and no explicit time predicate, the query inherits
whatever range is selected in Axiom — the dashboard's range picker, or the
range on the panel if you have overridden it there. Set the dashboard to 24h
and this reads daily actives; set it to 30d and the same tile reads monthly
actives, with no edit.

That is usually what you want for a headline tile, with one thing to watch:
**the label no longer means a fixed thing.** A tile captioned "DAU" that
quietly becomes a 30-day count when someone widens the range is a number
people will misread in a meeting. Either caption it neutrally — "Active users
(selected range)" — or pin the window, below.

`isnotempty(user_id)` does the probe filtering for free, so no
`route !in (...)` line is needed: health checks are unauthenticated.

**Variant: pin the window regardless of the dashboard range**

```
['drserver']
| where msg == "request" and environment == "production"
| where isnotempty(user_id)
| where _time > ago(1d)
| summarize active_users = dcount(user_id)
```

`ago(1d)` fixes this to a rolling 24 hours whatever the range picker says.
Use it when the tile sits next to others that must stay comparable, or when
the caption makes a specific promise. `ago(7d)` and `ago(30d)` alongside it
give you the DAU / WAU / MAU trio on one row — and those three only mean
anything next to each other if each is pinned.

**Panel: Active users over time** (timechart)

```
['drserver']
| where msg == "request" and environment == "production"
| where isnotempty(user_id)
| summarize active_users = dcount(user_id) by bin_auto(_time)
```

`bin_auto` picks a bucket from the selected range, so this one also follows
the picker. Swap in `bin(_time, 1d)` if you specifically want calendar-day
buckets — note those are UTC, so for US-timezone users the boundary lands
mid-evening and a single day's bar is a slightly odd slice of a person's day.
Consistent enough for a trend line; don't read one bar as "that day."

**Always `dcount(user_id)`, never `count()`** — one user making 300 requests
is one active user. A result of 14 means 14 distinct `user_id` values, not 14
requests.

#### `dcount` is an estimate, and how to get an exact number

Axiom documents `dcount` as a probabilistic aggregation — fast and "highly
accurate", but explicitly an approximation, with no error rate published and
no exact equivalent (`count_distinct` does not exist in APL). In practice
small values are essentially always exact; the approximation exists for
high-cardinality fields, not for a set of a few dozen users. Do not treat a
`dcount` tile as an audited figure, though — it is a metric, not a ledger.

When you need the exact count, use the `distinct` operator instead of the
aggregation:

```
['drserver']
| where msg == "request" and environment == "production"
| where isnotempty(user_id)
| distinct user_id
| summarize exact_users = count()
```

`distinct` materializes the unique set and `count()` counts the rows, so
there is no estimation involved. This is fine at small scale and gets
expensive as the set grows — which is exactly why `dcount` is the default.

Dropping the `summarize` line gives you the actual list of user IDs, which is
often more useful than the number when the number looks wrong.

#### What the number counts

Distinct **accounts** that made an authenticated request in the range — which
is not quite the same as people:

- One person with two accounts counts twice.
- Someone who opened the app but made no authenticated call does not count at
  all; `user_id` is set by the auth middleware on `/api/v1` routes, so
  unauthenticated traffic has no user to count.
- The mobile app refreshing in the background does count (see below).

#### What "active" means here, and why you may want the other definition

The queries above count anyone whose authenticated HTTP request reached the
server. That includes the mobile app refreshing in the background — a phone in
a pocket can register as active without its owner opening the app. Fine as a
reach number, as long as you know that is what it is.

For activity that implies a human decided to do something, count product
events instead:

```
['drserver']
| where msg == "product_event"
| where event.event_name == "work_item_completed"
| summarize active_users = dcount(event.user_id)
```

Also range-following. Put both on the dashboard: the first is reach, the
second is engagement, and the gap between them moving is a signal neither
shows alone. §5 has the weekly version.

#### Total registered users is not a question Axiom can answer

There is no correct query for it, and the plausible-looking ones are wrong in
a way that does not announce itself.

Axiom stores **events**, so the most it can tell you is "distinct users who
did something within the query window." Widening the range to "all time" makes
that look like a total-users count, but it is still just active users over a
longer window — it silently omits every dormant user, it cannot see further
back than retention (30 days on the operational stream), and the number drifts
downward on its own as old data ages out. A tile like that is worse than
missing, because it looks authoritative and is quietly wrong.

Total registered users is a **database** fact. Three ways to get it, worst to
best:

1. **Count `signup_completed` events.** Accurate only from the day
   instrumentation shipped (`ab5680f`), so it misses every user who signed up
   before that, and is still bounded by retention. Fine as a *new signups in
   range* metric — which is a genuinely useful tile — but it is not a total.

   ```
   ['drserver']
   | where msg == "product_event"
   | where event.event_name == "signup_completed"
   | summarize new_signups = dcount(event.user_id)
   ```

2. **Query Postgres directly** and put the number wherever you keep such
   things. Correct, but it does not live on this dashboard.

3. **Emit it as a periodic gauge — recommended.** A scheduled job reads
   `count(*)` from the users table and logs it as an event; Axiom then charts
   it like anything else. This is the standard way to get a database fact into
   an event store, and the infrastructure is already here: `main.go` registers
   cron jobs via `mustRegister`, and the heartbeat at `* * * * *` is the
   existing example of a fixed-cadence emitter.

   Once such a job exists, the panels are:

   ```
   ['drserver']
   | where msg == "user_totals" and environment == "production"
   | summarize total_users = max(total_users) by bin(_time, 1d)
   ```

   `max()` rather than `avg()` or `count()` — a gauge emitted N times per
   window should be read as its latest value, not summed or averaged across
   emissions.

**This job does not exist yet** — the query above will read empty until it is
written. See §9.

---

### 3.6 WebSockets, background work, and liveness

**Panel: WebSocket sessions** (timechart + duration)

```
['drserver']
| where msg == "ws_connection" and environment == "production"
| summarize
    sessions = count(),
    p50_session_s = round(percentile(session_ms, 50) / 1000.0, 1),
    p95_session_s = round(percentile(session_ms, 95) / 1000.0, 1)
  by route, bin_auto(_time)
```

Separate from `request` on purpose — a 20-minute WebSocket session logged as a
request would put your p99 in the minutes.

**Panel: Roadmap generation health**

```
['drserver']
| where msg == "roadmap generation finished" and environment == "production"
| summarize
    attempts = count(),
    succeeded = countif(outcome == "completed"),
    p50_s = round(percentile(generation_ms, 50) / 1000.0, 1),
    p95_s = round(percentile(generation_ms, 95) / 1000.0, 1)
  by model, bin_auto(_time)
| extend success_rate = round(100.0 * succeeded / attempts, 2)
```

The most expensive and most failure-prone path in the product, and invisible to
HTTP metrics because it runs async after the request returns 202.

**Panel: Uptime / probe health** (keep health checks IN for this one)

```
['drserver']
| where msg == "request"
| where route in ("/health", "/healthz")
| summarize
    probes = count(),
    failures = countif(status >= 500),
    p95_ms = percentile(duration_ms, 95)
  by route, bin_auto(_time)
```

`/healthz` pings the database and 503s when it can't, so failures here mean
the DB, not the app.

**Panel: Telemetry heartbeat — the monitor that catches total failure**

```
['drserver']
| where msg == "telemetry_heartbeat"
| summarize beats = count() by bin(_time, 5m)
```

Emitted every minute by the scheduler. **Alert when this drops below ~3 in 5
minutes.** Every other panel on this dashboard goes quiet both when nothing is
happening and when logging itself is broken; this is the only one whose
silence is unambiguous. Build it, alert on it, and never delete it.

---

### 3.7 Suggested alerts

| Alert | Query shape | Threshold |
|---|---|---|
| 5xx spike | 3.3 error rate over time | `error_rate_5xx > 1` for 5 min |
| Endpoint down | 3.3 error rate by endpoint | any route `> 10%` with `requests > 20` |
| Latency regression | 3.2 percentiles | `p95 > 2000` for 10 min |
| Panic | 3.3 panics | `count() > 0` |
| Subsystem failure | 3.3 failures by subsystem | `count() > 20` in 5 min, by category |
| Telemetry dead | 3.6 heartbeat | `count() < 3` in 5 min |
| DB unhealthy | 3.6 probes | `/healthz` failures `> 0` |

Crash-free users and stack-trace grouping are **already in Sentry** for both
server and mobile. Link that dashboard rather than rebuilding it here.

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
| Total registered users (gauge) | A scheduled job emitting `count(*)` from the users table — see §3.5 |
| Goal discovery funnel | Client events reserved in the catalog; screens not yet instrumented |
| Anchor invite expiry | No expiry job exists |

Everything else on the original list is live.
