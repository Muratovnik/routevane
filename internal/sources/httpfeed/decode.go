package httpfeed

import (
	"encoding/json"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

func decode(format domain.FeedFormat, body []byte) ([]domain.Resource, int, error) {
	switch format {
	case domain.FeedFormatText:
		return decodeText(body)
	case domain.FeedFormatJSON:
		return decodeJSON(body)
	case domain.FeedFormatDomainList:
		return decodeDomainList(body)
	default:
		return nil, 0, ErrUnsupportedFormat
	}
}

func decodeText(body []byte) ([]domain.Resource, int, error) {
	entries := make([]domain.Resource, 0, 64)
	skipped := 0
	for _, rawLine := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		resource, ok := parseEntry(line)
		if !ok {
			skipped++
			continue
		}
		if len(entries) >= MaxEntries {
			return nil, 0, ErrTooManyEntries
		}
		entries = append(entries, resource)
	}
	return entries, skipped, nil
}

// jsonFeed accepts the two shapes official feeds actually publish: a bare array
// of strings, or an object carrying a prefix list. Unknown sibling fields are
// ignored on purpose so a vendor adding metadata does not break a refresh.
type jsonFeed struct {
	Prefixes []jsonEntry `json:"prefixes"`
}

type jsonEntry struct {
	value string
}

func (e *jsonEntry) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		e.value = text
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}
	// The key set is closed so an unrelated string field cannot be mistaken for
	// a routable prefix.
	for _, key := range []string{"ip_prefix", "ipv6_prefix", "prefix", "cidr", "ip"} {
		encoded, ok := object[key]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(encoded, &text); err != nil {
			continue
		}
		if text != "" {
			e.value = text
			return nil
		}
	}
	return nil
}

func decodeJSON(body []byte) ([]domain.Resource, int, error) {
	trimmed := strings.TrimSpace(string(body))
	var raw []jsonEntry
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, 0, ErrMalformedFeed
		}
	} else {
		var feed jsonFeed
		if err := json.Unmarshal(body, &feed); err != nil {
			return nil, 0, ErrMalformedFeed
		}
		raw = feed.Prefixes
	}
	entries := make([]domain.Resource, 0, len(raw))
	skipped := 0
	for _, item := range raw {
		resource, ok := parseEntry(strings.TrimSpace(item.value))
		if !ok {
			skipped++
			continue
		}
		if len(entries) >= MaxEntries {
			return nil, 0, ErrTooManyEntries
		}
		entries = append(entries, resource)
	}
	return entries, skipped, nil
}

// parseEntry accepts one address, one prefix, or one domain name. An entry
// that does not normalize is refused; a feed never contributes an unchecked
// string. Domains are the shape the product prefers, and refusing them here is
// what previously made every domain-first source unusable.
func parseEntry(value string) (domain.Resource, bool) {
	if value == "" || len(value) > maxEntryBytes {
		return domain.Resource{}, false
	}
	if strings.Contains(value, "/") {
		resource, err := domain.NewPrefixResourceFromString(value)
		if err != nil || !resource.IsValid() {
			return domain.Resource{}, false
		}
		return resource, true
	}
	if resource, err := domain.NewAddrResourceFromString(value); err == nil && resource.IsValid() {
		return resource, true
	}
	// A name whose last label is all digits is a mistyped address, not a host.
	// Without this a feed of addresses would quietly publish its typos as
	// routing rules for names that cannot exist.
	if label := value[strings.LastIndexByte(value, '.')+1:]; label != "" && strings.Trim(label, "0123456789") == "" {
		return domain.Resource{}, false
	}
	resource, err := domain.NewDomainResource(value)
	if err != nil || !resource.IsValid() {
		return domain.Resource{}, false
	}
	return resource, true
}

// decodeDomainList reads the v2fly domain-list dialect strictly and partially.
// A bare name and an explicit "full:" name are taken; "keyword:" and "regexp:"
// are refused because this product routes names, not patterns; "include:" is
// not expanded because following it would pull in a list nobody chose. Every
// refusal is counted, so a skipped line is a diagnostic rather than a silence.
func decodeDomainList(body []byte) ([]domain.Resource, int, error) {
	entries := make([]domain.Resource, 0, 64)
	skipped := 0
	for _, rawLine := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if index := strings.IndexByte(line, '#'); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		if line == "" {
			continue
		}
		// An attribute suffix such as "@cn" narrows a name to one audience. The
		// name itself is still the name, so the attribute is dropped.
		if index := strings.IndexByte(line, ' '); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		if index := strings.IndexByte(line, '@'); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		value := line
		if rest, found := strings.CutPrefix(line, "full:"); found {
			value = strings.TrimSpace(rest)
		} else if strings.ContainsRune(line, ':') {
			// keyword:, regexp:, include:, domain: and anything else this
			// decoder does not implement.
			if rest, found := strings.CutPrefix(line, "domain:"); found {
				value = strings.TrimSpace(rest)
			} else {
				skipped++
				continue
			}
		}
		resource, err := domain.NewDomainResource(value)
		if err != nil || !resource.IsValid() {
			skipped++
			continue
		}
		if len(entries) >= MaxEntries {
			return nil, 0, ErrTooManyEntries
		}
		entries = append(entries, resource)
	}
	return entries, skipped, nil
}
