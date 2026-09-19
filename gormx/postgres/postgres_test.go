package postgres

import (
	"github.com/hatami57/microjet/gormx"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestConnectionStringPreservesSpecialCharacters(t *testing.T) {
	for _, password := range []string{"", "spaces in password", "quote'and\\slash", "secret sslmode=disable"} {
		cfg := gormx.Config{Host: "localhost", Port: 5432, User: "user name", Password: password, Name: "db name", SSLMode: "require"}
		parsed, err := pgconn.ParseConfig(connectionString(cfg))
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Password != password || parsed.User != cfg.User || parsed.Database != cfg.Name || parsed.TLSConfig == nil {
			t.Fatalf("connection parameters did not round-trip for %q", password)
		}
	}
}
