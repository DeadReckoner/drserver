# Mobile RevenueCat Integration Guide

**Audience:** mobile (Expo / React Native) frontend dev.
**Companion doc:** `BILLING_API_CONTRACT.md` (the backend's HTTP contract).
**Status of backend:** entitlements + /billing/status (PR #43) shipped; handler gating (PR #45) shipped; RC webhook (PR #55 + #56 fix) shipped.

This is everything you need on the **Expo / mobile** side to get the Free → Paid purchase loop working end-to-end against the backend that's already live.

---

## TL;DR

```ts
// the ONE line that ties RC purchases to backend users
await Purchases.logIn(supabaseUserId);
```

Without that call, every purchase ends up under an anonymous RC ID and the backend can't link it to the user. **Get this right first.**

---

## 1. Prerequisites

Before writing any code:

- [ ] Expo SDK 50+ (the project is already on 53 per `CLAUDE.md`)
- [ ] EAS Build set up — IAP doesn't work in Expo Go, you need a dev client
- [ ] Real iOS device or simulator with iOS 16+ for sandbox testing
- [ ] Apple Developer account + App Store Connect access (the user owns this)
- [ ] RevenueCat project created (the user owns this)
- [ ] Sandbox Apple ID created in App Store Connect → Users and Access → Sandbox
- [ ] RC public API keys from the user (one for iOS, one for Android when applicable)

---

## 2. Install the SDK

```bash
npx expo install react-native-purchases
```

`react-native-purchases-ui` is optional — only needed if you want RC's pre-built paywall components. Since we're building a custom paywall, skip it.

## 3. Wire the Expo config plugin

In `app.json` (or `app.config.js`):

```json
{
  "expo": {
    "plugins": [
      [
        "react-native-purchases",
        {
          "android": { "billingClientVersion": "8.0.0" }
        }
      ]
    ]
  }
}
```

The plugin auto-adds:
- iOS: `In-App Purchase` capability + StoreKit entitlement
- Android: `com.android.vending.BILLING` permission

## 4. Rebuild the dev client

**Critical** — `react-native-purchases` ships native code, so Expo Go can't load it.

```bash
npx expo prebuild --clean
eas build --profile development --platform ios
```

Then install the resulting build on your test device. Re-run this whenever the config plugin section changes.

For day-to-day JS iteration after the initial build, the dev client is fine — you only need to rebuild when native config changes.

---

## 5. SDK initialization

In your root layout (`src/app/_layout.tsx` or wherever app boot happens), initialize once on app start:

```ts
import { Platform } from 'react-native';
import Purchases, { LOG_LEVEL } from 'react-native-purchases';

// Set BEFORE configure()
if (__DEV__) {
  Purchases.setLogLevel(LOG_LEVEL.DEBUG);
}

Purchases.configure({
  apiKey: Platform.select({
    ios: process.env.EXPO_PUBLIC_REVENUECAT_IOS_KEY,
    android: process.env.EXPO_PUBLIC_REVENUECAT_ANDROID_KEY,
  })!,
});
```

The API keys are **public** (safe to ship in the app binary) — different from the webhook secret on the backend.

## 6. Identify the user — THE most important integration point

Every time Supabase auth completes (sign-in or sign-up), immediately call:

```ts
import Purchases from 'react-native-purchases';

await Purchases.logIn(supabaseUserId);
```

`supabaseUserId` must be the UUID returned by Supabase Auth — the exact same value the backend stores as `users.user_id`. The webhook handler resolves the `app_user_id` field on incoming events to find the matching DR user.

On sign-out:

```ts
await Purchases.logOut();
```

### Why this matters

The backend currently treats any event with an unknown / non-existent `app_user_id` as `noop_unmapped` and records it with `user_id NULL`. That means:

- Purchase succeeds client-side (RC SDK reports entitlement granted)
- Webhook fires, backend can't find the user, plan doesn't flip
- `GET /billing/status` keeps returning `"plan": "free"`
- Lock UI stays on, paid features stay locked

**Symptom of getting this wrong:** the app says "you're Pro!" but no paid features unlock. Always test with `logIn` wired up before testing the rest of the flow.

### Recommended hook

```ts
// src/hooks/useRevenueCatIdentity.ts
import { useEffect } from 'react';
import Purchases from 'react-native-purchases';
import { useAppSelector } from '@/store/hooks/store.hooks';

export function useRevenueCatIdentity() {
  const userId = useAppSelector((s) => s.auth.user?.id);

  useEffect(() => {
    if (userId) {
      Purchases.logIn(userId).catch((err) => {
        console.error('[RC] logIn failed', err);
      });
    } else {
      Purchases.logOut().catch(() => {});
    }
  }, [userId]);
}
```

Call `useRevenueCatIdentity()` once in the root layout, alongside the existing auth listener.

---

## 7. Purchase flow

```ts
import Purchases, { PurchasesOffering } from 'react-native-purchases';

// Fetch offerings
const offerings = await Purchases.getOfferings();
const current = offerings.current;
if (!current) {
  // No offering configured in RC dashboard, or network failure
  return;
}

// Show paywall with current.availablePackages
// User taps "Subscribe (monthly)"
const monthlyPackage = current.availablePackages.find(
  (p) => p.product.identifier === 'dr_pro_monthly'
);

try {
  const { customerInfo } = await Purchases.purchasePackage(monthlyPackage);
  if (customerInfo.entitlements.active['pro']) {
    // Client-side: RC says pro is active
    // Server-side: webhook will arrive within seconds and flip backend state
    // Optimistically close paywall, then re-fetch /billing/status on next foreground
  }
} catch (err: any) {
  if (err.userCancelled) {
    // User backed out, no-op
  } else {
    // Show error toast
  }
}
```

## 8. Restore purchases (Apple REQUIRES this button)

```ts
const customerInfo = await Purchases.restorePurchases();
if (customerInfo.entitlements.active['pro']) {
  // restored
}
```

Put a "Restore Purchases" button somewhere visible on the paywall AND in settings. Apple will reject the app without it.

## 9. Manage subscription deep link

```ts
import { Linking } from 'react-native';

// iOS deep-links straight to the subscription management screen
Linking.openURL('https://apps.apple.com/account/subscriptions');
```

Add a "Manage Subscription" link in the settings screen for paid users.

---

## 10. Reconciliation with the backend

After any state change (purchase, restore, app foreground), re-fetch the backend's authoritative plan state:

```ts
const res = await fetch(`${API_URL}/api/v1/billing/status`, {
  headers: { Authorization: `Bearer ${token}` },
});
const { plan, status, limits, usage } = await res.json();
```

**Use this as the source of truth for gating UI**, not RC's client-side `customerInfo`. The RC SDK shows the immediate post-purchase state, but the backend may briefly lag (webhook delivery is usually < 5 seconds). On the next foreground or 5s after a purchase, the backend will have caught up.

Reasonable strategy:

1. **On purchase success**: optimistically update local state to paid (RC SDK already does this).
2. **5 seconds later**: re-fetch `/billing/status`. By now the webhook has landed and the backend confirms.
3. **On every app foreground**: re-fetch `/billing/status`. Catches state changes that happened off-device (renewal, cancellation, billing issue).

## 11. Handle 402 responses

The backend gates several endpoints behind plan limits (see `BILLING_API_CONTRACT.md` for the full list). When a free user exceeds a limit, the endpoint returns HTTP **402 Payment Required** with:

```json
{
  "code": "plan_limit",
  "feature": "active_roadmaps",
  "limit": 1,
  "current": 1,
  "message": "Free users can have 1 active roadmap. Upgrade to create unlimited roadmaps."
}
```

Wire your API client (Axios interceptor / RTK Query baseQuery / whatever you use) to detect 402 and dispatch the paywall modal with the `feature` key. **Don't auto-retry** the original request after the user dismisses the paywall — they explicitly opted out.

The `feature` string is stable and you can key localized copy off it. See §3 of `BILLING_API_CONTRACT.md` for the full enum.

---

## 12. Required paywall disclosures (App Review)

Apple WILL reject the app if the paywall is missing any of these. Add them visibly on the paywall screen:

1. **Subscription title** (e.g., "Dead Reckoner Pro")
2. **Length of subscription period** (e.g., "Monthly subscription")
3. **Price per period** (e.g., "$9.99/month, billed monthly")
4. **Auto-renewal disclosure** — exact text:
   > Subscription auto-renews unless cancelled at least 24 hours before the end of the current period. Manage or cancel anytime in iOS Settings.
5. **Link to Privacy Policy** (in-app or browser)
6. **Link to Terms of Use / EULA** (in-app or browser) — Apple specifically requires this on the paywall, not just the App Store listing
7. **7-day free trial disclosure** (if showing the trial CTA): "7 days free, then $X/month"

## 13. Privacy Manifest

iOS 17+ requires declaring certain API usage. Add to `app.json`:

```json
{
  "expo": {
    "ios": {
      "privacyManifests": {
        "NSPrivacyAccessedAPITypes": [
          {
            "NSPrivacyAccessedAPIType": "NSPrivacyAccessedAPICategoryUserDefaults",
            "NSPrivacyAccessedAPITypeReasons": ["CA92.1"]
          }
        ]
      }
    }
  }
}
```

(The `react-native-purchases` config plugin handles most of the manifest entries automatically; this is the one common addition.)

---

## 14. Sandbox testing

1. On your iOS device: **Settings → Developer → Sandbox Apple Account** (NOT the production Apple ID setting). Sign in with a sandbox tester from App Store Connect.
2. Install the EAS dev build on the device.
3. Launch the app, log in to Supabase, navigate to the paywall.
4. Tap "Subscribe (monthly)" → Apple's sandbox sheet appears → confirm → purchase completes.
5. Sandbox renewal timing:
   - Weekly sub → renews every 3 minutes
   - Monthly sub → renews every 5 minutes
   - Annual sub → renews every hour
   - Trial → trial period is 3 minutes
6. You can force events from the RC dashboard's **Customers** view (find your test user, click "Send a test event" for any specific event type).

### Sandbox quirks to know

- The price shown in sandbox is sometimes wrong; it's a known StoreKit bug
- Sandbox accounts are limited to ~12 renewals before they stop renewing (then you need a new tester)
- iOS Simulator works for most sandbox testing; only family sharing and a few edge cases require a real device

---

## 15. End-to-end test checklist

When you think you're done, walk through this whole list on a real device:

- [ ] Fresh install + first launch
- [ ] Sign up via Supabase → confirm `logIn` was called (check RC dashboard, your user should appear under Customers)
- [ ] `GET /billing/status` returns `"plan": "free"`
- [ ] Trigger a 402 (try to create a 2nd roadmap as free user) → paywall opens with correct feature copy
- [ ] Subscribe to monthly with 7-day trial → purchase sheet → confirm
- [ ] Check RC dashboard → INITIAL_PURCHASE event appears
- [ ] Wait ~5 seconds, call `GET /billing/status` → returns `"plan": "paid", "status": "trialing", "trial_ends_at": "..."`
- [ ] Lock UI disappears, paid features unlock
- [ ] Restore Purchases button works on a fresh app install (sign in with same Apple ID, restore brings back entitlement)
- [ ] Cancel subscription via iOS Settings → CANCELLATION event in RC → backend still says paid (correct — access keeps until period end)
- [ ] Wait for sandbox renewal cycle → if cancelled, EXPIRATION event → backend flips to free
- [ ] Buy on iOS, sign in on Android with same `supabaseUserId` → entitlement should be active on both (RC handles cross-platform automatically because we called `logIn`)

---

## 16. Common pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Purchase succeeds but backend stays free | `logIn` not called or called with wrong ID | Verify `logIn(supabaseUserId)` runs after auth |
| `getOfferings()` returns null current | RC dashboard offering not configured | Ask user to set up the default offering in RC |
| Build crashes on launch with native error | Forgot to rebuild dev client after adding plugin | `npx expo prebuild --clean && eas build` |
| `restorePurchases()` does nothing in sandbox | Sandbox Apple ID not signed in on device | Settings → Developer → Sandbox Account |
| "Authorization header missing" in webhook logs | RC dashboard secret doesn't match `REVENUECAT_WEBHOOK_SECRET` env on backend | Coordinate the secret with backend dev |
| 402 fires but paywall has no copy | Reading `error` field instead of `feature` | The body shape is `{code, feature, limit, current, message}`, not generic |
| Paywall rejected by Apple Review | Missing one of the required disclosures (§12) | Check all 7 items are visible on the paywall |

---

## 17. What's mine vs yours (split of responsibilities)

| Task | Owner |
|---|---|
| RC SDK install + config plugin | **Frontend dev** |
| Dev client build | **Frontend dev** |
| `Purchases.logIn(userId)` on auth | **Frontend dev** |
| Paywall UI + purchase + restore flow | **Frontend dev** |
| 402 interceptor + paywall trigger | **Frontend dev** |
| Required Apple disclosures | **Frontend dev** |
| Privacy manifest | **Frontend dev** |
| Sandbox testing on devices | **Frontend dev** |
| App Store submission + review | **Frontend dev / user** |
| App Store Connect product setup | **User** |
| RevenueCat project setup, offerings, webhook secret | **User** |
| Backend webhook handler | **Backend (done — PR #55, #56)** |
| `GET /billing/status` endpoint | **Backend (done — PR #43)** |
| 402 responses from gated endpoints | **Backend (done — PR #45)** |
| Lifecycle emails (welcome, trial, cancellation) | **Backend (pending — Step 6.5)** |

---

## 18. References

- **RevenueCat Expo install guide**: https://www.revenuecat.com/docs/getting-started/installation/expo
- **RC purchases-react-native API docs**: https://revenuecat.github.io/react-native-purchases-docs/
- **Apple App Review Guidelines for IAP**: https://developer.apple.com/app-store/review/guidelines/#in-app-purchase
- **Backend API contract**: `BILLING_API_CONTRACT.md` (in this repo, top level)

## 19. Questions

Ping the backend dev (me) if you're seeing:
- 402 responses with unexpected shape
- Webhook events landing as `noop_unmapped` despite `logIn` being called
- Any new `feature` value you need wired into the backend gating logic

Plan changes (new product tiers, new entitlements, new gated endpoints) need backend changes — coordinate before promising anything to product.
