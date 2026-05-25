package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEntitlement_IsPaid(t *testing.T) {
	tests := []struct {
		name string
		plan string
		want bool
	}{
		{"free user", PlanFree, false},
		{"paid user", PlanPaid, true},
		{"unknown plan defaults to not paid", "weird", false},
		{"empty plan defaults to not paid", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Entitlement{Plan: tt.plan}
			assert.Equal(t, tt.want, e.IsPaid())
		})
	}
}

func TestEntitlement_CanUseFeature(t *testing.T) {
	free := &Entitlement{Plan: PlanFree}
	paid := &Entitlement{Plan: PlanPaid}

	gatedFeatures := []FeatureKey{
		FeatureCustomReminders,
		FeatureAnchorSettings,
		FeatureHabitAnchors,
		FeatureAdvancedStats,
		FeatureHistoricalPerf,
		FeatureArchive,
		FeatureWeeklyReview,
		FeatureMessagingReactions,
	}

	for _, f := range gatedFeatures {
		t.Run(string(f)+"/free=denied", func(t *testing.T) {
			assert.False(t, free.CanUseFeature(f))
		})
		t.Run(string(f)+"/paid=granted", func(t *testing.T) {
			assert.True(t, paid.CanUseFeature(f))
		})
	}
}

func TestLimitsForPlan(t *testing.T) {
	assert.Equal(t, FreeLimits, LimitsForPlan(PlanFree))
	assert.Equal(t, PaidLimits, LimitsForPlan(PlanPaid))
	assert.Equal(t, FreeLimits, LimitsForPlan("unknown"), "unknown plan should fall back to free limits")
}

func TestFreeLimits_MatchesProductSpec(t *testing.T) {
	// Guards against accidental drift from the product spec in the ticket.
	assert.Equal(t, 1, FreeLimits.ActiveRoadmaps)
	assert.Equal(t, 3, FreeLimits.ActiveHabits)
	assert.Equal(t, 1, FreeLimits.AnchorsPerRoadmap)
	assert.Equal(t, 3, FreeLimits.AIGenerationsPerMonth)
}

func TestPaidLimits_AllUnlimited(t *testing.T) {
	assert.Equal(t, -1, PaidLimits.ActiveRoadmaps)
	assert.Equal(t, -1, PaidLimits.ActiveHabits)
	assert.Equal(t, -1, PaidLimits.AnchorsPerRoadmap)
	assert.Equal(t, -1, PaidLimits.AIGenerationsPerMonth)
}

func TestEntitlement_Limits(t *testing.T) {
	assert.Equal(t, FreeLimits, (&Entitlement{Plan: PlanFree}).Limits())
	assert.Equal(t, PaidLimits, (&Entitlement{Plan: PlanPaid}).Limits())
}
