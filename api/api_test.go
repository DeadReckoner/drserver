package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHealthEndpoint(t *testing.T) {
	router := NewRouter()
	
	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, "dr-server", response["service"])
}

func TestHealthzEndpoint(t *testing.T) {
	router := NewRouter()
	
	req, _ := http.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "ok", response["status"])
}

func TestUUIDValidationMiddleware(t *testing.T) {
	router := gin.New()
	router.GET("/test/:id", utils.ValidateUUID("id"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	tests := []struct {
		name           string
		id             string
		expectedStatus int
	}{
		{
			name:           "Valid UUID",
			id:             "550e8400-e29b-41d4-a716-446655440000",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid UUID",
			id:             "invalid-uuid",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Empty UUID",
			id:             "",
			expectedStatus: http.StatusNotFound, // Gin returns 404 for empty param routes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/test/"+tt.id, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestBindingUtilities(t *testing.T) {
	router := gin.New()
	router.POST("/test", func(c *gin.Context) {
		var data map[string]interface{}
		if !utils.BindJSON(c, &data) {
			return
		}
		c.JSON(http.StatusOK, data)
	})

	t.Run("Valid JSON", func(t *testing.T) {
		body := `{"name":"test","value":123}`
		req, _ := http.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		body := `{invalid json}`
		req, _ := http.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestResponseUtilities(t *testing.T) {
	router := gin.New()
	
	router.GET("/error", func(c *gin.Context) {
		utils.ErrorResponse(c, http.StatusNotFound, "Resource not found")
	})
	
	router.GET("/success", func(c *gin.Context) {
		utils.SuccessResponse(c, http.StatusOK, "Operation successful", gin.H{"id": 123})
	})
	
	router.GET("/data", func(c *gin.Context) {
		utils.DataResponse(c, http.StatusOK, gin.H{"items": []string{"a", "b", "c"}})
	})

	t.Run("Error Response", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/error", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusNotFound, w.Code)
		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "Resource not found", response["message"])
		assert.Equal(t, float64(404), response["code"])
	})

	t.Run("Success Response", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/success", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "Operation successful", response["message"])
		assert.NotNil(t, response["data"])
	})

	t.Run("Data Response", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/data", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.NotNil(t, response["items"])
	})
}