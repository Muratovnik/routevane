// Command csv-renderer is an example external renderer.
//
// It is deliberately small: the point is to show the whole contract an author
// must satisfy — a manifest that matches the installed one, a canonical output,
// and a validator that reads back exactly what the renderer wrote.
//
// Build it and install it next to its manifest:
//
//	go build -o csv-renderer ./examples/plugins/csv-renderer
//	sha256sum csv-renderer            # put the digest in manifest.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	plugin "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

const (
	rendererID    = "example-csv"
	formatVersion = "example-csv-v1"
	header        = "kind,value,service,component\n"
)

// plan is the part of the canonical plan document this renderer reads. Ignoring
// the rest is deliberate: a renderer that only needs rules should not break when
// unrelated diagnostics are added to the document.
type plan struct {
	Rules []struct {
		Kind      string `json:"kind"`
		Value     string `json:"value"`
		Service   string `json:"service_id"`
		Component string `json:"component_id"`
	} `json:"rules"`
}

type handler struct{ plugin.Unimplemented }

func (handler) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:            "example-csv-renderer",
		Version:         "1.0.0",
		ProtocolVersion: plugin.ProtocolVersion,
		Kind:            plugin.KindRenderer,
		Permissions:     []plugin.Permission{plugin.PermissionRenderPlan},
		Renderer: &plugin.RendererManifest{
			ID:                 rendererID,
			FormatVersion:      formatVersion,
			ContentType:        "text/csv",
			FileExtension:      "csv",
			SupportedRuleKinds: []string{"domain_exact", "domain_suffix", "ipv4", "ipv6", "prefix4", "prefix6"},
		},
	}
}

func (handler) Render(document []byte) ([]byte, error) {
	rows, err := rowsOf(document)
	if err != nil {
		return nil, err
	}
	var builder strings.Builder
	builder.WriteString(header)
	for _, row := range rows {
		builder.WriteString(row)
		builder.WriteString("\n")
	}
	return []byte(builder.String()), nil
}

func (handler) ProjectedRuleCount(document []byte) (int, error) {
	rows, err := rowsOf(document)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// Validate reads the document back independently and re-renders it. Byte
// equality is what makes a non-canonical file impossible to publish.
func (h handler) Validate(payload []byte) error {
	text := string(payload)
	if !strings.HasPrefix(text, header) {
		return fmt.Errorf("missing header")
	}
	body := strings.TrimSuffix(strings.TrimPrefix(text, header), "\n")
	if body == "" {
		return fmt.Errorf("no rows")
	}
	rows := strings.Split(body, "\n")
	seen := map[string]struct{}{}
	for index, row := range rows {
		fields := strings.Split(row, ",")
		if len(fields) != 4 {
			return fmt.Errorf("row %d has %d fields", index, len(fields))
		}
		for _, field := range fields {
			if field == "" || strings.TrimSpace(field) != field {
				return fmt.Errorf("row %d has an empty or padded field", index)
			}
		}
		if _, duplicate := seen[row]; duplicate {
			return fmt.Errorf("row %d is a duplicate", index)
		}
		seen[row] = struct{}{}
		if index > 0 && rows[index-1] >= row {
			return fmt.Errorf("row %d is out of canonical order", index)
		}
	}
	return nil
}

func rowsOf(document []byte) ([]string, error) {
	var decoded plan
	if err := json.Unmarshal(document, &decoded); err != nil {
		return nil, fmt.Errorf("plan document is not usable: %w", err)
	}
	if len(decoded.Rules) == 0 {
		return nil, fmt.Errorf("plan has no rules")
	}
	rows := make([]string, 0, len(decoded.Rules))
	seen := map[string]struct{}{}
	for _, rule := range decoded.Rules {
		if rule.Kind == "" || rule.Value == "" || rule.Service == "" || rule.Component == "" {
			return nil, fmt.Errorf("plan rule is incomplete")
		}
		row := strings.Join([]string{rule.Kind, rule.Value, rule.Service, rule.Component}, ",")
		if _, duplicate := seen[row]; duplicate {
			continue
		}
		seen[row] = struct{}{}
		rows = append(rows, row)
	}
	sort.Strings(rows)
	return rows, nil
}

func main() {
	if err := plugin.Serve(handler{}); err != nil {
		fmt.Fprintln(os.Stderr, "example-csv-renderer stopped:", err)
		os.Exit(1)
	}
}
