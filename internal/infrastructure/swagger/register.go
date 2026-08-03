package swagger

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
)

func Register(router gin.IRoutes, enabled bool, path string) {
	if !enabled {
		return
	}
	router.GET(path, func(c *gin.Context) {
		suffix := strings.TrimSpace(c.Param("any"))
		switch suffix {
		case "", "/":
			c.Redirect(http.StatusTemporaryRedirect, strings.TrimSuffix(c.Request.URL.Path, "/")+"/openapi.json")
		case "/openapi.json":
			serveSpec(c, "application/json; charset=utf-8", contractsopenapi.JSON(), contractsopenapi.JSONSHA256)
		case "/openapi.yaml":
			serveSpec(c, "application/yaml; charset=utf-8", contractsopenapi.YAML(), contractsopenapi.YAMLSHA256)
		default:
			c.Status(http.StatusNotFound)
		}
	})
}

func serveSpec(c *gin.Context, contentType string, body []byte, sha256 string) {
	c.Header("ETag", `"`+sha256+`"`)
	c.Header("X-Soha-Contracts-Version", contractsopenapi.Version)
	c.Header("X-Soha-Contracts-SHA256", sha256)
	c.Data(http.StatusOK, contentType, body)
}
