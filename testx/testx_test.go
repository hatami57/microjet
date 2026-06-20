package testx

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/cache"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/messaging"
)

func TestNewAppDefaults(t *testing.T) {
	app := NewApp(t)
	if gormx.Of(app.App) == nil {
		t.Error("expected an in-memory database")
	}
	if cache.Of(app.App) == nil {
		t.Error("expected an in-memory cache")
	}
	if got := app.Clock.Now(); !got.Equal(DefaultFixedTime) {
		t.Errorf("clock = %v, want fixed %v", got, DefaultFixedTime)
	}
}

func TestNewAppWithoutDatabase(t *testing.T) {
	app := NewApp(t, WithoutDatabase())
	if gormx.Of(app.App) != nil {
		t.Error("expected no database when WithoutDatabase is set")
	}
}

func TestNewDBUsable(t *testing.T) {
	db := NewDB(t)
	type widget struct {
		ID   uint `gorm:"primarykey"`
		Name string
	}
	if err := db.AutoMigrate(&widget{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&widget{Name: "a"}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var n int64
	db.Model(&widget{}).Count(&n)
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}

func TestBrokerPublishSubscribe(t *testing.T) {
	b := NewBroker()
	ctx := context.Background()
	var got string
	if _, err := b.Subscribe(ctx, "greet", messaging.HandleJSON(func(_ context.Context, s string) error {
		got = s
		return nil
	})); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	msg, err := messaging.NewJSONMessage("greet", "hello")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	if err := b.Publish(ctx, msg); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got != "hello" {
		t.Errorf("handler got %q, want hello", got)
	}
	if len(b.Published()) != 1 {
		t.Errorf("published count = %d, want 1", len(b.Published()))
	}
}

func TestBrokerRequestRespond(t *testing.T) {
	b := NewBroker()
	if _, err := b.Respond("add", func(_ context.Context, req *messaging.Request) (*messaging.Response, error) {
		return &messaging.Response{Data: append([]byte("re:"), req.Data...)}, nil
	}); err != nil {
		t.Fatalf("respond: %v", err)
	}
	resp, err := b.Request(context.Background(), messaging.Request{Subject: "add", Data: []byte("hi")})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if string(resp.Data) != "re:hi" {
		t.Errorf("response = %q, want re:hi", resp.Data)
	}

	if _, err := b.Request(context.Background(), messaging.Request{Subject: "missing"}); err == nil {
		t.Error("expected timeout error for an unhandled subject")
	}
}

func TestBrokerUnsubscribe(t *testing.T) {
	b := NewBroker()
	ctx := context.Background()
	calls := 0
	sub, _ := b.Subscribe(ctx, "x", func(context.Context, *messaging.Message) error { calls++; return nil })
	_ = b.Publish(ctx, messaging.Message{Subject: "x"})
	if err := sub.Unsubscribe(); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	_ = b.Publish(ctx, messaging.Message{Subject: "x"})
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1 (no delivery after unsubscribe)", calls)
	}
}

func TestHTTPHelpersAgainstApp(t *testing.T) {
	app := NewApp(t, WithHTTPServer(), WithSetup(func(a *host.App) error {
		httpx.Of(a).Router.POST("/echo", func(c *gin.Context) {
			var body map[string]string
			_ = c.ShouldBindJSON(&body)
			c.JSON(http.StatusCreated, gin.H{"echo": body["msg"]})
		})
		return nil
	}))

	w := Request(t, httpx.Of(app.App).Router, http.MethodPost, "/echo", map[string]string{"msg": "hi"})
	AssertStatus(t, w, http.StatusCreated)

	var out struct {
		Echo string `json:"echo"`
	}
	DecodeJSON(t, w, &out)
	if out.Echo != "hi" {
		t.Errorf("echo = %q, want hi", out.Echo)
	}
}

func TestAppWithBroker(t *testing.T) {
	b := NewBroker()
	app := NewApp(t, WithBroker(b))
	if messaging.Of(app.App) == nil {
		t.Fatal("expected messaging client to be wired")
	}
	if app.Broker != b {
		t.Error("App.Broker should expose the injected broker")
	}
	_ = messaging.Of(app.App).Publish(context.Background(), messaging.Message{Subject: "evt", Data: []byte("1")})
	if len(b.Published()) != 1 {
		t.Errorf("published = %d, want 1", len(b.Published()))
	}
}

// Ensure the default fixed time is stable across the package.
func TestFixedTimeStable(t *testing.T) {
	if DefaultFixedTime.Location() != time.UTC {
		t.Error("DefaultFixedTime should be UTC")
	}
}
