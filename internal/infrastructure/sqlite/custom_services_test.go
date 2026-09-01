package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestCustomServicesSurviveWriteReadAndUpdate(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	service := application.CustomService{
		ID: "custom-1234567890abcdef", Title: "Мои сайты",
		Domains: []string{"a.example", "b.example"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateCustomService(context.Background(), service); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCustomService(context.Background(), service); !errors.Is(err, application.ErrIdentityCollision) {
		t.Fatalf("duplicate err = %v", err)
	}
	stored, err := store.CustomServices(context.Background())
	if err != nil || !reflect.DeepEqual(stored, []application.CustomService{service}) {
		t.Fatalf("stored = %#v err = %v", stored, err)
	}
	updated := service
	updated.Title, updated.Domains, updated.UpdatedAt = "Сайты 2", []string{"c.example"}, now.Add(time.Hour)
	if err := store.UpdateCustomService(context.Background(), updated); err != nil {
		t.Fatal(err)
	}
	stored, err = store.CustomServices(context.Background())
	if err != nil || len(stored) != 1 || stored[0].Title != "Сайты 2" || !reflect.DeepEqual(stored[0].Domains, []string{"c.example"}) || !stored[0].CreatedAt.Equal(now) {
		t.Fatalf("updated = %#v err = %v", stored, err)
	}
	missing := updated
	missing.ID = "custom-fedcba0987654321"
	if err := store.UpdateCustomService(context.Background(), missing); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing update err = %v", err)
	}
	if _, err := store.db.Exec(`UPDATE custom_services SET id=? WHERE id=?`, "custom-aaaaaaaaaaaaaaaa", service.ID); err == nil {
		t.Fatal("identity rewrite was accepted")
	}
	// A list the operator created is theirs to delete (ADR 0029). It leaves no
	// removal record: the row was the whole object, so its absence is the
	// deletion, and a record would outlive whatever later took the identity.
	if err := store.RemoveFromLibrary(context.Background(), application.LibraryRemoval{
		Kind: application.RemovalService, ID: service.ID, RemovedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	stored, err = store.CustomServices(context.Background())
	if err != nil || len(stored) != 0 {
		t.Fatalf("deleted = %#v err = %v", stored, err)
	}
	overlay, err := store.CategoryOverlay(context.Background())
	if err != nil || len(overlay.Removals) != 0 {
		t.Fatalf("an operator-created list recorded a removal: %#v err = %v", overlay.Removals, err)
	}
	if err := store.RemoveFromLibrary(context.Background(), application.LibraryRemoval{
		Kind: application.RemovalService, ID: service.ID, RemovedAt: now,
	}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("second deletion err = %v", err)
	}
}

func TestCustomServiceWritesRefuseWhatTheGrammarForbids(t *testing.T) {
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	base := application.CustomService{ID: "custom-1234567890abcdef", Title: "X", Domains: []string{"a.example"}, CreatedAt: now, UpdatedAt: now}
	cases := []func(application.CustomService) application.CustomService{
		func(s application.CustomService) application.CustomService { s.ID = "shipped-id"; return s },
		func(s application.CustomService) application.CustomService { s.Title = ""; return s },
		func(s application.CustomService) application.CustomService { s.Domains = nil; return s },
		func(s application.CustomService) application.CustomService {
			s.Domains = []string{"Not.Normalized"}
			return s
		},
		func(s application.CustomService) application.CustomService { s.CreatedAt = time.Time{}; return s },
	}
	for index, mutate := range cases {
		if err := store.CreateCustomService(context.Background(), mutate(base)); err == nil {
			t.Errorf("case %d accepted", index)
		}
	}
}
