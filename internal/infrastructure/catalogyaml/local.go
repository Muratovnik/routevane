package catalogyaml

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

// ErrDraftExists reports that a list identity is already defined. A draft
// never overwrites an existing definition, built-in or local, because that would
// silently replace a reviewed list with an automatically derived one.
var ErrDraftExists = errors.New("list definition already exists")

// LocalGroup is the catalog directory automatically derived definitions live in.
const LocalGroup = "local"

// WriteLocalDraft serializes one derived list definition into
// `<catalog>/local/<id>.yaml` and returns its path.
//
// The written document is deliberately the same schema Load already accepts, so
// a draft is either loadable by the product or rejected at write time; there is
// no draft-only dialect.
func WriteLocalDraft(ctx context.Context, catalogRoot string, definition domain.ListDefinition) (string, error) {
	if ctx == nil || ctx.Err() != nil {
		return "", fmt.Errorf("%w: context", ErrInvalidCatalog)
	}
	if domain.ValidateSlug(definition.ID) != nil {
		return "", fmt.Errorf("%w: draft list id", ErrInvalidCatalog)
	}
	payload, err := encodeDraft(definition)
	if err != nil {
		return "", err
	}
	// A first draft may be the reason the catalog directory exists at all, so the
	// root is created before it is resolved. Resolution still refuses a root that
	// escapes through a link.
	if err := os.MkdirAll(filepath.Join(catalogRoot, LocalGroup), 0o700); err != nil {
		return "", fmt.Errorf("create local catalog directory: %w", err)
	}
	root, err := filesystem.ResolveRoot(catalogRoot)
	if err != nil {
		return "", fmt.Errorf("%w: catalog root", ErrInvalidCatalog)
	}
	// A definition that cannot be loaded back is never written.
	if err := verifyDraftLoads(ctx, payload, definition); err != nil {
		return "", err
	}
	name := definition.ID + ".yaml"
	for _, group := range []string{"builtin", LocalGroup} {
		if _, statErr := os.Lstat(filepath.Join(root, group, name)); statErr == nil {
			return "", fmt.Errorf("%w: %s/%s", ErrDraftExists, group, name)
		}
	}
	localDir := filepath.Join(root, LocalGroup)
	target := filepath.Join(localDir, name)
	temporary, err := os.CreateTemp(localDir, "."+definition.ID+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create draft temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err := temporary.Write(payload); err != nil {
		return "", fmt.Errorf("write draft: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync draft: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close draft: %w", err)
	}
	// O_EXCL semantics: Link fails rather than replacing an existing definition
	// that appeared between the check above and this commit.
	if err := os.Link(temporaryName, target); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%w: %s/%s", ErrDraftExists, LocalGroup, name)
		}
		return "", fmt.Errorf("commit draft: %w", err)
	}
	committed = true
	if err := os.Remove(temporaryName); err != nil {
		return "", fmt.Errorf("remove draft temporary file: %w", err)
	}
	return target, nil
}

// verifyDraftLoads parses the encoded draft through the same strict decoder the
// catalog uses, in an isolated temporary directory, before anything is written
// into the real catalog.
func verifyDraftLoads(ctx context.Context, payload []byte, definition domain.ListDefinition) error {
	probeRoot, err := os.MkdirTemp("", "routevane-draft-probe-")
	if err != nil {
		return fmt.Errorf("create draft probe: %w", err)
	}
	defer func() { _ = os.RemoveAll(probeRoot) }()
	if err := os.MkdirAll(filepath.Join(probeRoot, "builtin"), 0o700); err != nil {
		return fmt.Errorf("create draft probe group: %w", err)
	}
	if err := os.WriteFile(filepath.Join(probeRoot, "builtin", definition.ID+".yaml"), payload, 0o600); err != nil {
		return fmt.Errorf("write draft probe: %w", err)
	}
	catalog, err := Load(ctx, probeRoot)
	if err != nil {
		return fmt.Errorf("%w: draft is not loadable: %v", ErrInvalidCatalog, err)
	}
	loaded, found := catalog.List(definition.ID)
	if !found || loaded.ID != definition.ID || len(loaded.Sources) != len(definition.Sources) || len(loaded.Seeds) == 0 {
		return fmt.Errorf("%w: draft did not round-trip", ErrInvalidCatalog)
	}
	return nil
}

func encodeDraft(definition domain.ListDefinition) ([]byte, error) {
	if len(definition.Components) == 0 || len(definition.Seeds) == 0 {
		return nil, fmt.Errorf("%w: draft has no component or seed", ErrInvalidCatalog)
	}
	document := rawList{
		ID:         definition.ID,
		Title:      definition.Title,
		Components: map[string]rawComponent{},
	}
	for _, component := range definition.Components {
		if domain.ValidateSlug(component.ID) != nil {
			return nil, fmt.Errorf("%w: draft component id", ErrInvalidCatalog)
		}
		document.Components[component.ID] = rawComponent{Required: component.Required}
	}
	for _, seed := range definition.Seeds {
		if seed.Kind != domain.RuleDomainSuffix && seed.Kind != domain.RuleDomainExact {
			// A derived draft carries domains only. An address or a network from
			// a browser session would be exactly the widening this boundary
			// forbids.
			return nil, fmt.Errorf("%w: draft seed kind %q", ErrInvalidCatalog, seed.Kind)
		}
		normalized, err := seed.Normalize()
		if err != nil {
			return nil, fmt.Errorf("%w: draft seed value", ErrInvalidCatalog)
		}
		document.Seeds = append(document.Seeds, rawSeed{Kind: normalized.Kind, Value: normalized.Value, Component: normalized.ComponentID, Source: string(domain.SourceManual)})
	}
	slices.SortFunc(document.Seeds, func(a, b rawSeed) int {
		return cmp.Compare(string(a.Kind)+"\x00"+a.Value, string(b.Kind)+"\x00"+b.Value)
	})
	for _, source := range definition.Sources {
		if source.Type != domain.SourceDNS || len(source.Names) == 0 {
			return nil, fmt.Errorf("%w: draft source must be a DNS source with names", ErrInvalidCatalog)
		}
		names := append([]string(nil), source.Names...)
		slices.Sort(names)
		document.Sources = append(document.Sources, rawSource{ID: source.ID, Type: source.Type, Component: source.ComponentID, Config: rawSourceConfig{Names: names}})
	}
	slices.SortFunc(document.Sources, func(a, b rawSource) int { return cmp.Compare(a.ID, b.ID) })

	var builder strings.Builder
	builder.WriteString("# Generated by Routevane discovery. Review before use.\n")
	builder.WriteString("# Observed addresses are never stored here; only domains are.\n")
	encoder := yaml.NewEncoder(&builder)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode draft: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close draft encoder: %w", err)
	}
	payload := []byte(builder.String())
	if len(payload) > MaxFileBytes {
		return nil, fmt.Errorf("%w: draft exceeds the catalog file bound", ErrInvalidCatalog)
	}
	return payload, nil
}
