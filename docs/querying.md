# Querying with `gormx`

`gormx.Table[T]` is a thin, chainable layer over GORM. The scopes shown here —
`Where`/`WhereIf`/`Order`/`Select`/`Group` — compose with every terminal method
(`First`, `Get`, `List`, `Count`, aggregates, projections), so the same filter
builds up once and is reused across reads. See the
[main README](../README.md) for setup and the `[database]` configuration block.

## Pagination

```go
import (
    "github.com/hatami57/microjet/httpx"
    "github.com/hatami57/microjet/gormx"
)

// Cursor-based pagination by ID. Filter by chaining Where on the table — the same
// Where/WhereIf/Order used by First, Count, and ListAll.
req := gormx.NewPageRequest[User, uint](httpx.PagedRequest(c), "id", func(u User) uint { return u.ID })

result, _ := userTable.Where("name ILIKE ?", "%john%").List(ctx, req)
for _, user := range result.Items {
    // ...
}
// result.NextPageToken is base64-encoded cursor for the next page

// Offset (page-number) pagination: set Page on the request, or call ForceOffset()
// to default a missing "page" query param to page 1 instead of cursor mode — for
// SQL-backed endpoints that always want page jumps with a computed TotalCount.
req = gormx.NewPageRequest[User, uint](httpx.PagedRequest(c).ForceOffset(), "id", func(u User) uint { return u.ID })
```

## Aggregates & projections

```go
// Aggregates scan a scalar into a dest pointer and compose with the chainable scopes.
// Sum/Avg/Max/Min cover numeric columns (empty set → 0 via COALESCE); Aggregate takes
// any raw SQL expression for the advanced cases.
var total uint64
orders.Where("is_confirmed = ?", true).Sum(ctx, "amount", &total)

var spread int
orders.Aggregate(ctx, "MAX(amount) - MIN(amount)", &spread, "is_confirmed = ?", true)

// Project maps rows into a result type instead of the entity — pair it with Select to
// pick or compute columns. dest is a *[]Result (or *[]map[string]any for ad-hoc shapes).
type tally struct {
    CampaignID uint
    Total      uint64
}
var rows []tally
orders.Select("campaign_id, COALESCE(SUM(amount), 0) AS total").
    Where("is_confirmed = ?", true).
    Group("campaign_id").
    Project(ctx, &rows)

// ProjectFirst is the single-row form; it reports whether a row was found.
var one tally
found, _ := orders.Select("campaign_id, amount AS total").Order("amount DESC").ProjectFirst(ctx, &one)
```

## Atomic & guarded updates

```go
// UpdateMap returns rows affected. Combine a gormx.Expr value (computed in-place by the
// database, no read-then-write) with a guard in the WHERE clause for a lock-free atomic
// compare-and-swap — a zero return means the guard rejected the update, not a failure.
// Portable across engines, SQLite included.
n, _ := campaigns.UpdateMap(ctx,
    map[string]any{"used": gormx.Expr("used + 1")},
    "id = ? AND used < capacity", id)
if n == 0 {
    // missing or at capacity
}

// For read-modify-write that can't be expressed as one statement, take a row lock inside
// a transaction. LockForUpdate emits SELECT … FOR UPDATE on Postgres/MySQL; on SQLite the
// clause is dropped (write safety comes from transaction-level serialization there).
repo.RunTx(ctx, func(ctx context.Context) error {
    acct, err := accounts.LockForUpdate().Get(ctx, "id = ?", id)
    if err != nil {
        return err
    }
    acct.Balance = recompute(acct.Balance)
    _, err = accounts.Update(ctx, acct)
    return err
})

// UpdateColumn(s) write raw columns without hooks or timestamp auto-updates; Raw/Exec are
// the escape hatch for SQL the builder can't express (both honor the ctx transaction).
var report []SalesRow
campaigns.Raw(ctx, &report, "SELECT region, SUM(total) AS total FROM orders GROUP BY region")
```
