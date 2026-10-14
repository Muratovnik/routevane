package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
)

// Only the final write is a probe: the HTTP decoder and application's actual
// batch-count, value validation and all-or-nothing path run unchanged.
type batchWriteRepository struct {
	application.PublicationRepository
	application.ObservationStore
	writes  int
	listID  string
	values  []string
	verdict application.DomainVerdict
}

func (s *batchWriteRepository) SetDomainVerdicts(_ context.Context, listID string, values []string, verdict application.DomainVerdict) error {
	s.writes++
	s.listID, s.values, s.verdict = listID, append([]string(nil), values...), verdict
	return nil
}

type batchUnusedSource struct{}

func (batchUnusedSource) Observe(context.Context, application.SourceRequest) (application.SourceResult, error) {
	return application.SourceResult{}, fmt.Errorf("batch import unexpectedly observed a source")
}

type batchApplicationBackend struct {
	*fakeBackend
	publication *application.PublicationService
}

func (b *batchApplicationBackend) SetListValues(ctx context.Context, listID string, values []string, verdict application.DomainVerdict) error {
	return b.publication.SetListValues(ctx, listID, values, verdict)
}

func newBatchImportHandler(t *testing.T) (http.Handler, *batchWriteRepository) {
	t.Helper()
	store := &batchWriteRepository{}
	target := domain.RawJSONTargetDefinition()
	publication, err := application.NewPublicationService(application.PublicationConfig{
		Definitions: map[string]domain.ListDefinition{"example": {
			ID: "example", CatalogRevision: strings.Repeat("c", 64),
			Components: []domain.ComponentDefinition{{ID: "web", Required: false}},
		}},
		Targets: map[string]domain.TargetDefinition{target.ID: target},
		Store:   store, Files: filesystem.PublishedStore{DataRoot: filepath.Join(t.TempDir(), "artifacts")},
		Renderers: application.RendererRegistry{rawjson.ID: rawjson.Renderer{}},
		Sources:   application.SourceRegistry{domain.SourceDNS: batchUnusedSource{}},
		Clock:     application.ClockFunc(func() time.Time { return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := New("http://127.0.0.1:8765", &batchApplicationBackend{fakeBackend: testBackend(), publication: publication}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler(), store
}

func longBatchDomains(t *testing.T) []string {
	t.Helper()
	values := make([]string, 1024)
	for i := range values {
		values[i] = fmt.Sprintf("%04d", i) + strings.Repeat("a", 59) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
		if len(values[i]) != 253 {
			t.Fatalf("maximum-length domain fixture has %d bytes", len(values[i]))
		}
	}
	return values
}

func batchImportJSON(t *testing.T, values []string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"values": values, "verdict": "include"})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestListValueImportAcceptsAFullMaximumLengthDomainBatch(t *testing.T) {
	values := longBatchDomains(t)
	body := batchImportJSON(t, values)
	if len(body) <= maxJSONBytes || len(body) >= maxListValuesJSONBytes {
		t.Fatalf("fixture did not cross only the ordinary body bound: %d bytes", len(body))
	}
	handler, store := newBatchImportHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, mutationRequest(t, "/v1/lists/example/domains", body))
	if response.Code != http.StatusOK || store.writes != 1 || store.listID != "example" || store.verdict != application.DomainVerdictInclude || !reflect.DeepEqual(store.values, values) {
		t.Fatalf("maximum valid batch code=%d writes=%d accepted=%d body=%s", response.Code, store.writes, len(store.values), response.Body.String())
	}
}

func TestListValueImportKeepsCountByteAndJSONBoundaries(t *testing.T) {
	small := batchImportJSON(t, []string{"example.com"})
	fullSize := strings.Repeat(" ", maxListValuesJSONBytes-len(small)) + small
	tooMany := append(longBatchDomains(t), "extra.example")
	tests := []struct {
		name   string
		path   string
		body   string
		status int
		writes int
	}{
		{"exact import byte bound", "/v1/lists/example/domains", fullSize, http.StatusOK, 1},
		{"above import byte bound", "/v1/lists/example/domains", " " + fullSize, http.StatusRequestEntityTooLarge, 0},
		{"1025 domains", "/v1/lists/example/domains", batchImportJSON(t, tooMany), http.StatusUnprocessableEntity, 0},
		{"empty batch", "/v1/lists/example/domains", batchImportJSON(t, nil), http.StatusBadRequest, 0},
		{"invalid final destination", "/v1/lists/example/domains", batchImportJSON(t, []string{"example.com", "not a destination"}), http.StatusBadRequest, 0},
		{"unknown field", "/v1/lists/example/domains", `{"values":["example.com"],"verdict":"include","extra":true}`, http.StatusBadRequest, 0},
		{"trailing document", "/v1/lists/example/domains", small + `{}`, http.StatusBadRequest, 0},
		{"ordinary mutation still 64 KiB", "/v1/profiles", `{"name":"` + strings.Repeat("x", maxJSONBytes) + `","lists":["example"]}`, http.StatusRequestEntityTooLarge, 0},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			handler, store := newBatchImportHandler(t)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, mutationRequest(t, testCase.path, testCase.body))
			if response.Code != testCase.status || store.writes != testCase.writes {
				t.Fatalf("status=%d want=%d writes=%d want=%d body=%s", response.Code, testCase.status, store.writes, testCase.writes, response.Body.String())
			}
		})
	}
}
