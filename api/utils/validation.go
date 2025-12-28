package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ValidateUUID(paramName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		paramValue := c.Param(paramName)

		if _, err := uuid.Parse(paramValue); err != nil {
			ErrorResponse(c, http.StatusBadRequest, "Invalid "+paramName+": must be a valid UUID")
			c.Abort()
			return
		}

		c.Next()
	}
}

func ValidateMultipleUUIDs(paramNames ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, paramName := range paramNames {
			paramValue := c.Param(paramName)
			if paramValue == "" {
				ErrorResponse(c, http.StatusBadRequest, "Missing required parameter: "+paramName)
				c.Abort()
				return
			}

			if _, err := uuid.Parse(paramValue); err != nil {
				ErrorResponse(c, http.StatusBadRequest, "Invalid "+paramName+": must be a valid UUID")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
