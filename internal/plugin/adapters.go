package plugin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planjson"
	wire "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

var (
	// ErrPluginOutput reports an answer the host refused to trust.
	ErrPluginOutput = errors.New("plugin answer is not usable")
)

// SourceValidity is how long an observation from a plugin source stays fresh
// when the plugin reports no TTL of its own.
const SourceValidity = 24 * time.Hour

// Renderer adapts a renderer plugin to the application's renderer seam.
//
// A plugin renderer is indistinguishable downstream: it goes through the same
// preflight, the same descriptor, the same artifact storage, and the same
// validation as a built-in one. What a plugin cannot do is skip any of them.
type Renderer struct {
	client   *Client
	manifest wire.RendererManifest
	kinds    []domain.RuleKind
}

// NewRenderer adapts a started renderer plugin.
func NewRenderer(client *Client) (*Renderer, error) {
	if client == nil {
		return nil, ErrUnavailable
	}
	manifest := client.Manifest()
	if manifest.Kind != wire.KindRenderer || manifest.Renderer == nil {
		return nil, fmt.Errorf("%w: %s is not a renderer", ErrManifestInvalid, manifest.Name)
	}
	kinds := make([]domain.RuleKind, 0, len(manifest.Renderer.SupportedRuleKinds))
	for _, value := range manifest.Renderer.SupportedRuleKinds {
		kind := domain.RuleKind(value)
		if !knownRuleKind(kind) {
			return nil, fmt.Errorf("%w: unknown rule kind %q", ErrManifestInvalid, value)
		}
		kinds = append(kinds, kind)
	}
	descriptor := domain.RendererDescriptor{
		ID: manifest.Renderer.ID, Version: manifest.Renderer.FormatVersion,
		ContentType: manifest.Renderer.ContentType, FileExtension: manifest.Renderer.FileExtension,
	}
	if !descriptor.IsValid() {
		return nil, fmt.Errorf("%w: renderer descriptor is not usable", ErrManifestInvalid)
	}
	return &Renderer{client: client, manifest: *manifest.Renderer, kinds: kinds}, nil
}

func knownRuleKind(kind domain.RuleKind) bool {
	switch kind {
	case domain.RuleDomainExact, domain.RuleDomainSuffix, domain.RuleIPv4, domain.RuleIPv6, domain.RulePrefix4, domain.RulePrefix6:
		return true
	default:
		return false
	}
}

func (r *Renderer) ID() string      { return r.manifest.ID }
func (r *Renderer) Version() string { return r.manifest.FormatVersion }

func (r *Renderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{
		ID: r.manifest.ID, Version: r.manifest.FormatVersion,
		ContentType: r.manifest.ContentType, FileExtension: r.manifest.FileExtension,
	}
}

func (r *Renderer) SupportedRuleKinds() []domain.RuleKind {
	return append([]domain.RuleKind(nil), r.kinds...)
}

// ProjectedRuleCount asks the plugin how many entries it would emit.
func (r *Renderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	encoded, err := planjson.Encode(plan)
	if err != nil {
		return 0, fmt.Errorf("encode plan for plugin: %w", err)
	}
	result, err := r.client.Call(context.Background(), wire.Envelope{
		Type: wire.MessageProjectedRuleCount, Render: &wire.RenderCall{PlanJSON: encoded, Projection: true},
	})
	if err != nil {
		return 0, err
	}
	if result.RuleCount < 0 {
		return 0, fmt.Errorf("%w: negative rule count", ErrPluginOutput)
	}
	return result.RuleCount, nil
}

// Render asks the plugin for an artifact and then requires the plugin's own
// validator to accept it.
//
// Validating the plugin's output with the plugin's own validator is the point: a
// renderer that cannot read back what it wrote is refused here rather than
// publishing bytes nothing can check.
func (r *Renderer) Render(plan domain.RoutingPlan) ([]byte, error) {
	encoded, err := planjson.Encode(plan)
	if err != nil {
		return nil, fmt.Errorf("encode plan for plugin: %w", err)
	}
	result, err := r.client.Call(context.Background(), wire.Envelope{
		Type: wire.MessageRender, Render: &wire.RenderCall{PlanJSON: encoded},
	})
	if err != nil {
		return nil, err
	}
	if len(result.Payload) == 0 {
		return nil, fmt.Errorf("%w: empty artifact", ErrPluginOutput)
	}
	if err := r.Validate(result.Payload); err != nil {
		return nil, fmt.Errorf("%w: the plugin cannot validate its own artifact: %v", ErrPluginOutput, err)
	}
	return result.Payload, nil
}

