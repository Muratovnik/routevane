package discovery

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

var (
	ErrInvalidHAR = errors.New("HAR file is not a usable archive")
	ErrHARTooBig  = errors.New("HAR file exceeds the import bound")
)

// HAR import bounds. An archive is user-supplied hostile input.
const (
	MaxHARBytes   = 64 << 20
	MaxHAREntries = 20000
)

// harArchive models only the fields this import consumes. Unknown fields are
// ignored so an archive from any tool that follows the format still imports.
type harArchive struct {
	Log struct {
		Version string     `json:"version"`
		Pages   []harPage  `json:"pages"`
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harPage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Comment carries the component attribution when a capture tool cannot
	// express steps. Routevane writes its own step id here when it exports.
	Comment string `json:"comment"`
}

type harEntry struct {
	PageRef  string      `json:"pageref"`
	Request  harRequest  `json:"request"`
	Response harResponse `json:"response"`
}

type harRequest struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers []harHeader `json:"headers"`
}

type harResponse struct {
	Status      int         `json:"status"`
	RedirectURL string      `json:"redirectURL"`
	Headers     []harHeader `json:"headers"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ImportHAR turns an archive into the same evidence shape a live session
// produces. Component attribution comes from the page a request belongs to: a
// page whose comment or title names a component in the taxonomy attributes every
// request it initiated. Anything unattributed stays unattributed rather than
// being guessed, which keeps the activation policy deterministic.
func ImportHAR(target Target, payload []byte) (SessionEvidence, error) {
	if len(payload) == 0 {
		return SessionEvidence{}, ErrInvalidHAR
	}
	if len(payload) > MaxHARBytes {
		return SessionEvidence{}, ErrHARTooBig
	}
	var archive harArchive
	if err := json.Unmarshal(payload, &archive); err != nil {
		return SessionEvidence{}, fmt.Errorf("%w: %v", ErrInvalidHAR, err)
	}
	if len(archive.Log.Entries) == 0 {
		return SessionEvidence{}, fmt.Errorf("%w: no entry", ErrInvalidHAR)
	}
	if len(archive.Log.Entries) > MaxHAREntries {
		return SessionEvidence{}, ErrHARTooBig
	}
	pageComponent := map[string]string{}
	pageHost := map[string]string{}
	steps := map[string]struct{}{}
	for _, page := range archive.Log.Pages {
		component := componentFromLabel(page.Comment)
		if component == "" {
			component = componentFromLabel(page.Title)
		}
		if component != "" {
			pageComponent[page.ID] = component
			steps[component] = struct{}{}
		}
	}
	// A page's own document host is the loader for the requests it initiated.
	for _, entry := range archive.Log.Entries {
		if entry.PageRef == "" {
			continue
		}
		if _, known := pageHost[entry.PageRef]; known {
			continue
		}
		if host, ok := hostOfURL(entry.Request.URL); ok && isDocument(entry) {
			pageHost[entry.PageRef] = host
		}
	}

	byHost := map[string]*HostEvidence{}
	requests := 0
	for _, entry := range archive.Log.Entries {
		host, ok := hostOfURL(entry.Request.URL)
		if !ok {
			continue
		}
		requests++
		evidence, known := byHost[host]
		if !known {
			evidence = &HostEvidence{Host: host}
			byHost[host] = evidence
		}
		evidence.Requests++
		if component := pageComponent[entry.PageRef]; component != "" {
			evidence.Component = strongerComponent(evidence.Component, component)
			evidence.StepIDs = domain.StableStrings(append(evidence.StepIDs, component))
		}
		if loader, hasLoader := pageHost[entry.PageRef]; hasLoader && loader != host {
			evidence.LoadedBy = domain.StableStrings(append(evidence.LoadedBy, loader))
		}
		if redirect := redirectTarget(entry); redirect != "" && redirect != host {
			evidence.RedirectsTo = domain.StableStrings(append(evidence.RedirectsTo, redirect))
		}
	}
	if len(byHost) == 0 {
		return SessionEvidence{}, fmt.Errorf("%w: no usable request", ErrInvalidHAR)
	}
	hosts := make([]HostEvidence, 0, len(byHost))
	for _, evidence := range byHost {
		hosts = append(hosts, *evidence)
	}
	slices.SortFunc(hosts, func(a, b HostEvidence) int { return cmp.Compare(a.Host, b.Host) })
	return SessionEvidence{Target: target, Steps: slices.Sorted(maps.Keys(steps)), Hosts: hosts, Requests: requests}, nil
}

// componentFromLabel accepts only an exact taxonomy identity. A fuzzy match
// would make the activation policy depend on how a capture tool names a page.
func componentFromLabel(label string) string {
	candidate := strings.ToLower(strings.TrimSpace(label))
	if domain.KnownComponent(candidate) {
		return candidate
	}
	return ""
}

func hostOfURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	normalized, err := domain.NormalizeDomain(host)
	if err != nil {
		return "", false
	}
	return normalized, true
}

// isDocument reports whether an entry looks like the page's own document rather
// than a subresource, using the request's Accept header only.
func isDocument(entry harEntry) bool {
	if !strings.EqualFold(entry.Request.Method, "GET") {
		return false
	}
	for _, header := range entry.Request.Headers {
		if strings.EqualFold(header.Name, "accept") && strings.Contains(strings.ToLower(header.Value), "text/html") {
			return true
		}
	}
	return false
}

func redirectTarget(entry harEntry) string {
	if entry.Response.Status < 300 || entry.Response.Status > 399 {
		return ""
	}
	location := entry.Response.RedirectURL
	if location == "" {
		for _, header := range entry.Response.Headers {
			if strings.EqualFold(header.Name, "location") {
				location = header.Value
				break
			}
		}
	}
	if location == "" {
		return ""
	}
	if strings.HasPrefix(location, "/") {
		// A relative redirect stays on the same host and adds no dependency.
		return ""
	}
	host, ok := hostOfURL(location)
	if !ok {
		return ""
	}
	return host
}
