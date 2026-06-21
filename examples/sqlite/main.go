// Command sqlite is a small CRUD service backed by SQLite, showing
// gormx.Module(sqlite.Driver()) (the pure-Go, no-cgo driver), AutoMigrate, and
// structured error handling.
//
// Unlike the http-postgres example it needs no external database server: the
// config points at a local file (or ":memory:"). Just run:
//
//	go run .
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/gormx/sqlite"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
)

type Note struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Body string `gorm:"not null" json:"body"`
}

func main() {
	app := host.MustNew()

	app.WithModule(gormx.Module(sqlite.Driver())).
		WithModule(httpx.Module()).
		Setup(func(a *host.App) error {
			return gormx.Of(a).AutoMigrate(&Note{})
		}).
		Setup(func(a *host.App) error {
			registerRoutes(a)
			return nil
		}).
		MustRun()
}

func registerRoutes(a *host.App) {
	r := httpx.Of(a).Router
	db := gormx.Of(a)

	// List: GET /notes
	r.GET("/notes", func(c *gin.Context) {
		var notes []Note
		if err := db.WithContext(c.Request.Context()).Find(&notes).Error; err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, notes)
	})

	// Fetch one: GET /notes/42
	r.GET("/notes/:id", func(c *gin.Context) {
		id, err := httpx.GetInt64Param(c, "id")
		if err != nil {
			c.Error(err)
			return
		}
		var note Note
		err = db.WithContext(c.Request.Context()).First(&note, id).Error
		if err != nil {
			c.Error(errorx.ErrNotFound.WithSubject("Note"))
			return
		}
		c.JSON(http.StatusOK, note)
	})

	// Create: POST /notes {"body":"..."}
	r.POST("/notes", func(c *gin.Context) {
		body, err := httpx.Body[Note](c)
		if err != nil {
			c.Error(err)
			return
		}
		if err := db.WithContext(c.Request.Context()).Create(&body).Error; err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusCreated, body)
	})
}
