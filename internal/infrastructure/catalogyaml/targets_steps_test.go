package catalogyaml

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

// A target gives its installation instruction as one sentence or as a list of
// steps, and the loader serves both as a list. The shipped Keenetic domain-based
// target is the reason the list exists, so it is checked as shipped.
func TestTargetCatalogGivesInstallationAsOneHintOrAsSteps(t *testing.T) {
	const withoutHint = `id: keenetic
format_key: keenetic-bat-ipv4-v1
kind: router
renderer: keenetic-route-bat
constraints:
  supports_ipv4: true
  supports_prefixes: true
  max_rules: 1024
  max_artifact_size: 131072
renderer_options: []
`
	load := func(t *testing.T, payload string) (domain.TargetDefinition, error) {
		t.Helper()
		root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
		writeTargetFile(t, root, "keenetic.yaml", []byte(payload))
		catalog, err := Load(context.Background(), root)
		if err != nil {
			return domain.TargetDefinition{}, err
		}
		target, ok := catalog.Target("keenetic")
		if !ok {
			t.Fatal("the target was not loaded")
		}
		return target, nil
	}
	t.Run("one sentence is one step", func(t *testing.T) {
		target, err := load(t, validTargetYAML)
		if err != nil {
			t.Fatal(err)
		}
		const sentence = "Import from the user-defined routes page and select an existing interface."
		if !reflect.DeepEqual(target.ManualInstallationSteps, []string{sentence}) || target.ManualInstallationHint != sentence || target.ManualInstallationStepsEN != nil {
			t.Fatalf("loaded target = %#v", target)
		}
	})
	t.Run("steps load in order and join into the one-line hint", func(t *testing.T) {
		// A version such as 5.0 inside a step is the sentence's own number, and
		// space around a step is layout, not text.
		payload := withoutHint + `manual_installation_steps:
  - "  KeeneticOS 5.0 or newer is required.  "
  - Pick the interface.
manual_installation_steps_en:
  - Needs KeeneticOS 5.0.
  - Pick the interface.
`
		target, err := load(t, payload)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(target.ManualInstallationSteps, []string{"KeeneticOS 5.0 or newer is required.", "Pick the interface."}) ||
			!reflect.DeepEqual(target.ManualInstallationStepsEN, []string{"Needs KeeneticOS 5.0.", "Pick the interface."}) ||
			target.ManualInstallationHint != "KeeneticOS 5.0 or newer is required. Pick the interface." ||
			target.ManualInstallationHintEN != "Needs KeeneticOS 5.0. Pick the interface." {
			t.Fatalf("loaded target = %#v", target)
		}
	})
	t.Run("the shipped domain-based target lists its steps", func(t *testing.T) {
		catalog, err := Load(context.Background(), filepath.Join("..", "..", "..", "catalog"))
		if err != nil {
			t.Fatal(err)
		}
		target, ok := catalog.Target("keenetic-dns")
		if !ok || len(target.ManualInstallationSteps) != 4 || len(target.ManualInstallationStepsEN) != 4 {
			t.Fatalf("keenetic-dns steps = %#v", target)
		}
		for _, step := range slices.Concat(target.ManualInstallationSteps, target.ManualInstallationStepsEN) {
			if numberedStep.MatchString(step) {
				t.Errorf("a shipped step carries its own number: %q", step)
			}
		}
	})
	for _, test := range []struct {
		name    string
		payload string
	}{
		{"neither hint nor steps", withoutHint},
		{"both hint and steps", validTargetYAML + "manual_installation_steps: [first]\n"},
		{"empty list", withoutHint + "manual_installation_steps: []\n"},
		{"blank step", withoutHint + "manual_installation_steps: [first, \"  \"]\n"},
		{"newline inside a step", withoutHint + "manual_installation_steps: [\"first\\nsecond\"]\n"},
		{"numbered step", withoutHint + "manual_installation_steps: [\"1. first\", \"2. second\"]\n"},
		{"too many steps", withoutHint + "manual_installation_steps: [" + strings.TrimSuffix(strings.Repeat("a, ", maxInstallationSteps+1), ", ") + "]\n"},
		{"oversize step", withoutHint + "manual_installation_steps: [" + strings.Repeat("x", maxInstallationStep+1) + "]\n"},
		{"scalar where a list belongs", withoutHint + "manual_installation_steps: first\n"},
		{"English steps for a one-sentence base", validTargetYAML + "manual_installation_steps_en: [first, second]\n"},
		{"English steps fewer than base steps", withoutHint + "manual_installation_steps: [a, b]\nmanual_installation_steps_en: [a]\n"},
		{"English hint for several steps", withoutHint + "manual_installation_steps: [a, b]\nmanual_installation_hint_en: a b\n"},
		{"English hint and steps together", withoutHint + "manual_installation_steps: [a]\nmanual_installation_hint_en: a\nmanual_installation_steps_en: [a]\n"},
		{"empty English list", withoutHint + "manual_installation_steps: [a]\nmanual_installation_steps_en: []\n"},
		{"newline inside an English step", withoutHint + "manual_installation_steps: [a]\nmanual_installation_steps_en: [\"a\\nb\"]\n"},
	} {
		t.Run("refuses "+test.name, func(t *testing.T) {
			if _, err := load(t, test.payload); err == nil {
				t.Fatalf("accepted %s", test.name)
			}
		})
	}
}
