package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/tenant"
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
			_ = c.Error(errorx.ErrUnauthorized)
			c.Abort()
			return
		}

		t, err := tenantStore.FindTenant(c, tenantID)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		} else if t == nil {
			_ = c.Error(errorx.ErrUnauthorized.WithParams("tenantId", tenantID))
			c.Abort()
			return
		} else if !t.AsBase().IsActive {
			_ = c.Error(errorx.ErrForbidden.WithParams("tenantId", tenantID))
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

// FindTenantBase returns the *tenant.Base for the tenant stored by the Tenant
// middleware, or nil if none is present in the context.
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

// FindTenant returns the tenant of type T stored by the Tenant middleware. It
// returns the zero value of T (nil for pointer types) when no tenant is present
// or the stored tenant is not a T. Use GetTenant when the absence or a type
// mismatch should surface as an error.
func FindTenant[T tenant.Tenant](c *gin.Context) T {
	v, exists := c.Get(TenantContextKey)
	if !exists {
		var zero T
		return zero
	}
	t, _ := v.(T)
	return t
}

// GetTenant returns the tenant of type T stored by the Tenant middleware. It
// returns errorx.ErrNotFound when no tenant is present and errorx.ErrInternal
// when the stored tenant is not a T. Use FindTenant when a missing tenant
// should yield the zero value instead of an error.
func GetTenant[T tenant.Tenant](c *gin.Context) (T, error) {
	var zero T
	v, exists := c.Get(TenantContextKey)
	if !exists {
		return zero, errorx.ErrNotFound
	}
	t, ok := v.(T)
	if !ok {
		return zero, errorx.ErrInternal.WithMessage("tenant in context has unexpected type")
	}
	return t, nil
}

// GetTenantID returns the ID of the tenant stored by the Tenant middleware. It
// returns errorx.ErrNotFound when no tenant ID is present and errorx.ErrInternal
// when the stored value is not a uuid.UUID.
func GetTenantID(c *gin.Context) (uuid.UUID, error) {
	value, exists := c.Get(TenantIDContextKey)
	if !exists {
		return uuid.Nil, errorx.ErrNotFound
	}
	id, ok := value.(uuid.UUID)
	if !ok {
		return uuid.Nil, errorx.ErrInternal.WithMessage("tenant ID in context has unexpected type")
	}
	return id, nil
}
