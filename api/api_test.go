package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// DatabaseInterface defines the interface for database operations
type DatabaseInterface interface {
	CreateRoadmap(*gin.Context, types.Roadmap) (types.Roadmap, error)
	GetRoadmapsByUser(*gin.Context) []types.Roadmap
	GetRoadmap(*gin.Context, string) types.Roadmap
	UpdateRoadmap(*gin.Context, string, types.Roadmap) types.Roadmap
	DeleteRoadmap(*gin.Context, string) error
	GetMilestonesByRoadmap(*gin.Context, string) []types.Milestone
	GetMilestone(*gin.Context, string) types.Milestone
	UpdateMilestone(*gin.Context, string, types.Milestone) types.Milestone
	CreateTask(*gin.Context, types.Task) (types.Task, error)
	GetTasksByMilestone(*gin.Context, string) []types.Task
	GetTask(*gin.Context, string) types.Task
	UpdateTask(*gin.Context, string, types.Task) types.Task
	DeleteTask(*gin.Context, string) error
}

// MockDatabase implements the DatabaseInterface for testing
type MockDatabase struct {
	CreateRoadmapFunc          func(*gin.Context, types.Roadmap) (types.Roadmap, error)
	GetRoadmapsByUserFunc      func(*gin.Context) []types.Roadmap
	GetRoadmapFunc             func(*gin.Context, string) types.Roadmap
	UpdateRoadmapFunc          func(*gin.Context, string, types.Roadmap) types.Roadmap
	DeleteRoadmapFunc          func(*gin.Context, string) error
	GetMilestonesByRoadmapFunc func(*gin.Context, string) []types.Milestone
	GetMilestoneFunc           func(*gin.Context, string) types.Milestone
	UpdateMilestoneFunc        func(*gin.Context, string, types.Milestone) types.Milestone
	CreateTaskFunc             func(*gin.Context, types.Task) (types.Task, error)
	GetTasksByMilestoneFunc    func(*gin.Context, string) []types.Task
	GetTaskFunc                func(*gin.Context, string) types.Task
	UpdateTaskFunc             func(*gin.Context, string, types.Task) types.Task
	DeleteTaskFunc             func(*gin.Context, string) error
}

func (m *MockDatabase) CreateRoadmap(c *gin.Context, rm types.Roadmap) (types.Roadmap, error) {
	if m.CreateRoadmapFunc != nil {
		return m.CreateRoadmapFunc(c, rm)
	}
	return types.Roadmap{}, nil
}

func (m *MockDatabase) GetRoadmapsByUser(c *gin.Context) []types.Roadmap {
	if m.GetRoadmapsByUserFunc != nil {
		return m.GetRoadmapsByUserFunc(c)
	}
	return []types.Roadmap{}
}

func (m *MockDatabase) GetRoadmap(c *gin.Context, id string) types.Roadmap {
	if m.GetRoadmapFunc != nil {
		return m.GetRoadmapFunc(c, id)
	}
	return types.Roadmap{}
}

func (m *MockDatabase) UpdateRoadmap(c *gin.Context, id string, rm types.Roadmap) types.Roadmap {
	if m.UpdateRoadmapFunc != nil {
		return m.UpdateRoadmapFunc(c, id, rm)
	}
	return types.Roadmap{}
}

func (m *MockDatabase) DeleteRoadmap(c *gin.Context, id string) error {
	if m.DeleteRoadmapFunc != nil {
		return m.DeleteRoadmapFunc(c, id)
	}
	return nil
}

func (m *MockDatabase) GetMilestonesByRoadmap(c *gin.Context, id string) []types.Milestone {
	if m.GetMilestonesByRoadmapFunc != nil {
		return m.GetMilestonesByRoadmapFunc(c, id)
	}
	return []types.Milestone{}
}

func (m *MockDatabase) GetMilestone(c *gin.Context, id string) types.Milestone {
	if m.GetMilestoneFunc != nil {
		return m.GetMilestoneFunc(c, id)
	}
	return types.Milestone{}
}

