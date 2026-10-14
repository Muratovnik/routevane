package catalogyaml

import (
	"context"
	"fmt"
	"regexp"
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
	// A target gives its installation instruction as one sentence or as a list
	// of steps, never both. The list is a sequence rather than text with
	// numbers in it, so no reader has to take a line apart to number it.
	ManualInstallationSteps []string `yaml:"manual_installation_steps"`
	// The English pair is a pointer because absent and blank are different
	// states here. A target file written before these fields, or by a plugin
	// author who ships one language, is still valid; a key written with
	// nothing behind it is a mistake in the file and is refused rather than
	// normalized into the same silence.
	TitleEN                   *string  `yaml:"title_en"`
	ManualInstallationHintEN  *string  `yaml:"manual_installation_hint_en"`
	ManualInstallationStepsEN []string `yaml:"manual_installation_steps_en"`
}

// The bounds of an installation instruction. A sentence keeps the one-line
// bound it always had; a list is bounded by its steps, because a step is one
// thing to do and a long one is two.
const (
	maxInstallationHint  = 1024
	maxInstallationSteps = 12
	maxInstallationStep  = 512
)

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

func loadTargets(ctx context.Context, root string) (map[string]domain.TargetDefinition, error) {
	paths, err := targetFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]domain.TargetDefinition, len(paths))
	total := int64(0)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		payload, err := boundedCatalogRead(path, MaxTargetFileBytes, &total, MaxTargetTotalBytes, "target", "target catalog")
		if err != nil {
			return nil, err
		}
		target, err := decodeTarget(payload)
		if err != nil {
			return nil, err
		}
		if _, collision := targets[target.ID]; collision {
			return nil, fmt.Errorf("%w: duplicate target id %q", ErrInvalidCatalog, target.ID)
		}
		targets[target.ID] = target
	}
	return targets, nil
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
	steps, ok := installationSteps(raw.ManualInstallationHint, raw.ManualInstallationSteps)
	if !ok {
		return domain.TargetDefinition{}, fmt.Errorf("%w: a target gives its manual installation as one hint or as steps, and every one is a single line", ErrInvalidCatalog)
	}
	// The English name and instruction hold to the same bounds as the catalog's
	// own language: a translation is the same instruction, not a second field
	// with its own budget. Its steps are as many as the base steps.
	titleEN, ok := optionalTargetText(raw.TitleEN, 64)
	if !ok {
		return domain.TargetDefinition{}, fmt.Errorf("%w: invalid English target title", ErrInvalidCatalog)
	}
	var stepsEN []string
	if raw.ManualInstallationHintEN != nil || raw.ManualInstallationStepsEN != nil {
		hintEN := ""
		if raw.ManualInstallationHintEN != nil {
			hintEN = *raw.ManualInstallationHintEN
		}
		stepsEN, ok = installationSteps(hintEN, raw.ManualInstallationStepsEN)
		if !ok || len(stepsEN) != len(steps) {
			return domain.TargetDefinition{}, fmt.Errorf("%w: invalid English manual installation", ErrInvalidCatalog)
		}
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
	return domain.TargetDefinition{ID: raw.ID, FormatKey: formatKey, Title: title, TitleEN: titleEN, Kind: kind, RendererID: raw.Renderer, Constraints: constraints, RendererOptions: []string{}, ManualInstallationHint: strings.Join(steps, " "), ManualInstallationSteps: steps, ManualInstallationHintEN: strings.Join(stepsEN, " "), ManualInstallationStepsEN: stepsEN}, nil
}

// installationSteps reads one language of a target's manual installation: a
// single sentence or a list of steps, exactly one of them. Each entry is
// trimmed and holds one line. A step does not carry its own number, since the
// screen numbers the list and a second number would read as a nested one.
func installationSteps(hint string, steps []string) ([]string, bool) {
	hint = strings.TrimSpace(hint)
	if (hint == "") == (steps == nil) {
		return nil, false
	}
	if steps == nil {
		if len(hint) > maxInstallationHint || containsControl(hint) {
			return nil, false
		}
		return []string{hint}, true
	}
	if len(steps) == 0 || len(steps) > maxInstallationSteps {
		return nil, false
	}
	cleaned := make([]string, len(steps))
	for i, step := range steps {
		step = strings.TrimSpace(step)
		if step == "" || len(step) > maxInstallationStep || containsControl(step) || numberedStep.MatchString(step) {
			return nil, false
		}
		cleaned[i] = step
	}
	return cleaned, true
}

// numberedStep matches a step that begins with its own ordinal, such as «2. »,
// and not a number that belongs to the sentence, such as «5.0».
var numberedStep = regexp.MustCompile(`^\d+[.)]\s`)

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
