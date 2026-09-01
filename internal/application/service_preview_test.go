package application

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

type previewSourceFunc func(context.Context, SourceRequest) (SourceResult, error)

func (f previewSourceFunc) Observe(ctx context.Context, request SourceRequest) (SourceResult, error) {
	return f(ctx, request)
}

func TestPreviewServiceShowsPartialSourceResultsWithoutPersisting(t *testing.T) {
	store := &publicationFakeStore{}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x61}, 256)))
	definition := service.config.Definitions["example"]
	definition.Sources = []domain.SourceDefinition{
		{ID: "vendor", Type: domain.SourceHTTP, ComponentID: "web", URL: "https://example.invalid/feed", Format: domain.FeedFormatText, Revision: "feed-v1"},
		{ID: "dns", Type: domain.SourceDNS, ComponentID: "web", Names: []string{"example.com"}, Revision: "dns-v1"},
	}
	service.config.Definitions["example"] = definition
	domainResource, _ := domain.NewDomainResource("api.example.com")
	addressResource, _ := domain.NewAddrResourceFromString("192.0.2.10")
	prefixResource, _ := domain.NewPrefixResourceFromString("198.51.100.0/24")
	service.config.Sources = SourceRegistry{
		domain.SourceDNS: previewSourceFunc(func(_ context.Context, request SourceRequest) (SourceResult, error) {
			if request.SourceID != "dns" || request.ObservedAt.IsZero() {
				t.Fatalf("request = %#v", request)
			}
			return SourceResult{Sightings: []domain.Sighting{
				{Resource: domainResource},
				{Resource: addressResource},
				{Resource: prefixResource},
				{Resource: domainResource},
			}, Skipped: 2}, nil
		}),
		domain.SourceHTTP: previewSourceFunc(func(context.Context, SourceRequest) (SourceResult, error) {
			return SourceResult{}, errors.New("upstream unavailable")
		}),
	}

	preview, err := service.PreviewService(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if preview.DomainCount != 1 || preview.AddressCount != 1 || preview.PrefixCount != 1 || preview.SkippedCount != 2 {
		t.Fatalf("preview = %#v", preview)
	}
	if !reflect.DeepEqual(preview.Domains, []string{"api.example.com"}) {
		t.Fatalf("domains = %#v", preview.Domains)
	}
	if len(preview.Sources) != 2 || preview.Sources[0].ID != "dns" || preview.Sources[0].Status != "ready" || preview.Sources[1].ID != "vendor" || preview.Sources[1].Status != "failed" || preview.Sources[1].ErrorCode != "http_feed_failed" {
		t.Fatalf("sources = %#v", preview.Sources)
	}
	if store.list.ID != "" || len(store.creates) != 0 || len(store.published) != 0 {
		t.Fatalf("preview mutated publication state: %#v", store)
	}
}

func TestPreviewServiceRejectsUnknownService(t *testing.T) {
	service := newPublicationTestService(t, &publicationFakeStore{}, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x62}, 256)))
	if _, err := service.PreviewService(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
