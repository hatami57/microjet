package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/tenant"
)

const (
	TenantIDQueryParamLower = "tenantid"
	TenantIDHeaderKey       = "X-Tenant-ID"
	TenantContextKey        = "tenant"
	TenantIDContextKey      = "tenantID"
)

func CachedTenant(tenantStore tenant.Store, ttl time.Duration) (gin.HandlerFunc, *tenant.CachedStore) {
	cached := tenant.NewCachedStore(tenantStore, ttl)
	return Tenant(cached), cached
}

func Tenant(tenantStore tenant.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, err := uuid.Parse(extractTenantID(c))
		if err != nil {
			_ = c.Error(core.ErrUnauthorized)
			c.Abort()
			return
		}

		t, err := tenantStore.FindTenant(c, tenantID)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		} else if t == nil {
			_ = c.Error(core.ErrUnauthorized.WithParams("tenantId", tenantID))
			c.Abort()
			return
		} else if !t.AsBase().IsActive {
			_ = c.Error(core.ErrForbidden.WithParams("tenantId", tenantID))
			c.Abort()
			return
		}

		c.Set(TenantContextKey, t)
		c.Set(TenantIDContextKey, t.AsBase().ID)
		c.Next()
	}
}

func extractTenantID(c *gin.Context) string {
	for name, values := range c.Request.URL.Query() {
		if strings.ToLower(name) == TenantIDQueryParamLower && len(values) > 0 {
			return values[0]
		}
	}
	return c.GetHeader(TenantIDHeaderKey)
}

func FindTenantBase(c *gin.Context) *tenant.Base {
	v, exists := c.Get(TenantContextKey)
	if !exists {
		return nil
	}
	t, _ := v.(tenant.Tenant)
	if t == nil {
		return nil
	}
	return t.AsBase()
}

func FindTenant[T any](c *gin.Context) *T {
	v, exists := c.Get(TenantContextKey)
	if !exists {
		return nil
	}
	t, _ := v.(*T)
	return t
}

func FindTenantID(c *gin.Context) (uuid.UUID, error) {
	value, exists := c.Get(TenantIDContextKey)
	if !exists {
		return uuid.Nil, core.ErrNotFound
	}
	id, ok := value.(uuid.UUID)
	if !ok {
		return uuid.Nil, core.ErrInternal.WithMessage("tenant ID in context has unexpected type")
	}
	return id, nil
}
