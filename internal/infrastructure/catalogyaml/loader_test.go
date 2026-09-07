package catalogyaml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validCatalogYAML = `id: example
title: Example
components:
  web:
    required: true
seeds:
  - kind: domain_suffix
    value: example.com
    component: web
    source: manual
sources:
  - id: dns-main
    type: dns
    component: web
    config:
      names: [example.com]
`

const validTargetYAML = `id: keenetic
format_key: keenetic-bat-ipv4-v1
kind: router
renderer: keenetic-route-bat
constraints:
  supports_domain_exact: false
  supports_domain_suffix: false
  supports_dynamic_dns_set: false
  supports_ipv4: true
  supports_ipv6: false
  supports_prefixes: true
  max_rules: 1024
  max_artifact_size: 131072
renderer_options: []
manual_installation_hint: Import from the user-defined routes page and select an existing interface.
`

func TestLoadRejectsHostileAndMalformedYAML(t *testing.T) {
	tooManyNames := make([]string, MaxListItems+1)
	for i := range tooManyNames {
		tooManyNames[i] = fmt.Sprintf("n%d.example.com", i)
	}
	tests := []struct {
		name    string
		payload string
	}{
		{"unknown field", validCatalogYAML + "unknown: true\n"},
		{"duplicate key", strings.Replace(validCatalogYAML, "id: example", "id: example\nid: other", 1)},
		{"second document", validCatalogYAML + "---\nid: other\n"},
		{"anchor", strings.Replace(validCatalogYAML, "title: Example", "title: &title Example", 1)},
		{"alias", strings.Replace(validCatalogYAML, "title: Example", "title: &title Example\nid: *title", 1)},
		{"merge", strings.Replace(validCatalogYAML, "web:\n    required: true", "web: &base\n    required: true\n  auth:\n    <<: *base", 1)},
		{"custom tag", strings.Replace(validCatalogYAML, "title: Example", "title: !secret Example", 1)},
		{"traversal id", strings.Replace(validCatalogYAML, "id: example", "id: ../example", 1)},
		{"invalid ip", strings.Replace(validCatalogYAML, "kind: domain_suffix\n    value: example.com", "kind: ipv4\n    value: 999.1.1.1", 1)},
		{"invalid cidr", strings.Replace(validCatalogYAML, "kind: domain_suffix\n    value: example.com", "kind: prefix4\n    value: 192.0.2.1/99", 1)},
		{"list bound", strings.Replace(validCatalogYAML, "names: [example.com]", "names: ["+strings.Join(tooManyNames, ", ")+"]", 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeCatalogFile(t, "builtin", "list.yaml", []byte(test.payload))
			if _, err := Load(context.Background(), root); err == nil {
				t.Fatalf("Load accepted %s", test.name)
			}
		})
	}
}

func TestLoadBoundsFilesAndTotalBytes(t *testing.T) {
	t.Run("single file", func(t *testing.T) {
		payload := append([]byte(validCatalogYAML), []byte("#"+strings.Repeat("x", MaxFileBytes))...)
		root := writeCatalogFile(t, "builtin", "large.yaml", payload)
		if _, err := Load(context.Background(), root); err == nil {
			t.Fatal("oversized file accepted")
		}
	})
	t.Run("file count", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "builtin")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for i := 0; i <= MaxFiles; i++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%04d.yaml", i)), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Load(context.Background(), root); err == nil {
			t.Fatal("file-count bound accepted")
		}
	})
	t.Run("total bytes", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "builtin")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 17; i++ {
			list := strings.Replace(validCatalogYAML, "id: example", fmt.Sprintf("id: list-%d", i), 1)
			payload := []byte(list + "#" + strings.Repeat("x", 250000-len(list)-2) + "\n")
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d.yaml", i)), payload, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Load(context.Background(), root); err == nil {
			t.Fatal("total byte bound accepted")
		}
	})
}

func TestLoadRejectsCollisionsAndLinks(t *testing.T) {
	t.Run("builtin local collision", func(t *testing.T) {
		root := writeCatalogFile(t, "builtin", "a.yaml", []byte(validCatalogYAML))
		local := filepath.Join(root, "local")
		if err := os.Mkdir(local, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(local, "b.yaml"), []byte(validCatalogYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(context.Background(), root); err == nil {
			t.Fatal("duplicate list id accepted")
		}
	})
	t.Run("yaml symlink", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "builtin")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "target")
		if err := os.WriteFile(target, []byte(validCatalogYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, "linked.yaml")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := Load(context.Background(), root); err == nil {
			t.Fatal("YAML symlink accepted")
		}
	})
}

