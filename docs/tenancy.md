# Multi-tenancy

`middleware.Tenant(store)` resolves the tenant on every request from the
`X-Tenant-ID` header or `tenantId` query param, using a `tenant.Store` you
provide. See the [main README](../README.md) for HTTP server setup.

## Cached tenant lookups

To avoid a per-request store hit, wrap any `tenant.Store` in
`tenant.NewCachedStore` — it caches both hits and "not found" results for the
given TTL and exposes `Invalidate(id)` to drop a single entry (and `Clear()` to
flush them all) when a tenant changes:

```go
import (
    "github.com/hatami57/microjet/core/tenant"
    "github.com/hatami57/microjet/httpx/middleware"
)

cached := tenant.NewCachedStore(dbStore, 5*time.Minute)
router.Use(middleware.Tenant(cached))
```

Inside handlers, read the resolved tenant with the typed accessors. `Find*`
returns the zero value when the tenant is absent, while `Get*` returns an
`errorx` error:

```go
t := middleware.FindTenant[*MyTenant](c) // *MyTenant, or nil if absent
t, err := middleware.GetTenant[*MyTenant](c) // err on absence or type mismatch
base := middleware.FindTenantBase(c)     // *tenant.Base, or nil
id, err := middleware.GetTenantID(c)     // uuid.UUID, err if absent
```
