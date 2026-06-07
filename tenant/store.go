package tenant

import (
	"context"

	"github.com/google/uuid"
)

type Store interface {
	FindTenant(ctx context.Context, id uuid.UUID) (Tenant, error)
}
