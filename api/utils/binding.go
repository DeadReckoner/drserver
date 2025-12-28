package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func BindJSON(c *gin.Context, obj interface{}) bool {
	if err := c.BindJSON(obj); err != nil {
		ValidationErrorResponse(c, err)
		return false
	}
	return true
}

func BindJSONOrAbort(c *gin.Context, obj interface{}) bool {
	if err := c.BindJSON(obj); err != nil {
		ValidationErrorResponse(c, err)
		c.Abort()
		return false
	}
	return true
}

func ShouldBindJSON(c *gin.Context, obj interface{}) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		ErrorResponse(c, http.StatusBadRequest, "Invalid JSON format: "+err.Error())
		return false
	}
	return true
}

func GetParamOrAbort(c *gin.Context, paramName string) (string, bool) {
	param := c.Param(paramName)
	if param == "" {
		ErrorResponse(c, http.StatusBadRequest, "Missing required parameter: "+paramName)
		c.Abort()
		return "", false
	}
	return param, true
}
