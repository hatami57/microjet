// Command http-postgres is a small CRUD service backed by PostgreSQL, showing
// the fluent host builder, the generic gormx.Table, cursor pagination, and
// structured error handling.
//
// Point it at a database via config.toml (see the file in this directory) or
// environment overrides like APP_DATABASE_HOST=localhost, then run:
//
//	go run .
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/gormx/postgres"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
)

type User struct {
	ID    uint   `gorm:"primaryKey" json:"id"`
	Name  string `gorm:"not null" json:"name"`
	Email string `gorm:"uniqueIndex;not null" json:"email"`
}

func main() {
	app := host.MustNew()

	app.WithDatabase(postgres.Driver()).
		Setup(func(a *host.App) error {
			return a.DB().AutoMigrate(&User{})
		}).
		WithHTTPServer(func(a *host.App) error {
			registerRoutes(a, gormx.NewTable[User](a.DB()))
			return nil
		}).
		MustRun()
}

func registerRoutes(a *host.App, users *gormx.Table[User]) {
	r := a.HTTPServer.Router

	// List with cursor pagination: GET /users?pageSize=20&nextPageToken=...
	r.GET("/users", func(c *gin.Context) {
		req := gormx.NewPageRequest[User, uint](httpx.PagedRequest(c), "id", func(u User) uint { return u.ID })
		page, err := users.List(c.Request.Context(), req)
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, page)
	})

	// Fetch one: GET /users/42
	r.GET("/users/:id", func(c *gin.Context) {
		id, err := httpx.GetInt64Param(c, "id")
		if err != nil {
			c.Error(err)
			return
		}
		user, err := users.Find(c.Request.Context(), "id = ?", id)
		if err != nil {
			c.Error(err)
			return
		}
		if user == nil {
			c.Error(core.ErrNotFound.WithSubject("User"))
			return
		}
		c.JSON(http.StatusOK, user)
	})

	// Create: POST /users {"name":"...","email":"..."}
	r.POST("/users", func(c *gin.Context) {
		body, err := httpx.Body[User](c)
		if err != nil {
			c.Error(err)
			return
		}
		if err := users.Create(c.Request.Context(), &body); err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusCreated, body)
	})
}
