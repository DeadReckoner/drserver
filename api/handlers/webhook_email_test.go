package handlers

import (
	"testing"
	"time"

	"dr/server/email"
	"dr/server/webhooks/revenuecat"

	"github.com/stretchr/testify/assert"
)

func TestEmailForEvent_InitialPurchaseTrial_TrialStarted(t *testing.T) {
	expires := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC).UnixMilli()
	kind, vars := emailForEvent(revenuecat.Event{
		Type:           revenuecat.EventTypeInitialPurchase,
		PeriodType:     revenuecat.PeriodTypeTrial,
		ProductID:      "dr_pro_monthly",
		ExpirationAtMs: expires,
	})
	assert.Equal(t, email.KindTrialStarted, kind)
	assert.Equal(t, "June 8, 2026", vars.TrialEndsAt)
	assert.Equal(t, "monthly subscription", vars.ProductName)
}

func TestEmailForEvent_InitialPurchaseNoTrial_TrialConverted(t *testing.T) {
	kind, vars := emailForEvent(revenuecat.Event{
		Type:       revenuecat.EventTypeInitialPurchase,
		PeriodType: revenuecat.PeriodTypeNormal,
		ProductID:  "dr_pro_annual",
	})
	assert.Equal(t, email.KindTrialConverted, kind)
	assert.Equal(t, "annual subscription", vars.ProductName)
}

func TestEmailForEvent_RenewalTrialConversion_TrialConverted(t *testing.T) {
	kind, _ := emailForEvent(revenuecat.Event{
		Type:              revenuecat.EventTypeRenewal,
		IsTrialConversion: true,
		ProductID:         "dr_pro_monthly",
	})
	assert.Equal(t, email.KindTrialConverted, kind)
}

func TestEmailForEvent_RegularRenewal_Silent(t *testing.T) {
	kind, _ := emailForEvent(revenuecat.Event{
		Type:      revenuecat.EventTypeRenewal,
		ProductID: "dr_pro_monthly",
	})
	assert.Empty(t, string(kind), "regular renewals should not send email")
}

func TestEmailForEvent_Cancellation(t *testing.T) {
	kind, vars := emailForEvent(revenuecat.Event{
		Type:           revenuecat.EventTypeCancellation,
		ExpirationAtMs: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC).UnixMilli(),
		ProductID:      "dr_pro_monthly",
	})
	assert.Equal(t, email.KindCancelled, kind)
	assert.Equal(t, "June 30, 2026", vars.AccessUntil)
	assert.NotEmpty(t, vars.ResubscribeURL)
}

func TestEmailForEvent_Expiration(t *testing.T) {
	kind, vars := emailForEvent(revenuecat.Event{
		Type: revenuecat.EventTypeExpiration,
	})
	assert.Equal(t, email.KindExpired, kind)
	assert.NotEmpty(t, vars.ResubscribeURL)
}

func TestEmailForEvent_BillingIssue(t *testing.T) {
	kind, vars := emailForEvent(revenuecat.Event{
		Type: revenuecat.EventTypeBillingIssue,
	})
	assert.Equal(t, email.KindBillingIssue, kind)
	assert.NotEmpty(t, vars.ManageSubscriptionURL)
}

func TestEmailForEvent_Silent(t *testing.T) {
	for _, tt := range []string{
		revenuecat.EventTypeProductChange,
		revenuecat.EventTypeUncancellation,
		revenuecat.EventTypeRefund,
		revenuecat.EventTypeTransfer,
		revenuecat.EventTypeTest,
	} {
		t.Run(tt, func(t *testing.T) {
			kind, _ := emailForEvent(revenuecat.Event{Type: tt})
			assert.Empty(t, string(kind), "expected silent")
		})
	}
}

func TestProductNameFor(t *testing.T) {
	assert.Equal(t, "monthly subscription", productNameFor("dr_pro_monthly"))
	assert.Equal(t, "annual subscription", productNameFor("dr_pro_annual"))
	assert.Equal(t, "subscription", productNameFor("unknown_product"))
}

func TestFormatMs(t *testing.T) {
	assert.Equal(t, "", formatMs(0))
	assert.Equal(t,
		"June 1, 2026",
		formatMs(time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC).UnixMilli()))
}
