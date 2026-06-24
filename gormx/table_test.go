package gormx

import (
	"context"
	"errors"
	"testing"

	"github.com/hatami57/microjet/core/errorx"
	glebarez "github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type widget struct {
	ID    int `gorm:"primaryKey"`
	Name  string
	Color string
	Price int
}

func openWidgets(t *testing.T, seed ...widget) *Table[widget] {
	t.Helper()
	db, err := gorm.Open(glebarez.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&widget{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	table := NewTable[widget](db)
	for i := range seed {
		w := seed[i]
		if err := table.Create(context.Background(), &w); err != nil {
			t.Fatalf("seed %v: %v", w, err)
		}
	}
	return table
}

func TestFirstRespectsOrder(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a", Price: 30},
		widget{ID: 2, Name: "b", Price: 10},
		widget{ID: 3, Name: "c", Price: 20},
	)

	got, err := table.Order("price ASC").First(context.Background())
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if got == nil || got.Name != "b" {
		t.Fatalf("cheapest widget = %v, want b", got)
	}
}

func TestLastByPrimaryKey(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a"},
		widget{ID: 2, Name: "b"},
	)

	got, err := table.Last(context.Background())
	if err != nil {
		t.Fatalf("Last: %v", err)
	}
	if got == nil || got.ID != 2 {
		t.Fatalf("Last = %v, want ID 2", got)
	}
}

func TestTakeReturnsNilWhenEmpty(t *testing.T) {
	table := openWidgets(t)

	got, err := table.Take(context.Background())
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if got != nil {
		t.Fatalf("Take on empty table = %v, want nil", got)
	}
}

func TestChainedScopesAccumulate(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a", Color: "red", Price: 5},
		widget{ID: 2, Name: "b", Color: "red", Price: 15},
		widget{ID: 3, Name: "c", Color: "blue", Price: 25},
	)

	got, err := table.
		Where("color = ?", "red").
		Order("price DESC").
		Limit(1).
		ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("chained scopes = %v, want single widget b", got)
	}
}

func TestExists(t *testing.T) {
	table := openWidgets(t, widget{ID: 1, Name: "a", Color: "red"})

	ok, err := table.Exists(context.Background(), "color = ?", "red")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatal("Exists(red) = false, want true")
	}

	ok, err = table.Exists(context.Background(), "color = ?", "green")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if ok {
		t.Fatal("Exists(green) = true, want false")
	}
}

func TestGetReturnsNotFoundError(t *testing.T) {
	table := openWidgets(t, widget{ID: 1, Name: "a"})

	got, err := table.Get(context.Background(), "id = ?", 1)
	if err != nil {
		t.Fatalf("Get(existing): %v", err)
	}
	if got == nil || got.ID != 1 {
		t.Fatalf("Get(existing) = %v, want ID 1", got)
	}

	_, err = table.Get(context.Background(), "id = ?", 999)
	if !errors.Is(err, errorx.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want errorx.ErrNotFound", err)
	}
	if subj := errorx.GetError(err).Subject; subj != "widget" {
		t.Fatalf("not-found subject = %q, want widget", subj)
	}
}

func TestScopesDoNotMutateOriginal(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red"},
		widget{ID: 2, Color: "blue"},
	)

	filtered := table.Where("color = ?", "red")

	all, err := table.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("original table returned %d rows, want 2 — Where mutated it", len(all))
	}

	red, err := filtered.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(red) != 1 {
		t.Fatalf("filtered table returned %d rows, want 1", len(red))
	}
}
