package catalogyaml

import (
	"context"
	"fmt"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

type rawTarget struct {
	ID        string `yaml:"id"`
	FormatKey string `yaml:"format_key"`
	// RetiredFormatKey is the key this format used before ADR 0039 renamed it,
	// so the word profile could name the operator's own object instead. It is
	// read for one minor version; a file naming both is refused rather than
	// merged.
	RetiredFormatKey       string         `yaml:"profile_key"`
	Title                  string         `yaml:"title"`
	Kind                   string         `yaml:"kind"`
	Renderer               string         `yaml:"renderer"`
	Constraints            rawConstraints `yaml:"constraints"`
	RendererOptions        []string       `yaml:"renderer_options"`
	ManualInstallationHint string         `yaml:"manual_installation_hint"`
	// The English pair is a pointer because absent and blank are different
	// states here. A target file written before these fields, or by a plugin
	// author who ships one language, is still valid; a key written with
	// nothing behind it is a mistake in the file and is refused rather than
	// normalized into the same silence.
	TitleEN                  *string `yaml:"title_en"`
	ManualInstallationHintEN *string `yaml:"manual_installation_hint_en"`
}

type rawConstraints struct {
	SupportsDomainExact   bool `yaml:"supports_domain_exact"`
	SupportsDomainSuffix  bool `yaml:"supports_domain_suffix"`
	SupportsDynamicDNSSet bool `yaml:"supports_dynamic_dns_set"`
	SupportsIPv4          bool `yaml:"supports_ipv4"`
	SupportsIPv6          bool `yaml:"supports_ipv6"`
	SupportsPrefixes      bool `yaml:"supports_prefixes"`
	MaxRules              int  `yaml:"max_rules"`
	MaxArtifactSize       int  `yaml:"max_artifact_size"`
	MaxEntriesPerList     int  `yaml:"max_entries_per_list"`
	MaxLists              int  `yaml:"max_lists"`
}

func loadTargets(ctx context.Context, root string) (map[string]domain.TargetDefinition, string, error) {
	paths, err := targetFiles(ctx, root)
	if err != nil {
		return nil, "", err
	}
	targets := make(map[string]domain.TargetDefinition, len(paths))
	total := int64(0)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		payload, err := boundedCatalogRead(path, MaxTargetFileBytes, &total, MaxTargetTotalBytes, "target", "target catalog")
		if err != nil {
			return nil, "", err
		}
		target, err := decodeTarget(payload)
		if err != nil {
			return nil, "", err
		}
		if _, collision := targets[target.ID]; collision {
			return nil, "", fmt.Errorf("%w: duplicate target id %q", ErrInvalidCatalog, target.ID)
		}
		targets[target.ID] = target
	}
	if len(targets) == 0 {
		return targets, "", nil
	}
	revision, err := targetCatalogRevision(targets)
	if err != nil {
		return nil, "", fmt.Errorf("%w: canonical target revision", ErrInvalidCatalog)
	}
	return targets, revision, nil
}

func targetFiles(ctx context.Context, root string) ([]string, error) {
	return optionalYAMLFiles(ctx, root, "targets", "target", MaxTargetFiles)
}

func decodeTarget(payload []byte) (domain.TargetDefinition, error) {
	raw, err := decodeStrictYAML[rawTarget](payload, "empty target YAML", "decode target YAML", "target YAML must contain exactly one document", "unknown or malformed target YAML field")
	if err != nil {
		return domain.TargetDefinition{}, err
	}
	return normalizeTarget(raw)
}

