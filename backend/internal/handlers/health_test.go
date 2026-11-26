package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/backend/internal/db/dbtest"
)

func TestHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		closeDB   bool
		wantCode  int
		wantState string
	}{
		{"open database", false, http.StatusOK, `{"status":"ok"}`},
		{"closed database", true, http.StatusServiceUnavailable, `{"status":"unavailable"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gdb, err := dbtest.SqliteForTest()
			require.NoError(t, err)
			if tt.closeDB {
				sqlDB, err := gdb.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			}

			r := gin.New()
			r.GET("/healthz", Healthz(gdb))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			assert.Equal(t, tt.wantCode, w.Code)
			assert.JSONEq(t, tt.wantState, w.Body.String())
		})
	}
}
