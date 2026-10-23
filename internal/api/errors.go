package api

import "github.com/gin-gonic/gin"

type apiError struct {
	Error errorBody `json:"error"`
}
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func abort(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, apiError{Error: errorBody{Code: code, Message: message}})
}
func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, apiError{Error: errorBody{Code: code, Message: message}})
}
