package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
)

// Recovery turns a panic into a 500 without exposing the panic value; the value and the stack go to the log.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				logger.Of(c).Error(panicError(rec),
					zap.String("behavior", "[PANIC RECOVERED]"),
					zap.String("stack", string(stack)),
				)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":  http.StatusInternalServerError,
					"error": "internal server error",
				})
			}
		}()

		c.Next()
	}
}

func panicError(rec any) error {
	if err, ok := rec.(error); ok {
		return err
	}
	return errors.New(fmt.Sprint(rec))
}

// ErrorHandler answers with the status of the errors attached to the context, without their messages
// (the Logger middleware already logs them).
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		status := c.Writer.Status()
		if status == http.StatusOK {
			status = http.StatusInternalServerError
		}

		message := http.StatusText(status)
		if status >= http.StatusInternalServerError {
			message = "internal server error"
		}

		c.JSON(status, gin.H{
			"code":  status,
			"error": message,
		})
	}
}
