package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// umwBindTarget is the struct the Bind* helpers populate in these tests. Name
// carries binding:"required" so the validator error path can be exercised.
type umwBindTarget struct {
	Name  string `json:"name" binding:"required"`
	Count int    `json:"count"`
}

// umwNewJSONCtx builds a test context carrying a POST body with the JSON
// content type, which is what the Bind* helpers expect from real handlers.
func umwNewJSONCtx(t *testing.T, body string) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return w, c
}

// umwErrorMessage decodes the error envelope and returns its message field.
func umwErrorMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body ErrorResponseStruct
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body: %s", w.Body.String())
	return body.Message
}

func TestBindJSON_ValidBody(t *testing.T) {
	w, c := umwNewJSONCtx(t, `{"name":"test","count":3}`)

	var target umwBindTarget
	ok := BindJSON(c, &target)

	assert.True(t, ok)
	assert.Equal(t, "test", target.Name)
	assert.Equal(t, 3, target.Count)
	assert.Zero(t, w.Body.Len(), "nothing should be written on success")
}

func TestBindJSON_MalformedBody(t *testing.T) {
	w, c := umwNewJSONCtx(t, `{oops`)

	var target umwBindTarget
	ok := BindJSON(c, &target)

	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.True(t, strings.HasPrefix(umwErrorMessage(t, w), "Invalid request data: "))
	// Note: gin's c.BindJSON (MustBindWith) aborts the context itself on any
	// bind error, so BindJSON leaves the context aborted too — the explicit
	// c.Abort() in BindJSONOrAbort is belt-and-suspenders, not a behavioral
	// difference. Documented here as current behavior.
	assert.True(t, c.IsAborted())
}

func TestBindJSON_MissingRequiredField(t *testing.T) {
	// Valid JSON but the binding:"required" field is absent — the validator
	// error must surface through the same 400 contract as a parse error.
	w, c := umwNewJSONCtx(t, `{"count":1}`)

	var target umwBindTarget
	ok := BindJSON(c, &target)

	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.True(t, strings.HasPrefix(umwErrorMessage(t, w), "Invalid request data: "))
}

func TestBindJSONOrAbort_ValidBody(t *testing.T) {
	w, c := umwNewJSONCtx(t, `{"name":"n"}`)

	var target umwBindTarget
	ok := BindJSONOrAbort(c, &target)

	assert.True(t, ok)
	assert.Equal(t, "n", target.Name)
	assert.False(t, c.IsAborted())
	assert.Zero(t, w.Body.Len())
}

func TestBindJSONOrAbort_MalformedBodyAborts(t *testing.T) {
	w, c := umwNewJSONCtx(t, `{oops`)

	var target umwBindTarget
	ok := BindJSONOrAbort(c, &target)

	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.True(t, c.IsAborted())
	assert.True(t, strings.HasPrefix(umwErrorMessage(t, w), "Invalid request data: "))
}

func TestShouldBindJSON_ValidBody(t *testing.T) {
	w, c := umwNewJSONCtx(t, `{"name":"n","count":9}`)

	var target umwBindTarget
	ok := ShouldBindJSON(c, &target)

	assert.True(t, ok)
	assert.Equal(t, 9, target.Count)
	assert.Zero(t, w.Body.Len())
	assert.False(t, c.IsAborted())
}

func TestShouldBindJSON_MalformedBodyDistinctPrefix(t *testing.T) {
	w, c := umwNewJSONCtx(t, `{oops`)

	var target umwBindTarget
	ok := ShouldBindJSON(c, &target)

	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	// ShouldBindJSON has its own message contract, distinct from the Bind*
	// helpers' "Invalid request data: " prefix.
	assert.True(t, strings.HasPrefix(umwErrorMessage(t, w), "Invalid JSON format: "))
	// ShouldBind never aborts — that's the point of the Should* variant.
	assert.False(t, c.IsAborted())
}

func TestGetParamOrAbort(t *testing.T) {
	t.Run("param present", func(t *testing.T) {
		w, c := umwNewJSONCtx(t, "")
		c.Params = gin.Params{{Key: "id", Value: "abc-123"}}

		value, ok := GetParamOrAbort(c, "id")

		assert.True(t, ok)
		assert.Equal(t, "abc-123", value)
		assert.False(t, c.IsAborted())
		assert.Zero(t, w.Body.Len(), "no response should be written on success")
	})

	t.Run("param absent", func(t *testing.T) {
		w, c := umwNewJSONCtx(t, "")

		value, ok := GetParamOrAbort(c, "id")

		assert.False(t, ok)
		assert.Empty(t, value)
		assert.True(t, c.IsAborted())
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "Missing required parameter: id", umwErrorMessage(t, w))
	})
}