func (m *MockDatabase) UpdateMilestone(c *gin.Context, id string, ms types.Milestone) types.Milestone {
	if m.UpdateMilestoneFunc != nil {
		return m.UpdateMilestoneFunc(c, id, ms)
	}
	return types.Milestone{}
}

func (m *MockDatabase) CreateTask(c *gin.Context, task types.Task) (types.Task, error) {
	if m.CreateTaskFunc != nil {
		return m.CreateTaskFunc(c, task)
	}
	return types.Task{}, nil
}

func (m *MockDatabase) GetTasksByMilestone(c *gin.Context, milestoneID string) []types.Task {
	if m.GetTasksByMilestoneFunc != nil {
		return m.GetTasksByMilestoneFunc(c, milestoneID)
	}
	return []types.Task{}
}

func (m *MockDatabase) GetTask(c *gin.Context, taskID string) types.Task {
	if m.GetTaskFunc != nil {
		return m.GetTaskFunc(c, taskID)
	}
	return types.Task{}
}

func (m *MockDatabase) UpdateTask(c *gin.Context, taskID string, task types.Task) types.Task {
	if m.UpdateTaskFunc != nil {
		return m.UpdateTaskFunc(c, taskID, task)
	}
	return types.Task{}
}

func (m *MockDatabase) DeleteTask(c *gin.Context, taskID string) error {
	if m.DeleteTaskFunc != nil {
		return m.DeleteTaskFunc(c, taskID)
	}
	return nil
}

var mockDB *MockDatabase

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.POST("/roadmap", createRoadmapWithMock)
	r.GET("/roadmaps", getRoadmapsByUserWithMock)
	r.GET("/roadmap/:roadmapID", getRoadmapWithMock)
	r.PATCH("/roadmap/:roadmapID", updateRoadmapWithMock)
	r.DELETE("/roadmap/:roadmapID", deleteRoadmapWithMock)
	r.GET("/roadmap/milestones/:roadmapid", getMilestonesByRoadmapWithMock)
	r.GET("/roadmap/milestone/:milestoneid", getMilestoneWithMock)
	r.PATCH("/roadmap/milestone/:milestoneid", updateMilestoneWithMock)
	r.POST("/milestone/task", createTaskWithMock)
	r.GET("/milestone/tasks/:milestoneid", getTasksByMilestoneWithMock)
	r.GET("/milestone/task/:taskid", getTaskWithMock)
	r.PATCH("/milestone/task/:taskid", updateTaskWithMock)
	r.DELETE("/milestone/task/:taskid", deleteTaskWithMock)
	return r
}

// Mock handlers that use the mock database
func createRoadmapWithMock(c *gin.Context) {
	var newRoadmap types.Roadmap
	if err := c.BindJSON(&newRoadmap); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	createdRoadmap, err := mockDB.CreateRoadmap(c, newRoadmap)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.IndentedJSON(http.StatusCreated, createdRoadmap)
}

func getRoadmapsByUserWithMock(c *gin.Context) {
	roadmaps := mockDB.GetRoadmapsByUser(c)
	c.IndentedJSON(http.StatusOK, roadmaps)
}

func getRoadmapWithMock(c *gin.Context) {
	roadmapID := c.Param("roadmapID")
	roadmap := mockDB.GetRoadmap(c, roadmapID)
	c.IndentedJSON(http.StatusOK, roadmap)
}

func updateRoadmapWithMock(c *gin.Context) {
	roadmapID := c.Param("roadmapID")
	var updatedRoadmap types.Roadmap
	if err := c.BindJSON(&updatedRoadmap); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	roadmap := mockDB.UpdateRoadmap(c, roadmapID, updatedRoadmap)
	c.IndentedJSON(http.StatusOK, roadmap)
}

