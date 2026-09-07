package catalogyaml

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

type rawCategory struct {
	ID    string   `yaml:"id"`
	Title string   `yaml:"title"`
	Lists []string `yaml:"lists"`
	// RetiredLists is the key this format used before ADR 0039 renamed it. It
	// read for one minor version so an operator's edited catalog survives the
	// upgrade; a file naming both is refused rather than merged, because the
	// two would have to be reconciled and neither is more authoritative.
	RetiredLists []string `yaml:"services"`
}

func loadCategories(ctx context.Context, root string, lists map[string]domain.ListDefinition) (map[string]domain.CategoryDefinition, error) {
	paths, err := categoryFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	categories := make(map[string]domain.CategoryDefinition, len(paths))
	total := int64(0)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		payload, err := boundedCatalogRead(path, MaxCategoryFileBytes, &total, MaxCategoryTotalBytes, "category", "category catalog")
		if err != nil {
			return nil, err
		}
		category, err := decodeCategory(payload)
		if err != nil {
			return nil, err
		}
		if _, collision := categories[category.ID]; collision {
			return nil, fmt.Errorf("%w: duplicate category id %q", ErrInvalidCatalog, category.ID)
		}
		// A category naming a list that does not exist would resolve to a
		// silently smaller list. The catalog is refused instead.
		for _, listID := range category.Lists {
			if _, known := lists[listID]; !known {
				return nil, fmt.Errorf("%w: category %q names unknown list %q", ErrInvalidCatalog, category.ID, listID)
			}
		}
		categories[category.ID] = category
	}
	return categories, nil
}

func categoryFiles(ctx context.Context, root string) ([]string, error) {
	return optionalYAMLFiles(ctx, root, "categories", "category", MaxCategoryFiles)
}

func decodeCategory(payload []byte) (domain.CategoryDefinition, error) {
	raw, err := decodeStrictYAML[rawCategory](payload, "empty category YAML", "decode category YAML", "category YAML must contain exactly one document", "unknown or malformed category YAML field")
	if err != nil {
		return domain.CategoryDefinition{}, err
	}
	return normalizeCategory(raw)
}

func normalizeCategory(raw rawCategory) (domain.CategoryDefinition, error) {
	title := strings.TrimSpace(raw.Title)
	if domain.ValidateSlug(raw.ID) != nil || title == "" || len(title) > 64 || containsControl(title) {
		return domain.CategoryDefinition{}, fmt.Errorf("%w: invalid category identity", ErrInvalidCatalog)
	}
	members := raw.Lists
	if raw.RetiredLists != nil {
		if raw.Lists != nil {
			return domain.CategoryDefinition{}, fmt.Errorf("%w: category %q names both lists and the retired services key", ErrInvalidCatalog, raw.ID)
		}
		members = raw.RetiredLists
	}
	if len(members) == 0 || len(members) > MaxListItems {
		return domain.CategoryDefinition{}, fmt.Errorf("%w: category must name between 1 and %d lists", ErrInvalidCatalog, MaxListItems)
	}
	seen := make(map[string]struct{}, len(members))
	lists := make([]string, 0, len(members))
	for _, listID := range members {
		if domain.ValidateSlug(listID) != nil {
			return domain.CategoryDefinition{}, fmt.Errorf("%w: invalid list id in category %q", ErrInvalidCatalog, raw.ID)
		}
		if _, duplicate := seen[listID]; duplicate {
			return domain.CategoryDefinition{}, fmt.Errorf("%w: duplicate list %q in category %q", ErrInvalidCatalog, listID, raw.ID)
		}
		seen[listID] = struct{}{}
		lists = append(lists, listID)
	}
	slices.Sort(lists)
	return domain.CategoryDefinition{ID: raw.ID, Title: title, Lists: lists}, nil
}
