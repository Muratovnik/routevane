// Package catalogyaml loads one strict, bounded snapshot of the service
// catalog. It is a concrete boundary: there is no catalog registry or SDK.
package catalogyaml

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"go.yaml.in/yaml/v3"
)

const (
	MaxFileBytes        = 256 << 10
	MaxTotalBytes       = 4 << 20
	MaxFiles            = 1000
	MaxListItems        = 128
	MaxTargetFileBytes  = 64 << 10
	MaxTargetTotalBytes = 512 << 10
	MaxTargetFiles      = 64

	MaxCategoryFileBytes  = 16 << 10
	MaxCategoryTotalBytes = 256 << 10
	MaxCategoryFiles      = 128
)

var ErrInvalidCatalog = errors.New("invalid catalog")

type Catalog struct {
	Root           string
	Revision       string
	TargetRevision string
	Lists          map[string]domain.ListDefinition
	Categories     map[string]domain.CategoryDefinition
	Targets        map[string]domain.TargetDefinition
	// LocalListIDs names entries loaded from catalog/local.  Those entries
	// are intentionally workstation-local discoveries, so a portable settings
	// transfer must never make a destination depend on one being present.
	LocalListIDs map[string]struct{}
}

func (c Catalog) List(id string) (domain.ListDefinition, bool) {
	list, ok := c.Lists[id]
	return list, ok
}

func (c Catalog) Category(id string) (domain.CategoryDefinition, bool) {
	category, ok := c.Categories[id]
	return category, ok
}

func (c Catalog) Target(id string) (domain.TargetDefinition, bool) {
	target, ok := c.Targets[id]
	return target, ok
}

func (c Catalog) ActiveSourceRevisions(listID string) map[string]string {
	list, ok := c.Lists[listID]
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(list.Sources))
	for _, source := range list.Sources {
		out[source.ID] = source.Revision
	}
	return out
}

func Load(ctx context.Context, rootPath string) (Catalog, error) {
	if ctx == nil {
		return Catalog{}, fmt.Errorf("%w: nil context", ErrInvalidCatalog)
	}
	root, err := filesystem.ResolveRoot(rootPath)
	if err != nil {
		return Catalog{}, fmt.Errorf("%w: catalog root", ErrInvalidCatalog)
	}
	lists, localListIDs, err := loadLists(ctx, root)
	if err != nil {
		return Catalog{}, err
	}
	categories, err := loadCategories(ctx, root, lists)
	if err != nil {
		return Catalog{}, err
	}
	// Categories take part in the revision because a list resolves its
	// composition through them: a category that gains a service changes what
	// the next plan is built from, and a snapshot that recorded the old
	// revision would claim otherwise.
	revision, err := catalogRevision(lists, categories)
	if err != nil {
		return Catalog{}, fmt.Errorf("%w: canonical revision", ErrInvalidCatalog)
	}
	for id, list := range lists {
		list.CatalogRevision = revision
		lists[id] = list
	}
	targets, targetRevision, err := loadTargets(ctx, root)
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Root: root, Revision: revision, TargetRevision: targetRevision, Lists: lists, Categories: categories, Targets: targets, LocalListIDs: localListIDs}, nil
}

// optionalYAMLFiles lists one bounded catalog subdirectory. A missing directory
// is an empty catalog, not a failure: a checkout may legitimately ship services
// with no categories and no targets of its own.
func optionalYAMLFiles(ctx context.Context, root, dirName, label string, maxFiles int) ([]string, error) {
	dir := filepath.Join(root, dirName)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil || !info.IsDir() || filesystem.IsLinkOrReparse(info) {
		return nil, fmt.Errorf("%w: %s catalog directory is unsafe", ErrInvalidCatalog, label)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s catalog directory", ErrInvalidCatalog, label)
	}
	paths := make([]string, 0, len(entries))
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
			return nil, fmt.Errorf("%w: %s YAML entry is not a regular file", ErrInvalidCatalog, label)
		}
		paths = append(paths, path)
		if len(paths) > maxFiles {
			return nil, fmt.Errorf("%w: %s catalog exceeds %d files", ErrInvalidCatalog, label, maxFiles)
		}
	}
	slices.Sort(paths)
	return paths, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("file exceeds bound")
	}
	return payload, nil
}

