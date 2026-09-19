package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core/tenant"
	"net/http/httptest"
	"testing"
	"time"
)

type contextTenantStore struct {
	t    *testing.T
	want context.Context
}

func (s contextTenantStore) FindTenant(ctx context.Context, id uuid.UUID) (tenant.Tenant, error) {
	if ctx != s.want {
		s.t.Error("store did not receive request context")
	}
	if ctx.Err() != context.Canceled {
		s.t.Errorf("lost cancellation: %v", ctx.Err())
	}
	return &tenant.Base{ID: id, IsActive: true}, nil
}
func TestTenantPassesRequestContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	cancel()
	r := gin.New()
	r.Use(Tenant(contextTenantStore{t, ctx}))
	r.GET("/", func(c *gin.Context) { c.Status(204) })
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	req.Header.Set(TenantIDHeaderKey, uuid.NewString())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("status = %d", w.Code)
	}
}
