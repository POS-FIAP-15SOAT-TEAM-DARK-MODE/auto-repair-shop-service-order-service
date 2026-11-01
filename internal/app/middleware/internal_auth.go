package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

const internalTokenHeader = "X-Internal-Token"

// InternalAuth guards /internal/* routes with a shared token between services.
// An empty expected token rejects every request (fail closed).
func InternalAuth(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader(internalTokenHeader)
		if expected == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":   http.StatusUnauthorized,
				"errors": []string{"unauthorized"},
			})
			return
		}
		c.Next()
	}
}
