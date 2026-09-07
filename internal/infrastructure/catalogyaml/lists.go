package catalogyaml

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/sources/dns"
	"github.com/Muratovnik/routevane/internal/sources/httpfeed"
)

type rawList struct {
	ID         string                  `yaml:"id"`
	Title      string                  `yaml:"title"`
	Components map[string]rawComponent `yaml:"components"`
	Seeds      []rawSeed               `yaml:"seeds"`
	Sources    []rawSource             `yaml:"sources"`
}

type rawComponent struct {
	Required bool `yaml:"required"`
}

type rawSeed struct {
	Kind      domain.RuleKind `yaml:"kind"`
	Value     string          `yaml:"value"`
	Component string          `yaml:"component"`
	Source    string          `yaml:"source"`
}

type rawSource struct {
	ID        string            `yaml:"id"`
	Type      domain.SourceType `yaml:"type"`
	Revision  string            `yaml:"revision,omitempty"`
	Component string            `yaml:"component"`
	Config    rawSourceConfig   `yaml:"config"`
}

type rawSourceConfig struct {
	Names  []string           `yaml:"names"`
	URL    string             `yaml:"url"`
	Format domain.FeedFormat  `yaml:"format"`
	Class  domain.SourceClass `yaml:"class"`
}

func loadLists(ctx context.Context, root string) (map[string]domain.ListDefinition, map[string]struct{}, error) {
	paths, err := catalogFiles(ctx, root)
	if err != nil {
		return nil, nil, err
	}
	lists := make(map[string]domain.ListDefinition, len(paths))
	local := make(map[string]struct{})
	total := int64(0)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		payload, err := boundedCatalogRead(path, MaxFileBytes, &total, MaxTotalBytes, "catalog", "catalog")
		if err != nil {
			return nil, nil, err
		}
		list, err := decodeList(payload)
		if err != nil {
			return nil, nil, err
		}
		if _, collision := lists[list.ID]; collision {
			return nil, nil, fmt.Errorf("%w: duplicate service id %q", ErrInvalidCatalog, list.ID)
		}
		lists[list.ID] = list
		if filepath.Base(filepath.Dir(path)) == LocalGroup {
			local[list.ID] = struct{}{}
		}
	}
	if len(lists) == 0 {
		return nil, nil, fmt.Errorf("%w: catalog contains no services", ErrInvalidCatalog)
	}
	return lists, local, nil
}

