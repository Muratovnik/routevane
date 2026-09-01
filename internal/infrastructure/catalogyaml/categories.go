package catalogyaml

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

type rawCategory struct {
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Services []string `yaml:"services"`
}

func loadCategories(ctx context.Context, root string, services map[string]domain.ServiceDefinition) (map[string]domain.CategoryDefinition, error) {
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
		// A category naming a service that does not exist would resolve to a
		// silently smaller list. The catalog is refused instead.
		for _, serviceID := range category.Services {
			if _, known := services[serviceID]; !known {
				return nil, fmt.Errorf("%w: category %q names unknown service %q", ErrInvalidCatalog, category.ID, serviceID)
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
	if len(raw.Services) == 0 || len(raw.Services) > MaxListItems {
		return domain.CategoryDefinition{}, fmt.Errorf("%w: category must name between 1 and %d services", ErrInvalidCatalog, MaxListItems)
	}
	seen := make(map[string]struct{}, len(raw.Services))
	services := make([]string, 0, len(raw.Services))
	for _, serviceID := range raw.Services {
		if domain.ValidateSlug(serviceID) != nil {
			return domain.CategoryDefinition{}, fmt.Errorf("%w: invalid service id in category %q", ErrInvalidCatalog, raw.ID)
		}
		if _, duplicate := seen[serviceID]; duplicate {
			return domain.CategoryDefinition{}, fmt.Errorf("%w: duplicate service %q in category %q", ErrInvalidCatalog, serviceID, raw.ID)
		}
		seen[serviceID] = struct{}{}
		services = append(services, serviceID)
	}
	slices.Sort(services)
	return domain.CategoryDefinition{ID: raw.ID, Title: title, Services: services}, nil
}
