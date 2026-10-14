package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/httpapi"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
)

// Pause before the removal takes the publication lock. The competing profile
// write can finish normally, so this checks integrity without making a blocked
// writer responsible for releasing a lock its reader already owns.
type removalInterleavingClock struct {
	ctx     context.Context
	now     time.Time
	mu      sync.Mutex
	armed   bool
	reached chan struct{}
	release chan struct{}
}

func (c *removalInterleavingClock) Now() time.Time {
	c.mu.Lock()
	pause := c.armed
	c.armed = false
	c.mu.Unlock()
	if pause {
		close(c.reached)
		select {
		case <-c.release:
		case <-c.ctx.Done():
			return time.Time{}
		}
	}
	return c.now
}

func (c *removalInterleavingClock) arm() {
	c.mu.Lock()
	c.armed = true
	c.mu.Unlock()
}

func TestLibraryRemovalRechecksConcurrentProfileReferences(t *testing.T) {
	for _, kind := range []string{"list", "category"} {
		for _, operation := range []string{"create", "update"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				dataRoot, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
				if err != nil {
					t.Fatal(err)
				}
				store, err := sqlite.Open(ctx, dataRoot)
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				clock := &removalInterleavingClock{ctx: ctx, now: time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC), reached: make(chan struct{}), release: make(chan struct{})}
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(clock.release) }) }
				defer release()
				service := newLibraryReferenceService(t, dataRoot, store, clock)
				backend := serveBackend{PublicationService: service, deliveryGate: application.NewDeliveryGate()}
				server, err := httpapi.New("http://127.0.0.1:43821", backend, nil)
				if err != nil {
					t.Fatal(err)
				}
				post := func(path, body string) *httptest.ResponseRecorder {
					r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:43821"+path, strings.NewReader(body)).WithContext(ctx)
					r.RemoteAddr = "127.0.0.1:12345"
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set("X-Routevane-Request", "1")
					r.Header.Set("Origin", "http://127.0.0.1:43821")
					w := httptest.NewRecorder()
					server.Handler().ServeHTTP(w, r)
					return w
				}
				var identity, field, removalPath, removalBody string
				if kind == "list" {
					list, err := backend.CreateCustomList(ctx, "Concurrent list", []string{"example.test"})
					if err != nil {
						t.Fatal(err)
					}
					identity, field, removalPath, removalBody = list.ID, "lists", "/v1/lists/"+list.ID+"/remove", "{}"
				} else {
					category, err := backend.CreateCategory(ctx, "Concurrent category", []string{"alpha"})
					if err != nil {
						t.Fatal(err)
					}
					identity, field, removalPath, removalBody = category.ID, "categories", "/v1/categories/"+category.ID+"/remove", `{"lists":"detach"}`
				}
				profilePath := "/v1/profiles"
				wantWriteStatus := http.StatusCreated
				if operation == "update" {
					prior, err := backend.CreateProfile(ctx, "Before", application.ProfileComposition{Lists: []string{"alpha"}})
					if err != nil {
						t.Fatal(err)
					}
					profilePath += "/" + prior.ID + "/update"
					wantWriteStatus = http.StatusOK
				}
				removed := make(chan *httptest.ResponseRecorder, 1)
				clock.arm()
				go func() { removed <- post(removalPath, removalBody) }()
				select {
				case <-clock.reached:
				case <-ctx.Done():
					t.Fatal("removal did not reach admission barrier")
				}
				written := post(profilePath, fmt.Sprintf(`{"name":"Concurrent profile",%q:[%q]}`, field, identity))
				if written.Code != wantWriteStatus {
					t.Fatalf("concurrent write status=%d body=%s", written.Code, written.Body)
				}
				var response struct {
					Profile application.Profile `json:"profile"`
				}
				if err := json.Unmarshal(written.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				release()
				var removedResponse *httptest.ResponseRecorder
				select {
				case removedResponse = <-removed:
				case <-ctx.Done():
					t.Fatal("removal did not complete")
				}
				if removedResponse.Code != http.StatusConflict {
					t.Fatalf("removal status=%d body=%s", removedResponse.Code, removedResponse.Body)
				}
				stored, err := store.Profile(ctx, response.Profile.ID)
				if err != nil {
					t.Fatal(err)
				}
				var refs []application.ProfileReference
				if kind == "list" {
					if !slices.Contains(stored.Lists, identity) {
						t.Fatal("profile no longer names list")
					}
					lists, err := store.CustomLists(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if !slices.ContainsFunc(lists, func(list application.CustomList) bool { return list.ID == identity }) {
						t.Fatal("referenced list was deleted")
					}
					refs, err = store.ProfileReferences(ctx, nil, []string{identity})
					if err != nil {
						t.Fatal(err)
					}
				} else {
					if !slices.Contains(stored.Categories, identity) {
						t.Fatal("profile no longer names category")
					}
					overlay, err := store.CategoryOverlay(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if !slices.ContainsFunc(overlay.Categories, func(category application.CustomCategory) bool { return category.ID == identity }) {
						t.Fatal("referenced category was deleted")
					}
					refs, err = store.ProfileReferences(ctx, []string{identity}, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				if len(refs) != 1 || refs[0].ID != stored.ID {
					t.Fatalf("persisted refs=%#v", refs)
				}
				// The refusal is recoverable: an ordinary edit releases the reference
				// and an ordinary retry then removes the unreferenced object.
				updated := post("/v1/profiles/"+stored.ID+"/update", `{"name":"Uses surviving list","lists":["alpha"]}`)
				if updated.Code != http.StatusOK {
					t.Fatalf("release reference status=%d body=%s", updated.Code, updated.Body)
				}
				retried := post(removalPath, removalBody)
				if retried.Code != http.StatusNoContent {
					t.Fatalf("ordinary removal status=%d body=%s", retried.Code, retried.Body)
				}
			})
		}
	}
}

