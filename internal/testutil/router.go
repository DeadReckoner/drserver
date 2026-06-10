package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"dr/server/api"
	"dr/server/api/handlers"
	"dr/server/api/routes"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// NewTestRouter builds the full API router wired against the given *gorm.DB
// and the fake auth middleware. Tests authenticate by setting the
// TestUserIDHeader on each request.
func NewTestRouter(db *gorm.DB) *gin.Engine {
	return api.NewRouterWithOptions(db, routes.Options{
		AuthMiddleware: FakeAuthMiddleware(),
	})
}

// NewTestRouterAs is like NewTestRouter but bakes in a single user; the
// TestUserIDHeader on the request is ignored and every request is treated as
// the given user. Convenient for single-user tests.
func NewTestRouterAs(db *gorm.DB, userID string) *gin.Engine {
	return api.NewRouterWithOptions(db, routes.Options{
		AuthMiddleware: FakeAuthMiddlewareAs(userID),
	})
}

// NewTestRouterFromHandlers builds a router from a pre-built *handlers.Handlers
// struct, wired with the fake auth middleware. Use when individual handlers
// have been constructed with fake services. Other handler fields can be left
// nil so long as the test doesn't hit their routes (method values on nil
// receivers are safe to register; only invocation panics).
func NewTestRouterFromHandlers(h *handlers.Handlers) *gin.Engine {
	return api.NewRouterFromHandlers(h, routes.Options{
		AuthMiddleware: FakeAuthMiddleware(),
	})
}

// Do runs an HTTP request against the router and returns the response
// recorder. Marshals body to JSON when non-nil and stamps the user header if
// userID is non-empty.
func Do(t *testing.T, router http.Handler, method, path, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID != "" {
		req.Header.Set(TestUserIDHeader, userID)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// DecodeJSON unmarshals the response body into out, failing the test on error.
func DecodeJSON(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("DecodeJSON: %v\nbody: %s", err, w.Body.String())
	}
}
