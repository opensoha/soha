package access

import (
	"errors"

	"github.com/gin-gonic/gin"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func writeError(c *gin.Context, err error) {
	if err == nil {
		err = errors.New("handler returned a nil error")
	}
	_ = c.Error(err)
	apiresponse.Error(c, apierrors.StatusCode(err), apierrors.Code(err), apierrors.Message(err))
}