func TestCanonicalRevisionIgnoresFileAndYAMLOrder(t *testing.T) {
	first := validCatalogYAML
	second := `title: Example
id: example
sources:
  - config:
      names: [example.com, EXAMPLE.COM.]
    component: web
    type: dns
    id: dns-main
seeds:
  - source: manual
    component: web
    value: EXAMPLE.COM.
    kind: domain_suffix
components:
  web: {required: true}
`
	rootA := writeCatalogFile(t, "builtin", "z.yaml", []byte(first))
	rootB := writeCatalogFile(t, "local", "a.yaml", []byte(second))
	a, err := Load(context.Background(), rootA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(context.Background(), rootB)
	if err != nil {
		t.Fatal(err)
	}
	if a.Revision != b.Revision || a.Lists["example"].Sources[0].Revision != b.Lists["example"].Sources[0].Revision {
		t.Fatalf("canonical revisions differ: %#v %#v", a, b)
	}
}

func TestTargetCatalogIsOptionalStrictAndSeparatelyRevisioned(t *testing.T) {
	t.Run("optional for old catalog", func(t *testing.T) {
		root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
		catalog, err := Load(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if len(catalog.Targets) != 0 || catalog.TargetRevision != "" {
			t.Fatalf("unexpected optional target catalog: %#v", catalog)
		}
	})
	t.Run("separate revisions", func(t *testing.T) {
		root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
		writeTargetFile(t, root, "keenetic.yaml", []byte(validTargetYAML))
		first, err := Load(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		target, ok := first.Target("keenetic")
		if !ok || target.FormatKey != "keenetic-bat-ipv4-v1" || target.Constraints.MaxRules != 1024 || len(target.RendererOptions) != 0 {
			t.Fatalf("loaded target = %#v", target)
		}
		writeTargetFile(t, root, "keenetic.yaml", []byte(strings.Replace(validTargetYAML, "max_artifact_size: 131072", "max_artifact_size: 131073", 1)))
		second, err := Load(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if first.Revision != second.Revision || first.TargetRevision == second.TargetRevision {
			t.Fatalf("list/target revisions are not separate: %q/%q vs %q/%q", first.Revision, first.TargetRevision, second.Revision, second.TargetRevision)
		}
	})
	for _, test := range []struct {
		name    string
		payload string
	}{
		{"unknown field", validTargetYAML + "unknown: true\n"},
		{"duplicate key", strings.Replace(validTargetYAML, "id: keenetic", "id: keenetic\nid: other", 1)},
		{"renderer options", strings.Replace(validTargetYAML, "renderer_options: []", "renderer_options: [unsafe]", 1)},
		{"control hint", strings.Replace(validTargetYAML, "manual_installation_hint: Import from the user-defined routes page and select an existing interface.", "manual_installation_hint: |\n  first\n  second", 1)},
		{"unbounded rules", strings.Replace(validTargetYAML, "max_rules: 1024", "max_rules: 0", 1)},
		{"blank English title", validTargetYAML + "title_en: \"   \"\n"},
		{"empty English title", validTargetYAML + "title_en: \"\"\n"},
		{"blank English hint", validTargetYAML + "manual_installation_hint_en: \"\"\n"},
		{"control English hint", validTargetYAML + "manual_installation_hint_en: |\n  first\n  second\n"},
		{"oversize English title", validTargetYAML + "title_en: " + strings.Repeat("x", 65) + "\n"},
		{"missing kind", strings.Replace(validTargetYAML, "kind: router\n", "", 1)},
		{"open-set kind", strings.Replace(validTargetYAML, "kind: router", "kind: protocol", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
			writeTargetFile(t, root, "keenetic.yaml", []byte(test.payload))
			if _, err := Load(context.Background(), root); err == nil {
				t.Fatalf("accepted hostile target %s", test.name)
			}
		})
	}
}

// English is the product's primary language while the shipped catalog is
// authored in Russian, so a target may carry both. The pair is optional and
// absent is a real state: a file written before these fields, or by a plugin
// author shipping one language, still loads and reports no translation rather
// than an empty name.
func TestTargetCatalogCarriesAnOptionalEnglishNameAndHint(t *testing.T) {
	t.Run("absent stays loadable", func(t *testing.T) {
		root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
		writeTargetFile(t, root, "keenetic.yaml", []byte(validTargetYAML))
		catalog, err := Load(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		target, ok := catalog.Target("keenetic")
		if !ok || target.TitleEN != "" || target.ManualInstallationHintEN != "" {
			t.Fatalf("loaded target = %#v", target)
		}
	})
	t.Run("present reaches the profile", func(t *testing.T) {
		const hintEN = "Open Routing — User-defined routes — Upload, choose the file, and point it at the VPN or WAN interface you need."
		payload := validTargetYAML + "title: Keenetic (по доменам)\ntitle_en:  Keenetic (domain-based) \nmanual_installation_hint_en: " + hintEN + "\n"
		root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
		writeTargetFile(t, root, "keenetic.yaml", []byte(payload))
		catalog, err := Load(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		target, ok := catalog.Target("keenetic")
		if !ok || target.Title != "Keenetic (по доменам)" {
			t.Fatalf("loaded target = %#v", target)
		}
		// Surrounding space is normalized the way every other catalog string is,
		// so the translation is one field's worth of text and not its layout.
		if target.TitleEN != "Keenetic (domain-based)" || target.ManualInstallationHintEN != hintEN {
			t.Fatalf("English pair = %q / %q", target.TitleEN, target.ManualInstallationHintEN)
		}
	})
	// The shipped catalog is the reason this exists, so it is checked rather
	// than assumed: every real target names itself in both languages.
	t.Run("every shipped target is translated", func(t *testing.T) {
		catalog, err := Load(context.Background(), filepath.Join("..", "..", "..", "catalog"))
		if err != nil {
			t.Fatal(err)
		}
		if len(catalog.Targets) == 0 {
			t.Fatal("the shipped catalog carries no target")
		}
		for id, target := range catalog.Targets {
			if target.TitleEN == "" || target.ManualInstallationHintEN == "" {
				t.Errorf("shipped target %q has no English name or hint: %q / %q", id, target.TitleEN, target.ManualInstallationHintEN)
			}
		}
	})
}

func TestTargetCatalogRejectsLinks(t *testing.T) {
	root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
	targets := filepath.Join(root, "targets")
	if err := os.Mkdir(targets, 0o700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(root, "external.yaml")
	if err := os.WriteFile(external, []byte(validTargetYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(targets, "keenetic.yaml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Load(context.Background(), root); err == nil {
		t.Fatal("target YAML symlink accepted")
	}
}

func FuzzDecodeList(f *testing.F) {
	f.Add([]byte(validCatalogYAML))
	f.Add([]byte("id: x\nid: y\n"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > MaxFileBytes {
			t.Skip()
		}
		definition, err := decodeList(payload)
		if err == nil {
			if definition.ID == "" || definition.CatalogRevision != "" {
				t.Fatalf("noncanonical decoded definition: %#v", definition)
			}
		}
	})
}

func FuzzDecodeTarget(f *testing.F) {
	f.Add([]byte(validTargetYAML))
	f.Add([]byte("id: x\nid: y\n"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > MaxTargetFileBytes {
			t.Skip()
		}
		target, err := decodeTarget(payload)
		if err == nil && (target.ID == "" || target.RendererID == "" || target.Constraints.MaxRules <= 0) {
			t.Fatalf("noncanonical decoded target: %#v", target)
		}
	})
}

func writeCatalogFile(t *testing.T, group, name string, payload []byte) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, group)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeTargetFile(t *testing.T, root, name string, payload []byte) {
	t.Helper()
	dir := filepath.Join(root, "targets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ADR 0039 renames this key to free the word profile for the operator's own
// object. The retired name is read for one minor version so a target file an
// operator wrote or vendored keeps loading; naming both is refused, because the
// two mean the same thing and neither is more authoritative.
func TestTargetReadsTheRetiredFormatKeyButRefusesBoth(t *testing.T) {
	retired := strings.Replace(validTargetYAML, "format_key:", "profile_key:", 1)
	for _, test := range []struct {
		name    string
		payload string
	}{
		{"current key", validTargetYAML},
		{"retired key", retired},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
			writeTargetFile(t, root, "keenetic.yaml", []byte(test.payload))
			catalog, err := Load(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			target, ok := catalog.Target("keenetic")
			if !ok || target.FormatKey != "keenetic-bat-ipv4-v1" {
				t.Fatalf("target = %#v", target)
			}
		})
	}

	t.Run("both keys", func(t *testing.T) {
		root := writeCatalogFile(t, "builtin", "list.yaml", []byte(validCatalogYAML))
		writeTargetFile(t, root, "keenetic.yaml", []byte(validTargetYAML+"profile_key: keenetic-bat-ipv4-v1\n"))
		_, err := Load(context.Background(), root)
		if err == nil {
			t.Fatal("a target naming both keys must be refused")
		}
		if !strings.Contains(err.Error(), "retired profile_key") {
			t.Fatalf("error=%v, want it to name the retired key", err)
		}
	})
}
