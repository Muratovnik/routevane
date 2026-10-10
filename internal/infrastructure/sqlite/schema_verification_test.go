package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestColumnVerificationPreservesShapeChecks(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "valid columns in a different order",
			sql:  "CREATE TABLE settings(updated_at_ns INTEGER, value TEXT, key TEXT)",
		},
		{
			name: "renamed required column with the same count",
			sql:  "CREATE TABLE settings(key TEXT, value TEXT, other_at_ns INTEGER)",
			want: "required column settings.updated_at_ns",
		},
		{
			name: "extra column",
			sql:  "CREATE TABLE settings(key TEXT, value TEXT, updated_at_ns INTEGER, extra TEXT)",
			want: "table shape settings",
		},
		{
			name: "column count is checked before names",
			sql:  "CREATE TABLE settings(key TEXT, value TEXT, other_at_ns INTEGER, extra TEXT)",
			want: "table shape settings",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newColumnVerificationDB(t)
			if _, err := db.ExecContext(t.Context(), test.sql); err != nil {
				t.Fatal(err)
			}
			err := verifyColumns(t.Context(), db, "settings")
			if test.want == "" {
				if err != nil {
					t.Fatalf("valid columns were refused: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrIncompatibleSchema) || err.Error() != ErrIncompatibleSchema.Error()+": "+test.want {
				t.Fatalf("column verification error=%v, want %s: %s", err, ErrIncompatibleSchema, test.want)
			}
		})
	}
}

func TestColumnVerificationPreservesReadErrors(t *testing.T) {
	db := newColumnVerificationDB(t)
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE settings(key TEXT, value TEXT, updated_at_ns INTEGER)"); err != nil {
		t.Fatal(err)
	}
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		assertColumnReadError(t, verifyColumns(ctx, db, "settings"), context.Canceled)
	})
	t.Run("closed connection", func(t *testing.T) {
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		assertColumnReadError(t, verifyColumns(t.Context(), conn, "settings"), sql.ErrConnDone)
	})
	if err := verifyColumns(t.Context(), db, "settings"); err != nil {
		t.Fatalf("the readable schema was refused: %v", err)
	}
}

func assertColumnReadError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || errors.Is(err, ErrIncompatibleSchema) || !strings.HasPrefix(err.Error(), "inspect schema: ") {
		t.Fatalf("column read error=%v, want wrapped %v without a schema verdict", err, want)
	}
}

func newColumnVerificationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close schema verification database: %v", err)
		}
	})
	return db
}