// boundedCatalogRead validates and reads one catalog-family file: it refuses a
// non-regular or linked entry, refuses a file over its family's per-file bound,
// reads it through readBounded, and adds its size to the caller's running total
// against the family's total bound. Load, loadCategories, and loadTargets share
// this one four-step contract instead of each repeating it with its own
// constants, so a change to the contract cannot be applied to two families and
// missed on the third.
//
// label names the entry in the not-a-regular-file, too-large, and read-failure
// messages ("catalog", "category", "target"). totalNoun names the family in the
// running-total message; it differs from label for the running total historically
// read as "catalog exceeds N bytes" rather than "catalog catalog exceeds N bytes".
func boundedCatalogRead(path string, fileLimit int64, running *int64, totalLimit int64, label, totalNoun string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || filesystem.IsLinkOrReparse(info) {
		return nil, fmt.Errorf("%w: %s entry is not a regular file", ErrInvalidCatalog, label)
	}
	if info.Size() > fileLimit {
		return nil, fmt.Errorf("%w: %s file exceeds %d bytes", ErrInvalidCatalog, label, fileLimit)
	}
	payload, err := readBounded(path, fileLimit)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s file", ErrInvalidCatalog, label)
	}
	*running += int64(len(payload))
	if *running > totalLimit {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidCatalog, totalNoun, totalLimit)
	}
	return payload, nil
}

// decodeStrictYAML runs the four-step hostile-YAML pipeline every catalog
// document goes through: decode into a yaml.Node, reject anything validateNode
// refuses, refuse a second document in the same file, then decode again with
// KnownFields(true) so an unrecognized field is a refusal rather than a silent
// drop. decodeList, decodeTarget, and decodeCategory differ only in the
// target type T and their error wording, which is exactly what this generic
// helper factors out: a change to the strict-parsing policy now has one
// implementation to change instead of three copies that could quietly drift.
func decodeStrictYAML[T any](payload []byte, emptyMsg, decodeMsg, multiDocMsg, unknownFieldMsg string) (T, error) {
	var zero T
	if len(payload) == 0 {
		return zero, fmt.Errorf("%w: %s", ErrInvalidCatalog, emptyMsg)
	}
	nodeDecoder := yaml.NewDecoder(bytes.NewReader(payload))
	var document yaml.Node
	if err := nodeDecoder.Decode(&document); err != nil {
		return zero, fmt.Errorf("%w: %s", ErrInvalidCatalog, decodeMsg)
	}
	if err := validateNode(&document); err != nil {
		return zero, err
	}
	var extra yaml.Node
	if err := nodeDecoder.Decode(&extra); err != io.EOF {
		return zero, fmt.Errorf("%w: %s", ErrInvalidCatalog, multiDocMsg)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	decoder.KnownFields(true)
	var raw T
	if err := decoder.Decode(&raw); err != nil {
		return zero, fmt.Errorf("%w: %s", ErrInvalidCatalog, unknownFieldMsg)
	}
	return raw, nil
}

func validateNode(node *yaml.Node) error {
	if node == nil {
		return fmt.Errorf("%w: empty YAML node", ErrInvalidCatalog)
	}
	if node.Kind == yaml.AliasNode || node.Alias != nil || node.Anchor != "" {
		return fmt.Errorf("%w: YAML aliases and anchors are forbidden", ErrInvalidCatalog)
	}
	if node.Tag != "" && !isCoreTag(node.Tag) {
		return fmt.Errorf("%w: custom YAML tags are forbidden", ErrInvalidCatalog)
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Value == "<<" {
				return fmt.Errorf("%w: merge or complex YAML keys are forbidden", ErrInvalidCatalog)
			}
			identity := key.Tag + "\x00" + key.Value
			if _, exists := seen[identity]; exists {
				return fmt.Errorf("%w: duplicate YAML key %q", ErrInvalidCatalog, key.Value)
			}
			seen[identity] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := validateNode(child); err != nil {
			return err
		}
	}
	return nil
}

func isCoreTag(tag string) bool {
	switch tag {
	case "!!map", "!!seq", "!!str", "!!bool", "!!int", "!!null", "!!float", "!!timestamp",
		"tag:yaml.org,2002:map", "tag:yaml.org,2002:seq", "tag:yaml.org,2002:str", "tag:yaml.org,2002:bool", "tag:yaml.org,2002:int", "tag:yaml.org,2002:null", "tag:yaml.org,2002:float", "tag:yaml.org,2002:timestamp":
		return true
	default:
		return false
	}
}

func containsControl(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func catalogRevision(lists map[string]domain.ListDefinition, categories map[string]domain.CategoryDefinition) (string, error) {
	ids := slices.Sorted(maps.Keys(lists))
	ordered := make([]domain.ListDefinition, 0, len(ids))
	for _, id := range ids {
		list := lists[id]
		list.CatalogRevision = ""
		ordered = append(ordered, list)
	}
	categoryIDs := slices.Sorted(maps.Keys(categories))
	orderedCategories := make([]domain.CategoryDefinition, 0, len(categoryIDs))
	for _, id := range categoryIDs {
		orderedCategories = append(orderedCategories, categories[id])
	}
	encoded, err := json.Marshal(struct {
		Lists      []domain.ListDefinition
		Categories []domain.CategoryDefinition
	}{ordered, orderedCategories})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func targetCatalogRevision(targets map[string]domain.TargetDefinition) (string, error) {
	ids := slices.Sorted(maps.Keys(targets))
	ordered := make([]domain.TargetDefinition, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, targets[id])
	}
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}
