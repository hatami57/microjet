package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/hatami57/microjet/core"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	TenantIDQueryParamLower = "tenantid"
	TenantIDHeaderKey       = "X-Tenant-ID"
	TenantContextKey        = "tenant"
	TenantIDContextKey      = "tenantID"
)

// TenantBase is the minimal tenant record needed by the middleware.
// Embed or implement this in your own tenant model.
type TenantBase struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TenantStore is the interface the middleware uses to look up tenants.
type TenantStore interface {
	Find(ctx context.Context, id uuid.UUID) (*TenantBase, error)
}

func Tenant(tenantStore TenantStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantRecord, err := extractValidTenant(c, tenantStore)
		if err != nil {
			c.Error(err)
			c.Abort()
			return
		}
		c.Set(TenantContextKey, tenantRecord)
		c.Set(TenantIDContextKey, tenantRecord.ID)
		c.Next()
	}
}

func extractValidTenant(c *gin.Context, tenantStore TenantStore) (*TenantBase, error) {
	tenantID, err := uuid.Parse(extractTenantID(c))
	if err != nil {
		return nil, core.ErrUnauthorized
	}

	tenantRecord, err := tenantStore.Find(c, tenantID)
	if err != nil {
		return nil, err
	} else if tenantRecord == nil {
		return nil, core.ErrUnauthorized
	} else if !tenantRecord.IsActive {
		return nil, core.ErrForbidden
	}

	return tenantRecord, nil
}

func extractTenantID(c *gin.Context) string {
	for name, values := range c.Request.URL.Query() {
		if strings.ToLower(name) == TenantIDQueryParamLower && len(values) > 0 {
			return values[0]
		}
	}
	return c.GetHeader(TenantIDHeaderKey)
}

func FindTenant(c *gin.Context) *TenantBase {
	tenantRecord, exists := c.Get(TenantContextKey)
	if !exists {
		return nil
	}
	return tenantRecord.(*TenantBase)
}

func FindTenantID(c *gin.Context) (uuid.UUID, error) {
	value, exists := c.Get(TenantIDContextKey)
	if !exists {
		return uuid.Nil, core.ErrNotFound
	}
	return value.(uuid.UUID), nil
}
