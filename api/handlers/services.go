package handlers

import (
	"dr/server/database"
	"dr/server/types"

	"github.com/gin-gonic/gin"
)

// RoadmapService is the seam between RoadmapHandler and the persistence
// layer. Only methods exercised by handler-level tests live here; untested
// code paths (anchor, share, realign) continue to call the database package
// directly and can be migrated as they gain test coverage.
//
// The signatures intentionally match the existing database.X(...) functions
// so DBRoadmapService is a trivial 1:1 adapter.
type RoadmapService interface {
	// CRUD
	Create(c *gin.Context, r types.Roadmap) (types.Roadmap, error)
	Get(c *gin.Context, roadmapID string) types.Roadmap
	GetFull(c *gin.Context, roadmapID string) types.Roadmap
	GetStrategic(c *gin.Context, roadmapID string) types.Roadmap
	ListByUser(c *gin.Context) []types.Roadmap
	Update(c *gin.Context, roadmapID string, r types.Roadmap) types.Roadmap
	Delete(c *gin.Context, roadmapID string) error

	// Lifecycle
	Restart(c *gin.Context, roadmapID string) (types.Roadmap, error)
	ShiftDates(c *gin.Context, roadmapID string, days int) (types.Roadmap, error)
	Pause(c *gin.Context, roadmapID string) error
	Resume(c *gin.Context, roadmapID string) (types.Roadmap, error)
	Archive(c *gin.Context, roadmapID string) error
	Restore(c *gin.Context, roadmapID string, startFromToday bool) (types.Roadmap, error)
}

// DBRoadmapService is the production implementation: a stateless adapter
// that delegates to the package-level database functions (which use the
// package-global *gorm.DB).
type DBRoadmapService struct{}

func (DBRoadmapService) Create(c *gin.Context, r types.Roadmap) (types.Roadmap, error) {
	return database.CreateRoadmap(c, r)
}
func (DBRoadmapService) Get(c *gin.Context, id string) types.Roadmap {
	return database.GetRoadmap(c, id)
}
func (DBRoadmapService) GetFull(c *gin.Context, id string) types.Roadmap {
	return database.GetRoadmapFull(c, id)
}
func (DBRoadmapService) GetStrategic(c *gin.Context, id string) types.Roadmap {
	return database.GetRoadmapWithStrategicView(c, id)
}
func (DBRoadmapService) ListByUser(c *gin.Context) []types.Roadmap {
	return database.GetRoadmapsByUser(c)
}
func (DBRoadmapService) Update(c *gin.Context, id string, r types.Roadmap) types.Roadmap {
	return database.UpdateRoadmap(c, id, r)
}
func (DBRoadmapService) Delete(c *gin.Context, id string) error {
	return database.DeleteRoadmap(c, id)
}
func (DBRoadmapService) Restart(c *gin.Context, id string) (types.Roadmap, error) {
	return database.RestartRoadmap(c, id)
}
func (DBRoadmapService) ShiftDates(c *gin.Context, id string, days int) (types.Roadmap, error) {
	return database.ShiftRoadmapDates(c, id, days)
}
func (DBRoadmapService) Pause(c *gin.Context, id string) error {
	return database.PauseRoadmap(c, id)
}
func (DBRoadmapService) Resume(c *gin.Context, id string) (types.Roadmap, error) {
	return database.ResumeRoadmap(c, id)
}
func (DBRoadmapService) Archive(c *gin.Context, id string) error {
	return database.ArchiveRoadmap(c, id)
}
func (DBRoadmapService) Restore(c *gin.Context, id string, startFromToday bool) (types.Roadmap, error) {
	return database.RestoreRoadmap(c, id, startFromToday)
}

// TaskService is the seam between TaskHandler and the persistence layer.
// See RoadmapService for the rationale on scope.
type TaskService interface {
	Create(c *gin.Context, t types.Task) (types.Task, error)
	Get(c *gin.Context, taskID string) types.Task
	List(c *gin.Context, startDate, endDate, roadmapID string) []types.Task
	Update(c *gin.Context, taskID string, t types.Task) types.Task
	Delete(c *gin.Context, taskID string) error

	UpdateStep(c *gin.Context, taskID, stepID string, s types.Step) types.Step
	CompleteAllSteps(c *gin.Context, taskID string) (int64, error)
}

// DBTaskService is the production implementation: a stateless adapter.
type DBTaskService struct{}

func (DBTaskService) Create(c *gin.Context, t types.Task) (types.Task, error) {
	return database.CreateTask(c, t)
}
func (DBTaskService) Get(c *gin.Context, id string) types.Task {
	return database.GetTask(c, id)
}
func (DBTaskService) List(c *gin.Context, start, end, roadmapID string) []types.Task {
	return database.GetTasks(c, start, end, roadmapID)
}
func (DBTaskService) Update(c *gin.Context, id string, t types.Task) types.Task {
	return database.UpdateTask(c, id, t)
}
func (DBTaskService) Delete(c *gin.Context, id string) error {
	return database.DeleteTask(c, id)
}
func (DBTaskService) UpdateStep(c *gin.Context, taskID, stepID string, s types.Step) types.Step {
	return database.UpdateStep(c, taskID, stepID, s)
}
func (DBTaskService) CompleteAllSteps(c *gin.Context, taskID string) (int64, error) {
	return database.CompleteAllSteps(c, taskID)
}
