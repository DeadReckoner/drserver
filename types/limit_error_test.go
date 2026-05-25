package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlanLimitError_Error(t *testing.T) {
	e := &PlanLimitError{Feature: "active_roadmaps", Limit: 1, Current: 1}
	assert.Equal(t, "plan limit hit: active_roadmaps (limit=1, current=1)", e.Error())
}

func TestPlanLimitError_Error_NilSafe(t *testing.T) {
	var e *PlanLimitError
	assert.Equal(t, "", e.Error())
}
