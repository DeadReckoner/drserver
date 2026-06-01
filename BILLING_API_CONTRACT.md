# Backend API Contract: Free / Paid Plan Gating

**Audience:** mobile (RN) frontend dev building the paywall + lock UI.
**Backend status:** Step 1 (entitlements + read endpoint) shipped (PR #43). Step 2 (handler gating + admin comp) in review (PR #45). Step 6 (RevenueCat webhook) and Step 8 (paid-only features) pending.

This doc is the contract between backend and mobile. It only covers Free/Paid behavior — auth, error shapes outside of 402, and unrelated endpoints follow existing conventions.

---

## 1. The feature flag

All plan enforcement is gated by the server-side env var `PRO_GATING_ENABLED`.

- **Off (default)**: every endpoint behaves exactly as before. No 402s will ever be returned. Useful while building UI against staging.
- **On**: free users start hitting 402s on the gated endpoints below. This is what production will run with.

The mobile client does NOT see this flag directly — your code should always be ready to handle 402s, and just won't see them when the flag is off.

---

## 2. Source of truth: `GET /api/v1/billing/status`

The authoritative plan + limits + live usage for the current user. Call on app foreground and after a purchase to reconcile with whatever RevenueCat's client SDK said.

**Auth:** standard Bearer token (Supabase JWT).
**Method/path:** `GET /api/v1/billing/status`
**Body:** none

### Response (free user)

```json
{
  "plan": "free",
  "source": "default",
  "limits": {
    "active_roadmaps": 1,
    "active_habits": 3,
    "anchors_per_roadmap": 1,
    "ai_generations_per_month": 3
  },
  "usage": {
    "active_roadmaps": 0,
    "active_habits": 0,
    "ai_generations_this_period": 0
  }
}
```

### Response (paid user)

```json
{
  "plan": "paid",
  "source": "apple",
  "status": "trialing",
  "trial_ends_at": "2026-06-01T18:24:36Z",
  "renews_at": "2026-06-25T18:24:36Z",
  "product_id": "dr_pro_monthly",
  "limits": {
    "active_roadmaps": -1,
    "active_habits": -1,
    "anchors_per_roadmap": -1,
    "ai_generations_per_month": -1
  },
  "usage": {
    "active_roadmaps": 2,
    "active_habits": 5,
    "ai_generations_this_period": 12
  }
}
```

### Field reference

| Field | Type | Notes |
|---|---|---|
| `plan` | `"free"` \| `"paid"` | Source of truth for paid features. **Use this, not RC's client SDK, for gating UI.** |
| `source` | `"default"` \| `"apple"` \| `"google"` \| `"stripe"` \| `"comp"` | How they got paid. `"comp"` = admin granted. Useful for "manage subscription" link logic. |
| `status` | `"active"` \| `"trialing"` \| `"in_grace"` \| `"cancelled"` \| `"expired"` \| `null` | Subscription lifecycle state. Null on default/comp users. |
| `trial_ends_at` | RFC3339 string or omitted | Show "trial ends in X days" copy when present. |
| `renews_at` | RFC3339 string or omitted | The end of the current paid period. Mobile may want to render "your subscription renews on…" |
| `product_id` | `"dr_pro_monthly"` \| `"dr_pro_annual"` \| omitted | Useful for displaying current plan tier. |
| `limits` | object | `-1` means unlimited. Render unlimited features as "unlimited" not "−1". |
| `usage` | object | Current period counts. AI counter resets on the 1st UTC of each month. |

---

## 3. The 402 contract

When `PRO_GATING_ENABLED=true` and a free user attempts a gated action over the limit (or any action requiring a paid-only feature), the endpoint returns **HTTP 402 Payment Required** with this body:

```json
{
  "code": "plan_limit",
  "feature": "active_roadmaps",
  "limit": 1,
  "current": 1,
  "message": "Free users can have 1 active roadmap. Upgrade to create unlimited roadmaps."
}
```

### Field reference

| Field | Type | Notes |
|---|---|---|
| `code` | `"plan_limit"` | Stable. Reserved for future codes like `"trial_required"`, `"fair_use"`. Right now always `plan_limit`. |
| `feature` | string enum (see below) | Stable. Mobile should key off this to render the right paywall variant. |
| `limit` | int | The cap that was hit. `0` for paid-only boolean features. |
| `current` | int | The user's current count. Lets you render "you have 3/3 habits" without an extra round trip. `0` for boolean features. |
| `message` | string | Default English copy. Treat as a fallback — preferred is to localize off `feature`. |

### Suggested UX mapping

| `feature` value | Suggested paywall headline | When the client sees it |
|---|---|---|
| `active_roadmaps` | "Unlimited roadmaps with Pro" | Free user tries to create a 2nd roadmap |
| `active_habits` | "Unlimited habits with Pro" | Free user tries to create a 4th habit |
| `roadmap_anchors` | "More anchors with Pro" | Free user tries to add an anchor when they already have one |
| `habit_anchors` | "Habit anchors are Pro" | Free user tries to add an anchor to a habit (always blocked on free) |
| `ai_generations_per_month` | "Unlimited AI roadmaps with Pro" | Free user has used 3 generations this month |
| `archive` | "Archive is Pro" | Free user hits archive on a roadmap or habit |
| `custom_reminders` *(Step 8)* | "Custom reminders are Pro" | TBD |
| `weekly_review` *(Step 8)* | "AI-powered weekly review is Pro" | TBD |
| `messaging_reactions` *(Step 8)* | "Reactions & replies are Pro" | TBD |
| `anchor_settings` *(Step 8)* | "Per-anchor settings are Pro" | TBD |
| `advanced_stats` *(Step 8)* | "Advanced stats are Pro" | TBD |
| `historical_performance` *(Step 8)* | "Historical performance is Pro" | TBD |

The starred ones don't have endpoints yet — they'll come online as Step 8 features land. Codes are stable so you can wire UI ahead of backend.

---

## 4. Which endpoints can return 402

These all gate when `PRO_GATING_ENABLED=true`. Endpoints not listed do NOT return 402.

| Endpoint | Trigger | Feature value |
|---|---|---|
| `POST /api/v1/roadmaps` | Free user already has 1 active (non-archived) roadmap | `active_roadmaps` |
| `POST /api/v1/roadmaps/:id/anchor-request` | Free user already has 1 active anchored roadmap | `roadmap_anchors` |
| `POST /api/v1/roadmaps/:id/archive` | Free user — archive is paid-only | `archive` |
| `POST /api/v1/habits` | Free user already has 3 active habits | `active_habits` |
| `POST /api/v1/habits/:id/anchor-request` | Free user — habit anchors are paid-only | `habit_anchors` |
| `POST /api/v1/habits/:id/archive` | Free user — archive is paid-only | `archive` |
| `POST /api/v1/agent/generate` | Free user already used 3 AI gens this month | `ai_generations_per_month` |
| `POST /api/v1/agent/generate-async` | same | same |
| `POST /api/v1/agent/generateMilestone/:id` | same | same |
| `POST /api/v1/agent/generate-next-milestone-async/:id` | same | same |

**Counter behavior**: the AI counter increments only on **successful** generation. Failed generations don't burn a credit. For async generations, the counter bumps when the background job finishes successfully — which means the count returned by `/billing/status` may briefly lag a freshly-submitted job.

---

## 5. Recommended client integration

1. **On auth / app foreground:** call `GET /billing/status`, stash plan + limits + usage in your store (Redux slice).
2. **Render lock UI** off `plan === 'free'` and the per-feature limits.
3. **Wrap your API client** with a 402 interceptor: on 402, parse the body, dispatch a paywall modal action with `feature` as the key.
4. **After a successful RevenueCat purchase**, call `GET /billing/status` to refresh. Don't trust RC's client SDK alone — webhook lag means the backend may briefly still say `free` immediately after purchase; treat your local state as optimistic and reconcile on the next call.
5. **On 402 dismissal / paywall close**, don't re-attempt the original request automatically — the user explicitly opted out.

---

## 6. Admin endpoint (NOT for mobile — for support tooling only)

Just listing for completeness. Don't call from mobile.

- `POST /admin/users/:userid/plan`
- Header: `X-Admin-Key: <ADMIN_API_KEY>`
- Body: `{"plan": "paid", "source": "comp", "expires_at": "..."}`
- Used to comp users without going through payments.

---

## 7. Coming soon (heads-up, not yet live)

### Step 6 — RevenueCat webhook (no mobile-facing change)
- Server-side handler that flips `plan`, `status`, `current_period_end`, etc. in response to RC events.
- **Mobile impact:** none direct. After this lands, `GET /billing/status` will reflect real-world Apple/Google subscription state instead of only admin comps.
- **Reconciliation:** still call `GET /billing/status` on foreground and post-purchase. Backend will sometimes be 1–10 seconds behind RC's client SDK due to webhook delivery.

### Step 8 — Paid-only feature endpoints
Each of these adds a new endpoint that returns 402 for free users. The `feature` codes are listed in §3 with a TBD note. As they land I'll add details here. Expect:
- `PUT /api/v1/roadmaps/:id/reminders` (custom reminders)
- `PATCH /api/v1/anchor-requests/:id/settings` (anchor-specific settings)
- `GET /api/v1/stats/historical` (historical performance)
- `POST /api/v1/habits/:id/archive` (already exists, archive gating already wired)
- `GET /api/v1/weekly-review` (AI-powered weekly review)
- Messaging reactions/replies endpoints (TBD)

---

## 8. Testing tips

### Without a payment flow
The admin endpoint lets you flip your own test user to paid without going through Apple/Google:
```bash
curl -X POST 'http://127.0.0.1:8080/admin/users/<your-user-id>/plan' \
  -H "X-Admin-Key: $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"plan":"paid","source":"comp"}'
```

To flip back to free:
```bash
curl -X POST 'http://127.0.0.1:8080/admin/users/<your-user-id>/plan' \
  -H "X-Admin-Key: $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"plan":"free","source":"comp"}'
```

Then re-call `GET /billing/status` to see the new state.

### Forcing the gating on locally
Set `PRO_GATING_ENABLED=true` in your backend `.env` and restart the Go server. Otherwise free users behave like paid users (no 402s ever).

### Smoke flow for paywall UI
1. Flag on, user free
2. Create 1 roadmap → 201 OK
3. Create another roadmap → expect 402 with `{"feature":"active_roadmaps","limit":1,"current":1}`
4. Flip user to paid via admin
5. Create another roadmap → 201 OK
6. Flip back to free, create another roadmap → 402 again

---

## 9. Questions / changes

Ping me (backend) before changing the 402 contract — mobile keys off `feature` and `code` to render the right paywall, so renames are breaking.

If you need a new `feature` value (e.g. for a new paid-only gate I haven't added), open an issue or just ask — I'll add it to the FeatureKey enum and to the table in §3.
