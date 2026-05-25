package postgres

import (
	"testing"
	"time"

	"github.com/hatami57/microjet/types"
)

type pgUser struct {
	ID        int
	CreatedAt time.Time
	Name      string
}

func TestByID_NoTokenMeansFirstPage(t *testing.T) {
	r := NewListRequestByID[pgUser](&types.PagedResultRequest{PageSize: 10})
	where, args, err := r.CurrentPageData()
	if err != nil {
		t.Fatalf("CurrentPageData: %v", err)
	}
	if where != nil || args != nil {
		t.Fatalf("expected no cursor on first page, got where=%v args=%v", where, args)
	}
	if r.OrderBy() != "id ASC" || r.PageSize() != 10 {
		t.Errorf("OrderBy=%q PageSize=%d", r.OrderBy(), r.PageSize())
	}
}

func TestByID_CreateThenConsumeToken(t *testing.T) {
	r := NewListRequestByID[pgUser](&types.PagedResultRequest{PageSize: 2})
	token, err := r.CreateNextPageToken([]pgUser{{ID: 1}, {ID: 2}})
	if err != nil {
		t.Fatalf("CreateNextPageToken: %v", err)
	}
	if token == nil {
		t.Fatal("expected a token for a full page")
	}

	next := NewListRequestByID[pgUser](&types.PagedResultRequest{PageSize: 2, NextPageToken: token})
	where, args, err := next.CurrentPageData()
	if err != nil {
		t.Fatalf("CurrentPageData: %v", err)
	}
	if where == nil || *where != "id > ?" {
		t.Fatalf("where = %v, want 'id > ?'", where)
	}
	// JSON round-trips numbers as float64.
	if len(args) != 1 || args[0].(float64) != 2 {
		t.Fatalf("args = %v, want [2]", args)
	}
}

func TestByID_EmptyItemsYieldsNoToken(t *testing.T) {
	r := NewListRequestByID[pgUser](&types.PagedResultRequest{PageSize: 10})
	token, err := r.CreateNextPageToken(nil)
	if err != nil || token != nil {
		t.Fatalf("expected nil token for empty page, got %v, %v", token, err)
	}
}

func TestByCreatedAt_CreateThenConsumeToken(t *testing.T) {
	ts := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	r := NewListRequestByCreatedAt[pgUser](&types.PagedResultRequest{PageSize: 1})
	token, err := r.CreateNextPageToken([]pgUser{{CreatedAt: ts}})
	if err != nil {
		t.Fatalf("CreateNextPageToken: %v", err)
	}

	next := NewListRequestByCreatedAt[pgUser](&types.PagedResultRequest{PageSize: 1, NextPageToken: token})
	where, args, err := next.CurrentPageData()
	if err != nil {
		t.Fatalf("CurrentPageData: %v", err)
	}
	if where == nil || *where != "created_at > ?" {
		t.Fatalf("where = %v, want 'created_at > ?'", where)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v, want one element", args)
	}
}

func TestSetWhereAndWhereMap(t *testing.T) {
	r := NewListRequestByID[pgUser](&types.PagedResultRequest{PageSize: 5}).
		SetWhere("name ILIKE ?", "%bob%")
	where, args := r.Where()
	if where != "name ILIKE ?" || len(args) != 1 || args[0] != "%bob%" {
		t.Fatalf("Where = %v, %v", where, args)
	}

	r2 := NewListRequestByID[pgUser](&types.PagedResultRequest{PageSize: 5}).
		SetWhereMap(map[string]any{"active": true})
	if r2.WhereMap()["active"] != true {
		t.Fatalf("WhereMap = %v", r2.WhereMap())
	}
}
