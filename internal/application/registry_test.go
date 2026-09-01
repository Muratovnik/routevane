package application

import (
	"errors"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

type stubRenderer struct {
	id          string
	version     string
	descriptor  domain.RendererDescriptor
	kinds       []domain.RuleKind
	descriptorZ bool
}

func (r stubRenderer) ID() string      { return r.id }
func (r stubRenderer) Version() string { return r.version }
func (r stubRenderer) Descriptor() domain.RendererDescriptor {
	if r.descriptorZ {
		return domain.RendererDescriptor{}
	}
	if r.descriptor.ID != "" || r.descriptor.ContentType != "" {
		return r.descriptor
	}
	return domain.RendererDescriptor{ID: r.id, Version: r.version, ContentType: "application/json", FileExtension: "json"}
}
func (r stubRenderer) SupportedRuleKinds() []domain.RuleKind {
	if r.kinds != nil {
		return r.kinds
	}
	return []domain.RuleKind{domain.RuleIPv4}
}
func (stubRenderer) ProjectedRuleCount(domain.RoutingPlan) (int, error) { return 0, nil }
func (stubRenderer) Render(domain.RoutingPlan) ([]byte, error)          { return []byte("{}"), nil }
func (stubRenderer) Validate([]byte) error                              { return nil }

func TestRendererRegistryValidateRefusesDriftAndIncompleteMetadata(t *testing.T) {
	good := stubRenderer{id: "good-renderer", version: "good-v1"}
	if err := (RendererRegistry{good.id: good}).Validate(); err != nil {
		t.Fatalf("a consistent registry was refused: %v", err)
	}
	cases := []struct {
		name     string
		registry RendererRegistry
	}{
		{"empty", RendererRegistry{}},
		{"nil implementation", RendererRegistry{"good-renderer": nil}},
		{"key does not match id", RendererRegistry{"other": good}},
		{"invalid id", RendererRegistry{"Bad_Renderer": stubRenderer{id: "Bad_Renderer", version: "good-v1"}}},
		{"invalid version", RendererRegistry{"good-renderer": stubRenderer{id: "good-renderer", version: "Bad Version"}}},
		{"empty descriptor", RendererRegistry{"good-renderer": stubRenderer{id: "good-renderer", version: "good-v1", descriptorZ: true}}},
		{"descriptor id drift", RendererRegistry{"good-renderer": stubRenderer{id: "good-renderer", version: "good-v1", descriptor: domain.RendererDescriptor{ID: "other-renderer", Version: "good-v1", ContentType: "application/json", FileExtension: "json"}}}},
		{"descriptor version drift", RendererRegistry{"good-renderer": stubRenderer{id: "good-renderer", version: "good-v1", descriptor: domain.RendererDescriptor{ID: "good-renderer", Version: "other-v1", ContentType: "application/json", FileExtension: "json"}}}},
		{"descriptor extension is not a bare suffix", RendererRegistry{"good-renderer": stubRenderer{id: "good-renderer", version: "good-v1", descriptor: domain.RendererDescriptor{ID: "good-renderer", Version: "good-v1", ContentType: "application/json", FileExtension: ".json"}}}},
		{"no rule kinds", RendererRegistry{"good-renderer": stubRenderer{id: "good-renderer", version: "good-v1", kinds: []domain.RuleKind{}}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.registry.Validate(); err == nil {
				t.Fatal("an inconsistent registry was accepted")
			}
		})
	}
}

func TestRendererRegistryForResolvesOnlyAMatchingTarget(t *testing.T) {
	renderer := stubRenderer{id: "good-renderer", version: "good-v1"}
	registry := RendererRegistry{renderer.id: renderer}
	resolved, err := registry.For(domain.TargetProfile{ID: "device", ProfileKey: "good-v1", RendererID: "good-renderer"})
	if err != nil || resolved.ID() != renderer.id {
		t.Fatalf("resolved = %v err = %v", resolved, err)
	}
	// Several targets may legitimately share one renderer.
	if _, err := registry.For(domain.TargetProfile{ID: "other-device", ProfileKey: "good-v1", RendererID: "good-renderer"}); err != nil {
		t.Fatalf("a second target sharing the format was refused: %v", err)
	}
	if _, err := registry.For(domain.TargetProfile{ID: "device", ProfileKey: "good-v1", RendererID: "missing"}); !errors.Is(err, ErrPreflight) {
		t.Fatalf("an unregistered renderer must be a preflight error: %v", err)
	}
	if _, err := registry.For(domain.TargetProfile{ID: "device", ProfileKey: "other-v1", RendererID: "good-renderer"}); !errors.Is(err, ErrPreflight) {
		t.Fatalf("a profile-key mismatch must be a preflight error: %v", err)
	}
}

func TestRendererDescriptorValidity(t *testing.T) {
	valid := domain.RendererDescriptor{ID: "good-renderer", Version: "good-v1", ContentType: "application/json", FileExtension: "json"}
	if !valid.IsValid() {
		t.Fatal("a complete descriptor was refused")
	}
	cases := []struct {
		name       string
		descriptor domain.RendererDescriptor
	}{
		{"zero", domain.RendererDescriptor{}},
		{"missing content type", domain.RendererDescriptor{ID: "good-renderer", Version: "good-v1", FileExtension: "json"}},
		{"missing extension", domain.RendererDescriptor{ID: "good-renderer", Version: "good-v1", ContentType: "application/json"}},
		{"path in extension", domain.RendererDescriptor{ID: "good-renderer", Version: "good-v1", ContentType: "application/json", FileExtension: "../json"}},
		{"uppercase extension", domain.RendererDescriptor{ID: "good-renderer", Version: "good-v1", ContentType: "application/json", FileExtension: "JSON"}},
		{"invalid id", domain.RendererDescriptor{ID: "Bad Id", Version: "good-v1", ContentType: "application/json", FileExtension: "json"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.descriptor.IsValid() {
				t.Fatalf("accepted %#v", testCase.descriptor)
			}
		})
	}
}
