package gormx

import (
	"context"
	"sync"
	"testing"
	"time"

	glebarez "github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// account carries an auto-updated timestamp so the update paths exercise GORM's
// AutoUpdateTime write-back branch, which (like plain column assignment) mutates
// the model struct GORM is given — the surface these tests guard against sharing.
type account struct {
	ID        int `gorm:"primaryKey"`
	Balance   int
	UpdatedAt time.Time
}

// newAccountsDB opens an in-memory SQLite pinned to a single connection. A
// ":memory:" database is per-connection, so capping the pool at one keeps every
// goroutine talking to the same database (and isolates each test to its own),
// while database/sql serializes access — which also avoids SQLite "database is
// locked" errors. Use it for correctness assertions that read state back.
func newAccountsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(glebarez.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	if err := db.AutoMigrate(&account{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// newSharedAccountsDB opens a shared-cache in-memory SQLite, uniquely named per
// test, with the default connection pool so multiple connections run in genuine
// parallel against one database. busy_timeout makes concurrent writers retry
// rather than fail with SQLITE_BUSY. This real parallelism is what lets the race
// detector observe the shared-model write-back; a single-connection pool would
// serialize the goroutines and hide it.
func newSharedAccountsDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=busy_timeout(10000)"
	db, err := gorm.Open(glebarez.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&account{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedAccounts(t *testing.T, table *Table[account], n int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= n; i++ {
		if err := table.Create(ctx, &account{ID: i}); err != nil {
			t.Fatalf("seed account %d: %v", i, err)
		}
	}
}

// TestUpdateMethodsConcurrentNoDataRace is the regression test for the shared-model
// data race: many connections drive UpdateMap / UpdateColumn / UpdateColumns through
// one Table in parallel. Before the fix, GORM wrote each map's columns (and the auto
// UpdatedAt) back into the Table's single shared *TEntity, so this tripped the race
// detector. Requires `go test -race` to be meaningful.
func TestUpdateMethodsConcurrentNoDataRace(t *testing.T) {
	table := NewTable[account](newSharedAccountsDB(t))
	const rows = 8
	seedAccounts(t, table, rows)

	var wg sync.WaitGroup
	for id := 1; id <= rows; id++ {
		wg.Go(func() {
			for j := range 25 {
				if _, err := table.UpdateMap(context.Background(),
					map[string]any{"balance": j}, "id = ?", id); err != nil {
					t.Errorf("UpdateMap(id=%d): %v", id, err)
					return
				}
				if _, err := table.UpdateColumn(context.Background(),
					"balance", j, "id = ?", id); err != nil {
					t.Errorf("UpdateColumn(id=%d): %v", id, err)
					return
				}
				if _, err := table.UpdateColumns(context.Background(),
					map[string]any{"balance": j}, "id = ?", id); err != nil {
					t.Errorf("UpdateColumns(id=%d): %v", id, err)
					return
				}
			}
		})
	}
	wg.Wait()
}

// TestConcurrentReadWriteMixNoDataRace exercises the remaining shared-model uses
// alongside writes: Get on a missing row calls entityName (which read the shared
// struct via reflection), Delete passes the model to GORM, and Count runs Model(...).
// None may trip the race detector. Requires `go test -race`.
func TestConcurrentReadWriteMixNoDataRace(t *testing.T) {
	table := NewTable[account](newSharedAccountsDB(t))
	const rows = 16
	seedAccounts(t, table, rows)
	ctx := context.Background()

	var wg sync.WaitGroup
	worker := func(fn func()) {
		wg.Go(func() {
			for range 40 {
				fn()
			}
		})
	}

	worker(func() { // writer
		if _, err := table.UpdateMap(ctx, map[string]any{"balance": 1}, "id = ?", 1); err != nil {
			t.Errorf("UpdateMap: %v", err)
		}
	})
	worker(func() { // missing-row Get drives entityName's reflection over TEntity
		if _, err := table.Get(ctx, "id = ?", -1); err == nil {
			t.Errorf("Get(missing) err = nil, want not-found")
		}
	})
	worker(func() { // Model(...) read
		if _, err := table.Count(ctx, "balance >= ?", 0); err != nil {
			t.Errorf("Count: %v", err)
		}
	})
	worker(func() { // Delete passes the model to GORM
		if _, err := table.Delete(ctx, "id = ?", 2); err != nil {
			t.Errorf("Delete: %v", err)
		}
	})
	wg.Wait()
}

// TestUpdateMapConcurrentDistinctRows checks that concurrent per-row updates land
// their final value — end-to-end correctness on top of the race guard above.
func TestUpdateMapConcurrentDistinctRows(t *testing.T) {
	table := NewTable[account](newAccountsDB(t))
	const rows = 8
	seedAccounts(t, table, rows)

	var wg sync.WaitGroup
	for id := 1; id <= rows; id++ {
		wg.Go(func() {
			for j := range 25 {
				n, err := table.UpdateMap(context.Background(),
					map[string]any{"balance": j}, "id = ?", id)
				if err != nil {
					t.Errorf("UpdateMap(id=%d): %v", id, err)
					return
				}
				if n != 1 {
					t.Errorf("UpdateMap(id=%d) affected = %d, want 1", id, n)
					return
				}
			}
		})
	}
	wg.Wait()

	for id := 1; id <= rows; id++ {
		got, err := table.Get(context.Background(), "id = ?", id)
		if err != nil {
			t.Fatalf("Get(id=%d): %v", id, err)
		}
		if got.Balance != 24 {
			t.Fatalf("account %d balance = %d, want 24", id, got.Balance)
		}
	}
}

// TestUpdateMapAtomicIncrementConcurrent checks that many goroutines atomically
// incrementing the same row through one Table lose no writes: the final balance
// equals the total number of increments.
func TestUpdateMapAtomicIncrementConcurrent(t *testing.T) {
	table := NewTable[account](newAccountsDB(t))
	seedAccounts(t, table, 1)

	const goroutines, perG = 8, 50
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range perG {
				n, err := table.UpdateMap(context.Background(),
					map[string]any{"balance": Expr("balance + 1")}, "id = ?", 1)
				if err != nil {
					t.Errorf("UpdateMap(incr): %v", err)
					return
				}
				if n != 1 {
					t.Errorf("UpdateMap(incr) affected = %d, want 1", n)
					return
				}
			}
		})
	}
	wg.Wait()

	got, err := table.Get(context.Background(), "id = ?", 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if want := goroutines * perG; got.Balance != want {
		t.Fatalf("balance = %d, want %d (lost updates)", got.Balance, want)
	}
}

// TestUpdateMapBumpsAutoTimestamp confirms the fresh per-call model still resolves
// the schema so GORM auto-updates UpdatedAt on a map update, and that a zero-valued
// model does not inject an implicit "id = 0" WHERE that would suppress the write.
func TestUpdateMapBumpsAutoTimestamp(t *testing.T) {
	table := NewTable[account](newAccountsDB(t))
	ctx := context.Background()
	if err := table.Create(ctx, &account{ID: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := table.Get(ctx, "id = ?", 1)
	if err != nil {
		t.Fatalf("Get(before): %v", err)
	}

	time.Sleep(2 * time.Millisecond)
	n, err := table.UpdateMap(ctx, map[string]any{"balance": 5}, "id = ?", 1)
	if err != nil {
		t.Fatalf("UpdateMap: %v", err)
	}
	if n != 1 {
		t.Fatalf("UpdateMap affected = %d, want 1", n)
	}

	after, err := table.Get(ctx, "id = ?", 1)
	if err != nil {
		t.Fatalf("Get(after): %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("UpdatedAt not advanced: before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
}

// TestUpdateMapNoImplicitPrimaryKeyWhere verifies the zero-valued per-call model
// does not narrow the statement: a broad WHERE updates every matching row rather
// than being silently scoped to the model's primary key.
func TestUpdateMapNoImplicitPrimaryKeyWhere(t *testing.T) {
	table := NewTable[account](newAccountsDB(t))
	const rows = 5
	seedAccounts(t, table, rows)

	n, err := table.UpdateMap(context.Background(), map[string]any{"balance": 7}, "id > ?", 0)
	if err != nil {
		t.Fatalf("UpdateMap: %v", err)
	}
	if n != rows {
		t.Fatalf("affected = %d, want %d (implicit PK where would narrow this)", n, rows)
	}
}
