package response

import (
	"github.com/gin-gonic/gin"
	contractsresponse "github.com/opensoha/soha-contracts/apiresponse"
)

type ErrorBody = contractsresponse.ErrorBody

func JSON(c *gin.Context, status int, payload any) {
	c.JSON(status, payload)
}

func Item(c *gin.Context, status int, data any) {
	c.JSON(status, contractsresponse.Item(data))
}

func Items(c *gin.Context, status int, items any) {
	c.JSON(status, contractsresponse.Items(items))
}

func Error(c *gin.Context, status int, code, message string) {
	c.JSON(status, contractsresponse.Error(code, message, c.GetString("request_id")))
}
