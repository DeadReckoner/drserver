package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type ErrorResponseStruct struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code"`
}

type SuccessResponseStruct struct {
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func ErrorResponse(c *gin.Context, statusCode int, message string) {
	response := ErrorResponseStruct{
		Error:   http.StatusText(statusCode),
		Message: message,
		Code:    statusCode,
	}
	c.JSON(statusCode, response)
}

func SuccessResponse(c *gin.Context, statusCode int, message string, data interface{}) {
	response := SuccessResponseStruct{
		Message: message,
		Data:    data,
	}
	c.JSON(statusCode, response)
}

func DataResponse(c *gin.Context, statusCode int, data interface{}) {
	c.JSON(statusCode, data)
}

func MessageResponse(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{"message": message})
}

func ValidationErrorResponse(c *gin.Context, err error) {
	ErrorResponse(c, http.StatusBadRequest, "Invalid request data: "+err.Error())
}

func NotFoundResponse(c *gin.Context, resource string) {
	ErrorResponse(c, http.StatusNotFound, resource+" not found")
}

func InternalErrorResponse(c *gin.Context, err error) {
	ErrorResponse(c, http.StatusInternalServerError, "Internal server error: "+err.Error())
}

func ConflictResponse(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusConflict, message)
}

func BadRequestResponse(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusBadRequest, message)
}
