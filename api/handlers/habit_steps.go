package handlers

import (
	"net/http"
	"time"

	"dr/server/api/utils"
	"dr/server/log"
	"dr/server/types"

	"github.com/gin-gonic/gin"
)

// Multi-step recurring habits/tasks.
//
// Two distinct surfaces, matching the template/occurrence split in the data
// model:
//
//	/habits/:habitid/steps            step DEFINITIONS on the recurring item
//	/habits/:habitid/occurrence/steps step STATE for one dated occurrence
//
// Mixing them into one endpoint would make "check today's step" and "rename the
// step forever" the same request, which is precisely the confusion the split
// exists to prevent.

// parseOccurrenceDate reads ?date=YYYY-MM-DD, defaulting to today. Mirrors the
// date handling already used by UncompleteHabit.
func parseOccurrenceDate(c *gin.Context) (time.Time, bool) {
	if s := c.Query("date"); s != "" {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			utils.BadRequestResponse(c, "invalid date format, use YYYY-MM-DD")
			return time.Time{}, false
		}
		return d, true
	}
	return time.Now(), true
}

// respondHabitStepError maps the domain errors onto status codes. Not-found is
// also what an unowned habit returns, so probing another user's UUIDs cannot
// distinguish "exists but yours" from "does not exist".
func respondHabitStepError(c *gin.Context, err error) {
	switch err {
	case types.ErrHabitNotFound:
		utils.NotFoundResponse(c, "Habit")
	case types.ErrHabitStepNotFound:
		utils.NotFoundResponse(c, "Step")
	case types.ErrInvalidStepStatus, types.ErrStepStateRequired, types.ErrInvalidOccurrenceDate:
		utils.BadRequestResponse(c, err.Error())
	default:
		utils.InternalErrorResponse(c, err)
	}
}

// GetHabitSteps returns the template's step definitions.
// GET /api/v1/habits/:habitid/steps
func (h *HabitHandler) GetHabitSteps(c *gin.Context) {
	steps, err := h.service.Steps(c, c.Param("habitid"))
	if err != nil {
		respondHabitStepError(c, err)
		return
	}
	utils.DataResponse(c, http.StatusOK, steps)
}

// CreateHabitStep appends a step definition.
// POST /api/v1/habits/:habitid/steps
func (h *HabitHandler) CreateHabitStep(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")

	var req types.CreateHabitStepRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	step, err := h.service.CreateStep(c, habitID, req)
	if err != nil {
		respondHabitStepError(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, step)
	log.Info(ctx, "Habit step created", step.HabitStepID)
}

// UpdateHabitStep edits a step definition (label/order).
// PATCH /api/v1/habits/:habitid/steps/:stepid
func (h *HabitHandler) UpdateHabitStep(c *gin.Context) {
	ctx := c.Request.Context()

	var req types.UpdateHabitStepRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	step, err := h.service.UpdateStep(c, c.Param("habitid"), c.Param("stepid"), req)
	if err != nil {
		respondHabitStepError(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, step)
	log.Info(ctx, "Habit step updated", step.HabitStepID)
}

// DeleteHabitStep removes a step definition. Past occurrences keep their record
// of it; only the active occurrence loses it.
// DELETE /api/v1/habits/:habitid/steps/:stepid
func (h *HabitHandler) DeleteHabitStep(c *gin.Context) {
	ctx := c.Request.Context()
	stepID := c.Param("stepid")

	if err := h.service.DeleteStep(c, c.Param("habitid"), stepID); err != nil {
		respondHabitStepError(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Step deleted successfully")
	log.Info(ctx, "Habit step deleted", stepID)
}

// ReorderHabitSteps sets display order from a single ordered id list.
// POST /api/v1/habits/:habitid/steps/reorder
func (h *HabitHandler) ReorderHabitSteps(c *gin.Context) {
	var req types.ReorderHabitStepsRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	steps, err := h.service.ReorderSteps(c, c.Param("habitid"), req.StepIDs)
	if err != nil {
		respondHabitStepError(c, err)
		return
	}
	utils.DataResponse(c, http.StatusOK, steps)
}

// GetHabitOccurrenceSteps returns the checklist for one occurrence.
// GET /api/v1/habits/:habitid/occurrence/steps?date=YYYY-MM-DD
func (h *HabitHandler) GetHabitOccurrenceSteps(c *gin.Context) {
	date, ok := parseOccurrenceDate(c)
	if !ok {
		return
	}

	steps, err := h.service.OccurrenceSteps(c, c.Param("habitid"), date)
	if err != nil {
		respondHabitStepError(c, err)
		return
	}
	utils.DataResponse(c, http.StatusOK, steps)
}

// SetHabitStepState checks/unchecks/skips one step on one occurrence, and
// returns the whole occurrence so the client gets the recomputed parent state
// and progress counters in the same round trip -- no follow-up GET, and no
// window where the UI has to guess whether the parent flipped.
// PATCH /api/v1/habits/:habitid/occurrence/steps/:stepid
func (h *HabitHandler) SetHabitStepState(c *gin.Context) {
	ctx := c.Request.Context()
	habitID := c.Param("habitid")
	stepID := c.Param("stepid")

	var req types.SetHabitStepStateRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}

	status, err := req.Resolve()
	if err != nil {
		respondHabitStepError(c, err)
		return
	}

	date := time.Now()
	if req.Date != nil && !req.Date.IsZero() {
		date = req.Date.Time
	}

	result, err := h.service.SetStepState(c, habitID, stepID, date, status)
	if err != nil {
		respondHabitStepError(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, result)
	log.Info(ctx, "Habit step state set", stepID, status)
}