func catalogFiles(ctx context.Context, root string) ([]string, error) {
	paths := make([]string, 0)
	for _, group := range []string{"builtin", "local"} {
		dir := filepath.Join(root, group)
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || filesystem.IsLinkOrReparse(info) {
			return nil, fmt.Errorf("%w: %s catalog directory is unsafe", ErrInvalidCatalog, group)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("%w: read %s catalog directory", ErrInvalidCatalog, group)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if filepath.Ext(entry.Name()) != ".yaml" {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || filesystem.IsLinkOrReparse(info) {
				return nil, fmt.Errorf("%w: YAML entry is not a regular file", ErrInvalidCatalog)
			}
			paths = append(paths, path)
			if len(paths) > MaxFiles {
				return nil, fmt.Errorf("%w: catalog exceeds %d files", ErrInvalidCatalog, MaxFiles)
			}
		}
	}
	slices.Sort(paths)
	return paths, nil
}

func decodeList(payload []byte) (domain.ListDefinition, error) {
	raw, err := decodeStrictYAML[rawList](payload, "empty YAML", "decode YAML", "YAML must contain exactly one document", "unknown or malformed YAML field")
	if err != nil {
		return domain.ListDefinition{}, err
	}
	return normalizeList(raw)
}

func normalizeList(raw rawList) (domain.ListDefinition, error) {
	if domain.ValidateSlug(raw.ID) != nil || strings.TrimSpace(raw.Title) == "" || len(raw.Title) > 128 {
		return domain.ListDefinition{}, fmt.Errorf("%w: invalid service identity", ErrInvalidCatalog)
	}
	if len(raw.Components) == 0 || len(raw.Components) > MaxListItems || len(raw.Seeds) > MaxListItems || len(raw.Sources) > MaxListItems {
		return domain.ListDefinition{}, fmt.Errorf("%w: list bound exceeded", ErrInvalidCatalog)
	}
	definition := domain.ListDefinition{ID: raw.ID, Title: strings.TrimSpace(raw.Title)}
	componentIDs := make([]string, 0, len(raw.Components))
	for id := range raw.Components {
		if domain.ValidateSlug(id) != nil {
			return domain.ListDefinition{}, fmt.Errorf("%w: invalid component id", ErrInvalidCatalog)
		}
		componentIDs = append(componentIDs, id)
	}
	slices.Sort(componentIDs)
	components := make(map[string]struct{}, len(componentIDs))
	for _, id := range componentIDs {
		components[id] = struct{}{}
		definition.Components = append(definition.Components, domain.ComponentDefinition{ID: id, Required: raw.Components[id].Required})
	}
	seenSeeds := make(map[string]struct{}, len(raw.Seeds))
	for _, value := range raw.Seeds {
		if _, ok := components[value.Component]; !ok || (value.Source != "" && value.Source != "manual") {
			return domain.ListDefinition{}, fmt.Errorf("%w: invalid manual seed identity", ErrInvalidCatalog)
		}
		seed := domain.Seed{Kind: value.Kind, Value: value.Value, ComponentID: value.Component, SourceClass: domain.SourceManual}
		normalized, err := seed.Normalize()
		if err != nil {
			return domain.ListDefinition{}, fmt.Errorf("%w: invalid manual seed", ErrInvalidCatalog)
		}
		normalized.SourceID = "manual:" + string(normalized.Kind) + ":" + normalized.Value
		seedKey := strings.Join([]string{string(normalized.Kind), normalized.Value, normalized.ComponentID, normalized.SourceID}, "\x00")
		if _, exists := seenSeeds[seedKey]; exists {
			continue
		}
		seenSeeds[seedKey] = struct{}{}
		definition.Seeds = append(definition.Seeds, normalized)
	}
	slices.SortFunc(definition.Seeds, func(a, b domain.Seed) int {
		return cmp.Compare(strings.Join([]string{string(a.Kind), a.Value, a.ComponentID, a.SourceID}, "\x00"), strings.Join([]string{string(b.Kind), b.Value, b.ComponentID, b.SourceID}, "\x00"))
	})
	sourceIDs := make(map[string]struct{}, len(raw.Sources))
	for _, value := range raw.Sources {
		if domain.ValidateSlug(value.ID) != nil {
			return domain.ListDefinition{}, fmt.Errorf("%w: invalid source identity", ErrInvalidCatalog)
		}
		if _, duplicate := sourceIDs[value.ID]; duplicate {
			return domain.ListDefinition{}, fmt.Errorf("%w: duplicate source id", ErrInvalidCatalog)
		}
		sourceIDs[value.ID] = struct{}{}
		if _, ok := components[value.Component]; !ok {
			return domain.ListDefinition{}, fmt.Errorf("%w: unknown source component", ErrInvalidCatalog)
		}
		source, err := normalizeSource(value)
		if err != nil {
			return domain.ListDefinition{}, err
		}
		definition.Sources = append(definition.Sources, source)
		if source.Type == domain.SourceDNS {
			definition.DNSNames = append(definition.DNSNames, source.Names...)
		}
	}
	slices.SortFunc(definition.Sources, func(a, b domain.SourceDefinition) int { return cmp.Compare(a.ID, b.ID) })
	definition.DNSNames = domain.StableStrings(definition.DNSNames)
	return definition, nil
}

// normalizeSource validates built-ins against their closed configurations and
// external types against the plugin protocol's names-only request. Availability
// and manifest-revision matching are runtime composition checks because catalog
// loading remains independent of what the operator installed on this machine.
func normalizeSource(raw rawSource) (domain.SourceDefinition, error) {
	switch raw.Type {
	case domain.SourceDNS:
		if raw.Revision != "" || len(raw.Config.Names) == 0 || len(raw.Config.Names) > MaxListItems || raw.Config.URL != "" || raw.Config.Format != "" || raw.Config.Class != "" {
			return domain.SourceDefinition{}, fmt.Errorf("%w: invalid DNS source config", ErrInvalidCatalog)
		}
		names, err := normalizeSourceNames(raw.Config.Names)
		if err != nil {
			return domain.SourceDefinition{}, err
		}
		source := domain.SourceDefinition{ID: raw.ID, Type: raw.Type, ComponentID: raw.Component, Names: names}
		source.Revision = sourceRevision(source)
		return source, nil
	case domain.SourceHTTP:
		if raw.Revision != "" || len(raw.Config.Names) != 0 {
			return domain.SourceDefinition{}, fmt.Errorf("%w: HTTP feed source does not take DNS names", ErrInvalidCatalog)
		}
		switch raw.Config.Format {
		case domain.FeedFormatText, domain.FeedFormatJSON, domain.FeedFormatDomainList:
		default:
			return domain.SourceDefinition{}, fmt.Errorf("%w: invalid feed format", ErrInvalidCatalog)
		}
		// A feed says what it is. Omitting the class keeps the meaning every
		// feed had before it existed: the vendor's own publication.
		class := raw.Config.Class
		switch class {
		case domain.SourceOfficial, domain.SourceCommunity:
		case "":
			class = domain.SourceOfficial
		default:
			return domain.SourceDefinition{}, fmt.Errorf("%w: a feed is either official or community", ErrInvalidCatalog)
		}
		if err := httpfeed.ValidateURL(raw.Config.URL); err != nil {
			return domain.SourceDefinition{}, fmt.Errorf("%w: invalid feed URL: %v", ErrInvalidCatalog, err)
		}
		source := domain.SourceDefinition{ID: raw.ID, Type: raw.Type, ComponentID: raw.Component, URL: raw.Config.URL, Format: raw.Config.Format, Class: class}
		source.Revision = sourceRevision(source)
		return source, nil
	default:
		if domain.ValidateSlug(string(raw.Type)) != nil || domain.ValidateSlug(raw.Revision) != nil || len(raw.Config.Names) == 0 || len(raw.Config.Names) > MaxListItems || raw.Config.URL != "" || raw.Config.Format != "" || raw.Config.Class != "" {
			return domain.SourceDefinition{}, fmt.Errorf("%w: invalid external source config", ErrInvalidCatalog)
		}
		names, err := normalizeSourceNames(raw.Config.Names)
		if err != nil {
			return domain.SourceDefinition{}, err
		}
		source := domain.SourceDefinition{
			ID: raw.ID, Type: raw.Type, ComponentID: raw.Component, Names: names,
			Class: domain.SourceCommunity, ImplementationRevision: raw.Revision,
		}
		source.Revision = sourceRevision(source)
		return source, nil
	}
}

func normalizeSourceNames(rawNames []string) ([]string, error) {
	names := make([]string, 0, len(rawNames))
	seen := make(map[string]struct{}, len(rawNames))
	for _, rawName := range rawNames {
		name, err := domain.NormalizeDomain(rawName)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid source name", ErrInvalidCatalog)
		}
		if _, exists := seen[name]; !exists {
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// sourceRevision binds stored observations to the exact decoder and
// configuration that produced them. Changing either invalidates the previous
// observations instead of mixing two semantics under one identity.
func sourceRevision(source domain.SourceDefinition) string {
	implementation := dns.SourceRevision
	if source.Type == domain.SourceHTTP {
		implementation = httpfeed.SourceRevision
	} else if source.Type != domain.SourceDNS {
		implementation = source.ImplementationRevision
	}
	payload := struct {
		Implementation string             `json:"implementation"`
		ID             string             `json:"id"`
		Type           domain.SourceType  `json:"type"`
		Component      string             `json:"component"`
		Names          []string           `json:"names"`
		URL            string             `json:"url,omitempty"`
		Format         domain.FeedFormat  `json:"format,omitempty"`
		Class          domain.SourceClass `json:"class,omitempty"`
	}{implementation, source.ID, source.Type, source.ComponentID, source.Names, source.URL, source.Format, source.Class}
	encoded, _ := json.Marshal(payload)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
