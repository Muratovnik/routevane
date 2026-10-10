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
	"bytes"
	"encoding/csv"
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
	// The third column deliberately repeats the plan's own key, which is
	// still service_id (ADR 0039). Renaming the column alone would make this
	// format disagree with the document it copies, and renaming both is a
	// new formatVersion rather than a rewording.
	header = "kind,value,service,component\n"
)

// plan is the part of the canonical plan document this renderer reads. Ignoring
// the rest is deliberate: a renderer that only needs rules should not break when
// unrelated diagnostics are added to the document.
type plan struct {
	Rules []struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
		// The plan's key for a list is still service_id: the plan format is
		// versioned separately from the product vocabulary (ADR 0039).
		List      string `json:"service_id"`
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
	return renderRows(rows), nil
}

func renderRows(rows []string) []byte {
	var builder strings.Builder
	builder.WriteString(header)
	for _, row := range rows {
		builder.WriteString(row)
		builder.WriteString("\n")
	}
	return []byte(builder.String())
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
func (handler) Validate(payload []byte) error {
	text := string(payload)
	if !strings.HasPrefix(text, header) {
		return fmt.Errorf("missing header")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, header)))
	reader.FieldsPerRecord = 4
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("invalid CSV: %w", err)
	}
	rows, err := canonicalRows(records)
	if err != nil {
		return err
	}
	if !bytes.Equal(payload, renderRows(rows)) {
		return fmt.Errorf("CSV is not canonical")
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
	records := make([][]string, 0, len(decoded.Rules))
	for _, rule := range decoded.Rules {
		records = append(records, []string{rule.Kind, rule.Value, rule.List, rule.Component})
	}
	return canonicalRows(records)
}

func canonicalRows(records [][]string) ([]string, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("no rows")
	}
	rows := make([]string, 0, len(records))
	seen := map[string]struct{}{}
	for index, fields := range records {
		if len(fields) != 4 {
			return nil, fmt.Errorf("row %d has %d fields", index, len(fields))
		}
		for _, field := range fields {
			if field == "" || strings.TrimSpace(field) != field {
				return nil, fmt.Errorf("row %d has an empty or padded field", index)
			}
			// Keep one physical line per rule. In particular, do not let the
			// CSV reader's CRLF normalization change a field's value.
			if strings.ContainsAny(field, "\r\n") {
				return nil, fmt.Errorf("row %d has a line break in a field", index)
			}
		}
		var builder strings.Builder
		writer := csv.NewWriter(&builder)
		if err := writer.Write(fields); err != nil {
			return nil, fmt.Errorf("write row %d: %w", index, err)
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, fmt.Errorf("write row %d: %w", index, err)
		}
		row := strings.TrimSuffix(builder.String(), "\n")
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