func deleteRoadmapWithMock(c *gin.Context) {
	roadmapID := c.Param("roadmapID")
	err := mockDB.DeleteRoadmap(c, roadmapID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Roadmap deleted successfully"})
}

func getMilestonesByRoadmapWithMock(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	milestones := mockDB.GetMilestonesByRoadmap(c, roadmapID)
	c.IndentedJSON(http.StatusOK, milestones)
}

func getMilestoneWithMock(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	milestone := mockDB.GetMilestone(c, milestoneID)
	c.IndentedJSON(http.StatusOK, milestone)
}

func updateMilestoneWithMock(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	var updatedMilestone types.Milestone
	if err := c.BindJSON(&updatedMilestone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	milestone := mockDB.UpdateMilestone(c, milestoneID, updatedMilestone)
	c.IndentedJSON(http.StatusOK, milestone)
}

func createTaskWithMock(c *gin.Context) {
	var newTask types.Task
	if err := c.BindJSON(&newTask); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	createdTask, err := mockDB.CreateTask(c, newTask)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.IndentedJSON(http.StatusCreated, createdTask)
}

func getTasksByMilestoneWithMock(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	tasks := mockDB.GetTasksByMilestone(c, milestoneID)
	c.IndentedJSON(http.StatusOK, tasks)
}

func getTaskWithMock(c *gin.Context) {
	taskID := c.Param("taskid")
	task := mockDB.GetTask(c, taskID)
	c.IndentedJSON(http.StatusOK, task)
}

func updateTaskWithMock(c *gin.Context) {
	taskID := c.Param("taskid")
	var updatedTask types.Task
	if err := c.BindJSON(&updatedTask); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	task := mockDB.UpdateTask(c, taskID, updatedTask)
	c.IndentedJSON(http.StatusOK, task)
}

func deleteTaskWithMock(c *gin.Context) {
	taskID := c.Param("taskid")
	err := mockDB.DeleteTask(c, taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task deleted successfully"})
}

func TestCreateRoadmap_Success(t *testing.T) {
	mockDB = &MockDatabase{
		CreateRoadmapFunc: func(c *gin.Context, r types.Roadmap) (types.Roadmap, error) {
			r.RoadmapID = "test-id"
			return r, nil
		},
	}
	router := setupRouter()

	body := `{"goal":"Test Goal"}`
	req, _ := http.NewRequest("POST", "/roadmap", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp types.Roadmap
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Test Goal", resp.Goal)
}

func TestCreateRoadmap_BadRequest(t *testing.T) {
	mockDB = &MockDatabase{}
	router := setupRouter()
	req, _ := http.NewRequest("POST", "/roadmap", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetRoadmapsByUser(t *testing.T) {
	mockDB = &MockDatabase{
		GetRoadmapsByUserFunc: func(c *gin.Context) []types.Roadmap {
			return []types.Roadmap{{Goal: "G1"}, {Goal: "G2"}}
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/roadmaps", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetRoadmap(t *testing.T) {
	mockDB = &MockDatabase{
		GetRoadmapFunc: func(c *gin.Context, id string) types.Roadmap {
			return types.Roadmap{RoadmapID: id, Goal: "G"}
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/roadmap/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDeleteRoadmap_Error(t *testing.T) {
	mockDB = &MockDatabase{
		DeleteRoadmapFunc: func(c *gin.Context, id string) error {
			return errors.New("fail")
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("DELETE", "/roadmap/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Task endpoint tests
func TestCreateTask_Success(t *testing.T) {
	mockDB = &MockDatabase{
		CreateTaskFunc: func(c *gin.Context, task types.Task) (types.Task, error) {
			task.TaskID = "test-task-id"
			return task, nil
		},
	}
	router := setupRouter()

	body := `{"milestone_id":"milestone-123","title":"Test Task","description":"Test Description","day":1,"completed":false}`
	req, _ := http.NewRequest("POST", "/milestone/task", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp types.Task
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Test Task", resp.Title)
	assert.Equal(t, "test-task-id", resp.TaskID)
}

func TestCreateTask_BadRequest(t *testing.T) {
	mockDB = &MockDatabase{}
	router := setupRouter()
	req, _ := http.NewRequest("POST", "/milestone/task", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateTask_DatabaseError(t *testing.T) {
	mockDB = &MockDatabase{
		CreateTaskFunc: func(c *gin.Context, task types.Task) (types.Task, error) {
			return types.Task{}, errors.New("database error")
		},
	}
	router := setupRouter()

	body := `{"milestone_id":"milestone-123","title":"Test Task"}`
	req, _ := http.NewRequest("POST", "/milestone/task", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetTasksByMilestone_Success(t *testing.T) {
	mockDB = &MockDatabase{
		GetTasksByMilestoneFunc: func(c *gin.Context, milestoneID string) []types.Task {
			return []types.Task{
				{TaskID: "task-1", Title: "Task 1", MilestoneID: milestoneID},
				{TaskID: "task-2", Title: "Task 2", MilestoneID: milestoneID},
			}
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/milestone/tasks/milestone-123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []types.Task
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Len(t, resp, 2)
	assert.Equal(t, "Task 1", resp[0].Title)
	assert.Equal(t, "milestone-123", resp[0].MilestoneID)
}

func TestGetTasksByMilestone_EmptyResult(t *testing.T) {
	mockDB = &MockDatabase{
		GetTasksByMilestoneFunc: func(c *gin.Context, milestoneID string) []types.Task {
			return []types.Task{}
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/milestone/tasks/milestone-123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []types.Task
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Len(t, resp, 0)
}

func TestGetTask_Success(t *testing.T) {
	mockDB = &MockDatabase{
		GetTaskFunc: func(c *gin.Context, taskID string) types.Task {
			return types.Task{
				TaskID: taskID,
				Title:  "Test Task",
				Description: "Test Description",
				Day: 1,
				Completed: false,
			}
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/milestone/task/task-123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp types.Task
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "task-123", resp.TaskID)
	assert.Equal(t, "Test Task", resp.Title)
}

func TestGetTask_NotFound(t *testing.T) {
	mockDB = &MockDatabase{
		GetTaskFunc: func(c *gin.Context, taskID string) types.Task {
			return types.Task{}
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/milestone/task/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp types.Task
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "", resp.TaskID)
}

func TestUpdateTask_Success(t *testing.T) {
	mockDB = &MockDatabase{
		UpdateTaskFunc: func(c *gin.Context, taskID string, task types.Task) types.Task {
			return types.Task{
				TaskID: taskID,
				Title:  "Updated Task",
				Description: "Updated Description",
				Day: 2,
				Completed: true,
			}
		},
	}
	router := setupRouter()

	body := `{"title":"Updated Task","description":"Updated Description","day":2,"completed":true}`
	req, _ := http.NewRequest("PATCH", "/milestone/task/task-123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp types.Task
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Updated Task", resp.Title)
	assert.Equal(t, "Updated Description", resp.Description)
	assert.Equal(t, 2, resp.Day)
	assert.True(t, resp.Completed)
}

func TestUpdateTask_BadRequest(t *testing.T) {
	mockDB = &MockDatabase{}
	router := setupRouter()
	req, _ := http.NewRequest("PATCH", "/milestone/task/task-123", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteTask_Success(t *testing.T) {
	mockDB = &MockDatabase{
		DeleteTaskFunc: func(c *gin.Context, taskID string) error {
			return nil
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("DELETE", "/milestone/task/task-123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Task deleted successfully", resp["message"])
}

func TestDeleteTask_Error(t *testing.T) {
	mockDB = &MockDatabase{
		DeleteTaskFunc: func(c *gin.Context, taskID string) error {
			return errors.New("database error")
		},
	}
	router := setupRouter()
	req, _ := http.NewRequest("DELETE", "/milestone/task/task-123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
