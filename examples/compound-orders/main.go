// Command compound-orders is a small but realistic service that combines several
// MicroJet features in one program, to show how they fit together:
//
//   - host modules + DI .......... gormx, httpx, cache wired declaratively
//   - gormx (SQLite) ............. persistence with the generic Table[T]
//   - cursor pagination .......... GET /orders?pageSize=..&nextPageToken=..
//   - cache ...................... product lookups served from cache
//   - idempotency ................ POST /orders is safe to retry
//   - money ...................... currency-aware totals via minor units
//   - structured errors .......... 404 / 400 with typed categories
//   - request binding/validation . typed body with binding tags
//
// It uses SQLite and an in-memory cache, so it runs offline:
//
//	go run .
//
// Try it:
//
//	curl -s localhost:8080/products/1
//	curl -s -XPOST localhost:8080/orders -H 'Idempotency-Key: k1' -d '{"productID":1,"qty":3}'
//	curl -s -XPOST localhost:8080/orders -H 'Idempotency-Key: k1' -d '{"productID":1,"qty":3}'  # replayed, no new order
//	curl -s localhost:8080/orders?pageSize=2
//	curl -s localhost:8080/orders/1
//	curl -s localhost:8080/products/999   # 404 structured error
package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/cache"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/types/money"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/gormx/sqlite"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/httpx/middleware"
)

// --- domain models ---------------------------------------------------------

// Prices are stored as integer minor units (e.g. cents) — the money type's
// MinorUnits/FromMinorUnits bridge to a human-facing decimal amount.
type Product struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Name       string `gorm:"not null" json:"name"`
	Currency   string `gorm:"not null" json:"currency"`
	PriceMinor int64  `gorm:"not null" json:"priceMinor"`
}

type Order struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ProductID  uint      `gorm:"not null" json:"productID"`
	Qty        int       `gorm:"not null" json:"qty"`
	Currency   string    `gorm:"not null" json:"currency"`
	TotalMinor int64     `gorm:"not null" json:"totalMinor"`
	CreatedAt  time.Time `json:"createdAt"`
}

// createOrderRequest is bound and validated by httpx.Body — the binding tags
// turn a missing/invalid field into a 400 with per-field detail automatically.
type createOrderRequest struct {
	ProductID uint `json:"productID" binding:"required"`
	Qty       int  `json:"qty" binding:"required,min=1"`
}

func main() {
	host.MustNew().
		WithModule(gormx.Module(sqlite.Driver())).
		WithModule(cache.Module()).
		WithModule(httpx.Module()).
		Setup(func(a *host.App) error {
			if err := gormx.Of(a).AutoMigrate(&Product{}, &Order{}); err != nil {
				return err
			}
			return seedProducts(a)
		}).
		Setup(registerRoutes).
		MustRun()
}

func seedProducts(a *host.App) error {
	db := gormx.Of(a)
	var count int64
	db.Model(&Product{}).Count(&count)
	if count > 0 {
		return nil
	}
	return db.Create(&[]Product{
		{Name: "Widget", Currency: "USD", PriceMinor: 1999},
		{Name: "Gadget", Currency: "USD", PriceMinor: 4950},
		{Name: "Sprocket", Currency: "JPY", PriceMinor: 300},
	}).Error
}

func registerRoutes(a *host.App) error {
	r := httpx.Of(a).Router
	db := gormx.Of(a)
	c := cache.Of(a)
	products := gormx.NewTable[Product](db)
	orders := gormx.NewTable[Order](db)

	// GET /products/:id — cache-aside: serve from cache, else load and populate.
	r.GET("/products/:id", func(ctx *gin.Context) {
		id, err := httpx.GetInt64Param(ctx, "id")
		if err != nil {
			ctx.Error(err)
			return
		}
		reqCtx := ctx.Request.Context()
		key := "product:" + ctx.Param("id")

		if p, found, _ := cache.GetJSON[Product](reqCtx, c, key); found {
			ctx.JSON(http.StatusOK, gin.H{"product": p, "cached": true})
			return
		}
		p, err := products.First(reqCtx, "id = ?", id)
		if err != nil {
			ctx.Error(err)
			return
		}
		if p == nil {
			ctx.Error(errorx.ErrNotFound.WithSubject("Product").WithParams("id", id))
			return
		}
		_ = cache.SetJSON(reqCtx, c, key, *p, time.Minute)
		ctx.JSON(http.StatusOK, gin.H{"product": p, "cached": false})
	})

	// POST /orders — idempotent create. Computes the total with the money type
	// and persists it as minor units. A retry with the same Idempotency-Key
	// replays the original response without creating a second order.
	r.POST("/orders",
		middleware.Idempotency(c, middleware.WithIdempotencyTTL(10*time.Minute)),
		func(ctx *gin.Context) {
			body, err := httpx.Body[createOrderRequest](ctx)
			if err != nil {
				ctx.Error(err) // validation failures become a 400 with field detail
				return
			}
			reqCtx := ctx.Request.Context()

			product, err := products.First(reqCtx, "id = ?", body.ProductID)
			if err != nil {
				ctx.Error(err)
				return
			}
			if product == nil {
				ctx.Error(errorx.ErrNotFound.WithSubject("Product").WithParams("id", body.ProductID))
				return
			}

			// Currency-aware total = unit price * quantity, all in minor units.
			unit := money.FromMinorUnits(product.PriceMinor, money.CurrencyCode(product.Currency))
			total := unit.MultiplyInt64(int64(body.Qty))

			order := Order{
				ProductID:  product.ID,
				Qty:        body.Qty,
				Currency:   product.Currency,
				TotalMinor: total.MinorUnits(),
			}
			if err := orders.Create(reqCtx, &order); err != nil {
				ctx.Error(err)
				return
			}
			ctx.JSON(http.StatusCreated, gin.H{
				"order":         order,
				"totalDisplay":  total.Value.String(),
				"totalCurrency": order.Currency,
			})
		})

	// GET /orders — cursor pagination by id (stable, no OFFSET scan).
	r.GET("/orders", func(ctx *gin.Context) {
		req := gormx.NewPageRequest[Order, uint](httpx.PagedRequest(ctx), "id", func(o Order) uint { return o.ID })
		page, err := orders.List(ctx.Request.Context(), req)
		if err != nil {
			ctx.Error(err)
			return
		}
		ctx.JSON(http.StatusOK, page)
	})

	// GET /orders/:id — fetch one, structured 404 when absent.
	r.GET("/orders/:id", func(ctx *gin.Context) {
		id, err := httpx.GetInt64Param(ctx, "id")
		if err != nil {
			ctx.Error(err)
			return
		}
		order, err := orders.First(ctx.Request.Context(), "id = ?", id)
		if err != nil {
			ctx.Error(err)
			return
		}
		if order == nil {
			ctx.Error(errorx.ErrNotFound.WithSubject("Order").WithParams("id", id))
			return
		}
		ctx.JSON(http.StatusOK, order)
	})

	return nil
}
