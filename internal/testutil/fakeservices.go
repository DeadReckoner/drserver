package testutil

import (
	"dr/server/api/handlers"
	"dr/server/types"

	"github.com/gin-gonic/gin"
)

// FakeRoadmapService implements handlers.RoadmapService for tests. Each
// method delegates to a func field set by the test; methods that aren't
// stubbed panic loudly so a misrouted call doesn't silently return zero
// values and produce a misleading pass.
type FakeRoadmapService struct {
	CreateFunc       func(c *gin.Context, r types.Roadmap) (types.Roadmap, error)
	GetFunc          func(c *gin.Context, roadmapID string) types.Roadmap
	GetFullFunc      func(c *gin.Context, roadmapID string) types.Roadmap
	GetStrategicFunc func(c *gin.Context, roadmapID string) types.Roadmap
	ListByUserFunc   func(c *gin.Context) []types.Roadmap
	UpdateFunc       func(c *gin.Context, roadmapID string, r types.Roadmap) types.Roadmap
	DeleteFunc       func(c *gin.Context, roadmapID string) error

	RestartFunc    func(c *gin.Context, roadmapID string) (types.Roadmap, error)
	ShiftDatesFunc func(c *gin.Context, roadmapID string, days int) (types.Roadmap, error)
	PauseFunc      func(c *gin.Context, roadmapID string) error
	ResumeFunc     func(c *gin.Context, roadmapID string) (types.Roadmap, error)
	ArchiveFunc    func(c *gin.Context, roadmapID string) error
	RestoreFunc    func(c *gin.Context, roadmapID string, startFromToday bool) (types.Roadmap, error)
}

var _ handlers.RoadmapService = (*FakeRoadmapService)(nil)

func (f *FakeRoadmapService) Create(c *gin.Context, r types.Roadmap) (types.Roadmap, error) {
	if f.CreateFunc == nil {
		panic("FakeRoadmapService.CreateFunc not set")
	}
	return f.CreateFunc(c, r)
}
func (f *FakeRoadmapService) Get(c *gin.Context, id string) types.Roadmap {
	if f.GetFunc == nil {
		panic("FakeRoadmapService.GetFunc not set")
	}
	return f.GetFunc(c, id)
}
func (f *FakeRoadmapService) GetFull(c *gin.Context, id string) types.Roadmap {
	if f.GetFullFunc == nil {
		panic("FakeRoadmapService.GetFullFunc not set")
	}
	return f.GetFullFunc(c, id)
}
func (f *FakeRoadmapService) GetStrategic(c *gin.Context, id string) types.Roadmap {
	if f.GetStrategicFunc == nil {
		panic("FakeRoadmapService.GetStrategicFunc not set")
	}
	return f.GetStrategicFunc(c, id)
}
func (f *FakeRoadmapService) ListByUser(c *gin.Context) []types.Roadmap {
	if f.ListByUserFunc == nil {
		panic("FakeRoadmapService.ListByUserFunc not set")
	}
	return f.ListByUserFunc(c)
}
func (f *FakeRoadmapService) Update(c *gin.Context, id string, r types.Roadmap) types.Roadmap {
	if f.UpdateFunc == nil {
		panic("FakeRoadmapService.UpdateFunc not set")
	}
	return f.UpdateFunc(c, id, r)
}
func (f *FakeRoadmapService) Delete(c *gin.Context, id string) error {
	if f.DeleteFunc == nil {
		panic("FakeRoadmapService.DeleteFunc not set")
	}
	return f.DeleteFunc(c, id)
}
func (f *FakeRoadmapService) Restart(c *gin.Context, id string) (types.Roadmap, error) {
	if f.RestartFunc == nil {
		panic("FakeRoadmapService.RestartFunc not set")
	}
	return f.RestartFunc(c, id)
}
func (f *FakeRoadmapService) ShiftDates(c *gin.Context, id string, days int) (types.Roadmap, error) {
	if f.ShiftDatesFunc == nil {
		panic("FakeRoadmapService.ShiftDatesFunc not set")
	}
	return f.ShiftDatesFunc(c, id, days)
}
func (f *FakeRoadmapService) Pause(c *gin.Context, id string) error {
	if f.PauseFunc == nil {
		panic("FakeRoadmapService.PauseFunc not set")
	}
	return f.PauseFunc(c, id)
}
func (f *FakeRoadmapService) Resume(c *gin.Context, id string) (types.Roadmap, error) {
	if f.ResumeFunc == nil {
		panic("FakeRoadmapService.ResumeFunc not set")
	}
	return f.ResumeFunc(c, id)
}
func (f *FakeRoadmapService) Archive(c *gin.Context, id string) error {
	if f.ArchiveFunc == nil {
		panic("FakeRoadmapService.ArchiveFunc not set")
	}
	return f.ArchiveFunc(c, id)
}
func (f *FakeRoadmapService) Restore(c *gin.Context, id string, startFromToday bool) (types.Roadmap, error) {
	if f.RestoreFunc == nil {
		panic("FakeRoadmapService.RestoreFunc not set")
	}
	return f.RestoreFunc(c, id, startFromToday)
}

// FakeTaskService implements handlers.TaskService for tests.
type FakeTaskService struct {
	CreateFunc           func(c *gin.Context, t types.Task) (types.Task, error)
	GetFunc              func(c *gin.Context, taskID string) types.Task
	ListFunc             func(c *gin.Context, startDate, endDate, roadmapID string) []types.Task
	UpdateFunc           func(c *gin.Context, taskID string, t types.Task) types.Task
	DeleteFunc           func(c *gin.Context, taskID string) error
	UpdateStepFunc       func(c *gin.Context, taskID, stepID string, s types.Step) types.Step
	CompleteAllStepsFunc func(c *gin.Context, taskID string) (int64, error)
}

var _ handlers.TaskService = (*FakeTaskService)(nil)

func (f *FakeTaskService) Create(c *gin.Context, t types.Task) (types.Task, error) {
	if f.CreateFunc == nil {
		panic("FakeTaskService.CreateFunc not set")
	}
	return f.CreateFunc(c, t)
}
func (f *FakeTaskService) Get(c *gin.Context, id string) types.Task {
	if f.GetFunc == nil {
		panic("FakeTaskService.GetFunc not set")
	}
	return f.GetFunc(c, id)
}
func (f *FakeTaskService) List(c *gin.Context, start, end, roadmapID string) []types.Task {
	if f.ListFunc == nil {
		panic("FakeTaskService.ListFunc not set")
	}
	return f.ListFunc(c, start, end, roadmapID)
}
func (f *FakeTaskService) Update(c *gin.Context, id string, t types.Task) types.Task {
	if f.UpdateFunc == nil {
		panic("FakeTaskService.UpdateFunc not set")
	}
	return f.UpdateFunc(c, id, t)
}
func (f *FakeTaskService) Delete(c *gin.Context, id string) error {
	if f.DeleteFunc == nil {
		panic("FakeTaskService.DeleteFunc not set")
	}
	return f.DeleteFunc(c, id)
}
func (f *FakeTaskService) UpdateStep(c *gin.Context, taskID, stepID string, s types.Step) types.Step {
	if f.UpdateStepFunc == nil {
		panic("FakeTaskService.UpdateStepFunc not set")
	}
	return f.UpdateStepFunc(c, taskID, stepID, s)
}
func (f *FakeTaskService) CompleteAllSteps(c *gin.Context, taskID string) (int64, error) {
	if f.CompleteAllStepsFunc == nil {
		panic("FakeTaskService.CompleteAllStepsFunc not set")
	}
	return f.CompleteAllStepsFunc(c, taskID)
}
