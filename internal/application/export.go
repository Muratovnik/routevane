package application

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

// ExportFormat is a renderer profile available for a one-off file. Its ID is
// deliberately not an output ID: choosing it creates no consumer,
// subscription or publication history.
type ExportFormat struct {
	ID            string `json:"id"`
	RendererID    string `json:"renderer_id"`
	FileExtension string `json:"file_extension"`
	ContentType   string `json:"content_type"`
}

type ExportPayload struct {
	Format     ExportFormat
	Descriptor domain.RendererDescriptor
	Payload    []byte
}

// ExportFormats lists every catalog-backed renderer profile and the generic
// diagnostic JSON renderer when this build carries it. The catalog target is
// only the constraint profile used internally; the UI presents the file
// dialect, not a requirement to create that consumer.
func (s *PublicationService) ExportFormats() []ExportFormat {
	ids := slices.Sorted(maps.Keys(s.config.Targets))
	type formatChoice struct {
		format ExportFormat
		target domain.TargetDefinition
	}
	choices := make(map[string]formatChoice, len(ids))
	for _, id := range ids {
		target, renderer, err := s.target(id)
		if err != nil {
			continue
		}
		descriptor := renderer.Descriptor()
		format := ExportFormat{
			ID: target.ID, RendererID: descriptor.ID,
			FileExtension: descriptor.FileExtension, ContentType: descriptor.ContentType,
		}
		key := target.FormatKey + "\x00" + descriptor.ID + "\x00" + strings.Join(target.RendererOptions, "\x00")
		current, exists := choices[key]
		if !exists || preferExportTarget(target, current.target) {
			choices[key] = formatChoice{format: format, target: target}
		}
	}
	formats := make([]ExportFormat, 0, len(choices)+1)
	for _, choice := range choices {
		formats = append(formats, choice.format)
	}
	raw := domain.RawJSONTargetDefinition()
	if renderer, ok := s.config.Renderers[raw.RendererID]; ok && renderer != nil && renderer.Version() == raw.FormatKey {
		descriptor := renderer.Descriptor()
		formats = append(formats, ExportFormat{
			ID: raw.ID, RendererID: descriptor.ID,
			FileExtension: descriptor.FileExtension, ContentType: descriptor.ContentType,
		})
	}
	slices.SortFunc(formats, func(a, b ExportFormat) int {
		return cmp.Or(cmp.Compare(a.RendererID, b.RendererID), cmp.Compare(a.ID, b.ID))
	})
	return formats
}

func preferExportTarget(candidate, current domain.TargetDefinition) bool {
	left, right := candidate.Constraints, current.Constraints
	leftKinds := supportedExportKinds(left)
	rightKinds := supportedExportKinds(right)
	if leftKinds != rightKinds {
		return leftKinds > rightKinds
	}
	if exportLimit(left.MaxRules) != exportLimit(right.MaxRules) {
		return exportLimit(left.MaxRules) > exportLimit(right.MaxRules)
	}
	if exportLimit(left.MaxArtifactSize) != exportLimit(right.MaxArtifactSize) {
		return exportLimit(left.MaxArtifactSize) > exportLimit(right.MaxArtifactSize)
	}
	return candidate.ID < current.ID
}

func supportedExportKinds(constraints domain.TargetConstraints) int {
	values := [...]bool{
		constraints.SupportsDomainExact,
		constraints.SupportsDomainSuffix,
		constraints.SupportsDynamicDNSSet,
		constraints.SupportsIPv4,
		constraints.SupportsIPv6,
		constraints.SupportsPrefixes,
	}
	count := 0
	for _, supported := range values {
		if supported {
			count++
		}
	}
	return count
}

func exportLimit(value int) int {
	if value == 0 {
		return int(^uint(0) >> 1)
	}
	return value
}

// Export renders one current list without persisting an output or artifact.
// Durable consumers continue to use AddOutput and Build; this is only the
// user's explicit "download this file format now" action.
func (s *PublicationService) Export(ctx context.Context, profileID, formatID string) (ExportPayload, error) {
	profile, err := s.Profile(ctx, profileID)
	if err != nil {
		return ExportPayload{}, err
	}
	target, renderer, err := s.exportTarget(formatID)
	if err != nil {
		return ExportPayload{}, ErrNotFound
	}
	prepared, _, err := s.prepareProfile(ctx, profile, target, renderer)
	if err != nil {
		return ExportPayload{}, err
	}
	payload, err := RenderPrepared(prepared, renderer)
	if err != nil {
		return ExportPayload{}, err
	}
	descriptor := renderer.Descriptor()
	return ExportPayload{
		Format: ExportFormat{
			ID: formatID, RendererID: descriptor.ID,
			FileExtension: descriptor.FileExtension, ContentType: descriptor.ContentType,
		},
		Descriptor: descriptor,
		Payload:    payload,
	}, nil
}

func (s *PublicationService) exportTarget(formatID string) (domain.TargetDefinition, Renderer, error) {
	if formatID != domain.RawJSONTargetDefinition().ID {
		return s.target(formatID)
	}
	target := domain.RawJSONTargetDefinition()
	renderer, ok := s.config.Renderers[target.RendererID]
	if !ok || renderer == nil || renderer.Version() != target.FormatKey {
		return domain.TargetDefinition{}, nil, ErrNotFound
	}
	return target, renderer, nil
}