// Validate asks the plugin's validator about bytes.
func (r *Renderer) Validate(payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: empty artifact", ErrPluginOutput)
	}
	_, err := r.client.Call(context.Background(), wire.Envelope{
		Type: wire.MessageValidate, Validate: &wire.ValidateCall{Payload: payload},
	})
	return err
}

// Source adapts a source plugin to the application's source seam.
//
// Every value a plugin reports is parsed by the host before it becomes an
// observation, so a plugin cannot introduce an unchecked address, a malformed
// prefix, or a value for a service it was not asked about.
type Source struct {
	client   *Client
	manifest wire.SourceManifest
	validity time.Duration
}

// NewSource adapts a started source plugin.
func NewSource(client *Client, validity time.Duration) (*Source, error) {
	if client == nil {
		return nil, ErrUnavailable
	}
	manifest := client.Manifest()
	if manifest.Kind != wire.KindSource || manifest.Source == nil {
		return nil, fmt.Errorf("%w: %s is not a source", ErrManifestInvalid, manifest.Name)
	}
	if validity <= 0 {
		validity = SourceValidity
	}
	return &Source{client: client, manifest: *manifest.Source, validity: validity}, nil
}

// Type reports the catalog source type this plugin serves.
func (s *Source) Type() domain.SourceType { return domain.SourceType(s.manifest.Type) }

// Revision reports the plugin's observation revision.
func (s *Source) Revision() string { return s.manifest.Revision }

func (s *Source) Observe(ctx context.Context, request application.SourceRequest) (application.SourceResult, error) {
	if domain.ValidateSlug(request.ServiceID) != nil || domain.ValidateSlug(request.ComponentID) != nil || domain.ValidateSlug(request.SourceID) != nil {
		return application.SourceResult{}, fmt.Errorf("%w: invalid observation request", ErrPluginOutput)
	}
	observedAt := request.ObservedAt.UTC()
	if observedAt.IsZero() {
		return application.SourceResult{}, fmt.Errorf("%w: zero observation time", ErrPluginOutput)
	}
	result, err := s.client.Call(ctx, wire.Envelope{Type: wire.MessageObserve, Observe: &wire.ObserveCall{
		ServiceID: request.ServiceID, ComponentID: request.ComponentID, SourceID: request.SourceID,
		Revision: request.SourceRevision, Names: append([]string(nil), request.Names...),
		ObservedAt: observedAt.Format(time.RFC3339Nano),
	}})
	if err != nil {
		return application.SourceResult{}, err
	}
	sightings := make([]domain.Sighting, 0, len(result.Observations))
	skipped := 0
	seen := map[string]struct{}{}
	for _, observation := range result.Observations {
		resource, ok := parseObservation(observation.Value)
		if !ok {
			// A value the host cannot normalize is refused, never stored. The
			// count is reported so an unusable plugin is visible.
			skipped++
			continue
		}
		key := resource.Kind.String() + "\x00" + resource.CanonicalValue()
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		validUntil := observedAt.Add(s.validity)
		ttlKnown := false
		if observation.TTLSeconds > 0 {
			validUntil = observedAt.Add(time.Duration(observation.TTLSeconds) * time.Second)
			ttlKnown = true
		}
		sightings = append(sightings, domain.Sighting{
			ServiceID: request.ServiceID, ComponentID: request.ComponentID, Resource: resource,
			SourceID: request.SourceID, SourceClass: domain.SourceCommunity, SourceRevision: request.SourceRevision,
			FirstSeen: observedAt, LastSeen: observedAt, ValidUntil: validUntil,
			TTLSeconds: observation.TTLSeconds, TTLKnown: ttlKnown,
			ObservationCount: 1, Validity: domain.ValidityValid,
		})
	}
	slices.SortFunc(sightings, func(a, b domain.Sighting) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	if len(sightings) == 0 {
		return application.SourceResult{Skipped: skipped}, fmt.Errorf("%w: no usable observation", ErrPluginOutput)
	}
	return application.SourceResult{Sightings: sightings, Skipped: skipped}, nil
}

// parseObservation accepts one address or one prefix, normalized by the domain
// model. A plugin never contributes an unchecked string.
func parseObservation(value string) (domain.Resource, bool) {
	if value == "" || len(value) > 64 {
		return domain.Resource{}, false
	}
	if _, err := netip.ParsePrefix(value); err == nil {
		resource, err := domain.NewPrefixResourceFromString(value)
		if err != nil || !resource.IsValid() {
			return domain.Resource{}, false
		}
		return resource, true
	}
	resource, err := domain.NewAddrResourceFromString(value)
	if err != nil || !resource.IsValid() {
		return domain.Resource{}, false
	}
	return resource, true
}