func normalizeTarget(raw rawTarget) (domain.TargetDefinition, error) {
	formatKey := raw.FormatKey
	if raw.RetiredFormatKey != "" {
		if raw.FormatKey != "" {
			return domain.TargetDefinition{}, fmt.Errorf("%w: target %q names both format_key and the retired profile_key", ErrInvalidCatalog, raw.ID)
		}
		formatKey = raw.RetiredFormatKey
	}
	if domain.ValidateSlug(raw.ID) != nil || domain.ValidateSlug(formatKey) != nil || domain.ValidateSlug(raw.Renderer) != nil {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid target identity", ErrInvalidCatalog)
	}
	if len(raw.RendererOptions) != 0 {
		return domain.TargetDefinition{}, fmt.Errorf("%w: renderer options are not supported", ErrInvalidCatalog)
	}
	// A title is optional so a target file written before this field, or by a
	// plugin author who did not supply one, stays loadable. The identity is the
	// honest fallback: it is what the operator selected on the command line.
	title := strings.TrimSpace(raw.Title)
	if title == "" {
		title = raw.ID
	}
	if len(title) > 64 || containsControl(title) {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid target title", ErrInvalidCatalog)
	}
	hint := strings.TrimSpace(raw.ManualInstallationHint)
	if hint == "" || len(hint) > 1024 || containsControl(hint) {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid manual installation hint", ErrInvalidCatalog)
	}
	// The English name and instruction hold to the same bounds as the catalog's
	// own language: a translation is the same sentence, not a second field with
	// its own budget.
	titleEN, ok := optionalTargetText(raw.TitleEN, 64)
	if !ok {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid English target title", ErrInvalidCatalog)
	}
	hintEN, ok := optionalTargetText(raw.ManualInstallationHintEN, 1024)
	if !ok {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid English manual installation hint", ErrInvalidCatalog)
	}
	// The kind is required and closed: a screen groups routers apart from
	// applications, and there is no honest fallback to guess from.
	kind := strings.TrimSpace(raw.Kind)
	if kind != domain.TargetKindRouter && kind != domain.TargetKindApp {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid target kind", ErrInvalidCatalog)
	}
	constraints := domain.TargetConstraints{
		SupportsDomainExact:   raw.Constraints.SupportsDomainExact,
		SupportsDomainSuffix:  raw.Constraints.SupportsDomainSuffix,
		SupportsDynamicDNSSet: raw.Constraints.SupportsDynamicDNSSet,
		SupportsIPv4:          raw.Constraints.SupportsIPv4,
		SupportsIPv6:          raw.Constraints.SupportsIPv6,
		SupportsPrefixes:      raw.Constraints.SupportsPrefixes,
		MaxRules:              raw.Constraints.MaxRules,
		MaxArtifactSize:       raw.Constraints.MaxArtifactSize,
		MaxEntriesPerList:     raw.Constraints.MaxEntriesPerList,
		MaxLists:              raw.Constraints.MaxLists,
	}
	// The two list bounds are one notion: a device either holds named lists or
	// it does not, and declaring half of it would leave the split unbounded on
	// one side.
	if (constraints.MaxEntriesPerList == 0) != (constraints.MaxLists == 0) {
		return domain.TargetDefinition{}, fmt.Errorf("%w: a target declares both list bounds or neither", ErrInvalidCatalog)
	}
	if constraints.MaxEntriesPerList < 0 || constraints.MaxLists < 0 || constraints.MaxEntriesPerList > 100000 || constraints.MaxLists > 100000 {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid target list bounds", ErrInvalidCatalog)
	}
	if (!constraints.SupportsDomainExact && !constraints.SupportsDomainSuffix && !constraints.SupportsIPv4 && !constraints.SupportsIPv6) || constraints.MaxRules <= 0 || constraints.MaxRules > 100000 || constraints.MaxArtifactSize <= 0 || constraints.MaxArtifactSize > 16<<20 {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid target constraints", ErrInvalidCatalog)
	}
	return domain.TargetDefinition{ID: raw.ID, FormatKey: formatKey, Title: title, TitleEN: titleEN, Kind: kind, RendererID: raw.Renderer, Constraints: constraints, RendererOptions: []string{}, ManualInstallationHint: hint, ManualInstallationHintEN: hintEN}, nil
}

// optionalTargetText normalizes one catalog field a file may omit entirely.
// A nil pointer is the absent state and answers with the empty string; a key
// that is present carries the same grammar the required fields do, so a blank
// or oversized translation is refused where it was written instead of reaching
// a screen as an empty name.
func optionalTargetText(value *string, limit int) (string, bool) {
	if value == nil {
		return "", true
	}
	text := strings.TrimSpace(*value)
	if text == "" || len(text) > limit || containsControl(text) {
		return "", false
	}
	return text, true
}