func TestCategoryRemovalUsesCurrentMembership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dataRoot, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := &removalInterleavingClock{ctx: ctx, now: time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC), reached: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(clock.release) }) }
	defer release()
	service := newLibraryReferenceService(t, dataRoot, store, clock)
	category, err := service.CreateCategory(ctx, "Changing category", []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := service.CreateCustomList(ctx, "Replacement", []string{"example.test"})
	if err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	clock.arm()
	go func() { removed <- service.RemoveCategory(ctx, category.ID, application.CategoryListsDelete) }()
	select {
	case <-clock.reached:
	case <-ctx.Done():
		t.Fatal("removal did not reach admission barrier")
	}
	members := []string{list.ID}
	if _, err := service.UpdateCategory(ctx, category.ID, application.CategoryUpdate{Lists: &members}); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-removed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("removal did not complete")
	}
	if lists := service.Lists(); !slices.Contains(lists, "alpha") || slices.Contains(lists, list.ID) {
		t.Fatalf("removal used stale membership: lists=%v", lists)
	}
	stored, err := store.CustomLists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(stored, func(value application.CustomList) bool { return value.ID == list.ID }) {
		t.Fatal("current member survived category deletion")
	}
}

func newLibraryReferenceService(t *testing.T, dataRoot string, store *sqlite.Store, clock application.Clock) *application.PublicationService {
	t.Helper()
	target := domain.RawJSONTargetDefinition()
	service, err := application.NewPublicationService(application.PublicationConfig{
		Definitions: map[string]domain.ListDefinition{"alpha": {ID: "alpha", CatalogRevision: strings.Repeat("c", 64), Components: []domain.ComponentDefinition{{ID: "web", Required: true}}, Seeds: []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:alpha", SourceClass: domain.SourceManual}}}},
		Targets:     map[string]domain.TargetDefinition{target.ID: target}, Store: store,
		Files: filesystem.PublishedStore{DataRoot: dataRoot}, Renderers: application.RendererRegistry{rawjson.ID: rawjson.Renderer{}},
		Sources: application.SourceRegistry{domain.SourceDNS: unusedPublicationSource{}}, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
