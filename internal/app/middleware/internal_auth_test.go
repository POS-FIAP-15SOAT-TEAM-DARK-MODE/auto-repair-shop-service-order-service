package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestInternalAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		expected string
		header   string
		wantCode int
	}{
		{"matching token", "s3cret", "s3cret", http.StatusOK},
		{"wrong token", "s3cret", "nope", http.StatusUnauthorized},
		{"missing header", "s3cret", "", http.StatusUnauthorized},
		{"unconfigured token rejects everything", "", "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/internal/x", InternalAuth(tt.expected), func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/internal/x", nil)
			if tt.header != "" {
				req.Header.Set("X-Internal-Token", tt.header)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode == http.StatusUnauthorized {
				assert.JSONEq(t, `{"code":401,"errors":["unauthorized"]}`, rec.Body.String())
			}
		})
	}
}
