package application

import (
	"strings"
	"unicode"

	"github.com/Muratovnik/routevane/internal/domain"
)

const uncategorizedRouteLabel = "Без категории"

// routeLabelsByList derives presentation provenance from the same merged
// category view used to resolve a route. Titles, not storage identities, cross
// into the immutable plan so a device description remains meaningful to the
// operator.
func routeLabelsByList(definitions []domain.ListDefinition, categories []CategoryDetail) map[string][]string {
	titles := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		titles[definition.ID] = safeRouteLabelSegment(definition.Title)
	}
	categoryTitles := make(map[string][]string, len(definitions))
	for _, category := range categories {
		title := safeRouteLabelSegment(category.Title)
		for _, listID := range category.Lists {
			categoryTitles[listID] = append(categoryTitles[listID], title)
		}
	}
	result := make(map[string][]string, len(definitions))
	for _, definition := range definitions {
		listTitle := titles[definition.ID]
		if listTitle == "" {
			listTitle = "Без названия"
		}
		groups := domain.StableStrings(categoryTitles[definition.ID])
		if len(groups) == 0 {
			groups = []string{uncategorizedRouteLabel}
		}
		labels := make([]string, 0, len(groups))
		for _, group := range groups {
			if group == "" {
				group = uncategorizedRouteLabel
			}
			labels = append(labels, "("+group+"/"+listTitle+")")
		}
		result[definition.ID] = domain.StableStrings(labels)
	}
	return result
}

// safeRouteLabelSegment replaces characters that would be ambiguous with the
// product-owned (category/list) grammar. Unicode letters are preserved.
func safeRouteLabelSegment(value string) string {
	value = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return ' '
		case r == '/' || r == '\\':
			return '／'
		case r == '(':
			return '（'
		case r == ')':
			return '）'
		case r == '+':
			return '＋'
		default:
			return r
		}
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func applyRouteLabels(plan *domain.RoutingPlan, labels map[string][]string) {
	if plan == nil {
		return
	}
	for index := range plan.Rules {
		plan.Rules[index].Labels = append([]string(nil), labels[plan.Rules[index].ListID]...)
	}
}
