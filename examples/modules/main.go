// Command modules demonstrates microjet's composable Module system: a unit of
// functionality that installs its own services, routes, and child modules,
// recursively. Here BillingModule imports UsersModule, which imports
// EmailModule; ReportsModule also imports EmailModule, so the "diamond" is
// deduplicated and the email sender is installed exactly once.
//
// The dependency tree:
//
//	AppModule
//	├── BillingModule
//	│   └── UsersModule
//	│       └── EmailModule        (leaf: provides the email sender)
//	└── ReportsModule
//	    └── EmailModule            (already installed — deduped)
//
// Try it:
//
//	go run .
//	curl localhost:8080/users/42          # resolves repo + email via DI
//	curl localhost:8080/billing/42/invoice
//	curl localhost:8080/reports/daily
//	curl localhost:8080/readyz            # every module's services report readiness
//
// Watch the startup logs (debug) to see the module tree register top-down and
// the services initialize once each.
package main

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
)

func main() {
	// AppModule is the composition root. Installing it pulls in the whole tree;
	// the host then drives config → init → start → close for every service any
	// module provided, regardless of how deeply nested.
	host.MustNew().
		WithModule(httpx.Module()).
		WithModule(AppModule{}).
		MustRun()
}

// AppModule is the top-level module wiring the application's feature modules.
type AppModule struct{}

func (AppModule) Register(app *host.App) error {
	app.WithModules(
		BillingModule{},
		ReportsModule{},
	)
	return app.Err()
}

// --- EmailModule: a leaf module providing a single managed service ----------

// EmailModule provides the shared email sender. Several feature modules import
// it; dedup by module type guarantees the sender is provided only once.
type EmailModule struct{}

func (EmailModule) Register(app *host.App) error {
	host.ProvideService(app, &EmailSender{})
	return nil
}

// EmailSender implements host.ServiceIniter, so the host initializes it during
// the init phase and it shows up in the module tree's lifecycle automatically.
type EmailSender struct {
	sent atomic.Int64
}

func (s *EmailSender) Init(app *host.App) error {
	app.Logger.Info("email sender ready")
	return nil
}

func (s *EmailSender) Send(to, body string) {
	s.sent.Add(1)
}

// Healthy makes the sender participate in the /readyz probe.
func (s *EmailSender) Healthy(ctx context.Context) error { return nil }

// --- UsersModule: imports EmailModule, owns repo + service + routes ---------

type UsersModule struct{}

func (UsersModule) Register(app *host.App) error {
	app.WithModule(EmailModule{}) // child module

	host.ProvideService(app, &UserRepo{})
	host.ProvideService(app, &UserService{})

	// Routes are queued via Setup; they run after the init phase, so the server
	// exists and every service this module needs is connected.
	return app.Setup(func(a *host.App) error {
		svc := host.MustResolveService[*UserService](a)
		httpx.Of(a).Router.GET("/users/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"user": svc.Describe(c.Param("id"))})
		})
		return nil
	}).Err()
}

type UserRepo struct{}

func (r *UserRepo) Find(id string) string { return "user-" + id }

// UserService follows the "Register provides, Init wires" rule: it is provided
// in Register with zero dependencies, then resolves them in Init — by which
// point every module (siblings and children) has registered.
type UserService struct {
	repo  *UserRepo
	email *EmailSender
}

func (s *UserService) Init(app *host.App) error {
	s.repo = host.MustResolveService[*UserRepo](app)
	s.email = host.MustResolveService[*EmailSender](app)
	return nil
}

func (s *UserService) Describe(id string) string {
	s.email.Send(id, "welcome")
	return s.repo.Find(id)
}

// --- BillingModule: imports UsersModule, adds its own service + routes ------

type BillingModule struct{}

func (BillingModule) Register(app *host.App) error {
	app.WithModule(UsersModule{}) // transitively brings in EmailModule

	host.ProvideService(app, &BillingService{})

	return app.Setup(func(a *host.App) error {
		svc := host.MustResolveService[*BillingService](a)
		httpx.Of(a).Router.GET("/billing/:id/invoice", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"invoice": svc.Invoice(c.Param("id"))})
		})
		return nil
	}).Err()
}

type BillingService struct {
	users *UserService
}

func (s *BillingService) Init(app *host.App) error {
	s.users = host.MustResolveService[*UserService](app)
	return nil
}

func (s *BillingService) Invoice(id string) string {
	return fmt.Sprintf("invoice for %s", s.users.Describe(id))
}

// --- ReportsModule: also imports EmailModule (diamond) ----------------------

type ReportsModule struct{}

func (ReportsModule) Register(app *host.App) error {
	app.WithModule(EmailModule{}) // already installed by the Users path — deduped

	return app.Setup(func(a *host.App) error {
		email := host.MustResolveService[*EmailSender](a)
		httpx.Of(a).Router.GET("/reports/daily", func(c *gin.Context) {
			email.Send("ops@example.com", "daily report")
			c.JSON(http.StatusOK, gin.H{"report": "sent", "total_emails": email.sent.Load()})
		})
		return nil
	}).Err()
}
