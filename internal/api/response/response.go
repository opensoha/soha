package response

import "github.com/gin-gonic/gin"

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func JSON(c *gin.Context, status int, payload any) {
	c.JSON(status, payload)
}

func Item(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"data": data})
}

func Items(c *gin.Context, status int, items any) {
	c.JSON(status, gin.H{"items": items})
}

func Error(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: c.GetString("request_id"),
		},
	})
}
