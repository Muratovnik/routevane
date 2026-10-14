package domain

import (
	"reflect"
	"slices"
	"testing"
)

// Every target field is either part of the artifact revision or named as
// presentation. A field added without that decision fails here, because
// leaving it out silently would let an artifact change go unnoticed, and
// adding it silently would strand every output on a text edit (ADR 0054).
func TestEveryTargetFieldIsClassifiedForTheRevision(t *testing.T) {
	artifact := fieldNames(reflect.TypeFor[targetArtifactFields]())
	for _, name := range artifact {
		if slices.Contains(targetPresentationFields, name) {
			t.Errorf("field %s is both artifact and presentation", name)
		}
	}
	for _, name := range fieldNames(reflect.TypeFor[TargetDefinition]()) {
		if !slices.Contains(artifact, name) && !slices.Contains(targetPresentationFields, name) {
			t.Errorf("TargetDefinition.%s is not classified as artifact or presentation", name)
		}
	}
	for _, name := range append(slices.Clone(artifact), targetPresentationFields...) {
		if _, ok := reflect.TypeFor[TargetDefinition]().FieldByName(name); !ok {
			t.Errorf("classified field %s does not exist on TargetDefinition", name)
		}
	}
	// Every constraint is consumed by the planner or a renderer, so the
	// revision carries each one under its own name.
	if got, want := fieldNames(reflect.TypeFor[targetArtifactConstraints]()), fieldNames(reflect.TypeFor[TargetConstraints]()); !slices.Equal(got, want) {
		t.Errorf("revision constraints %v, target constraints %v", got, want)
	}
}

func TestTargetRevisionFollowsArtifactFieldsOnly(t *testing.T) {
	base := TargetDefinition{
		ID: "router", FormatKey: "router-v1", RendererID: "router", Kind: TargetKindRouter,
		Title: "Роутер", TitleEN: "Router",
		Constraints:             TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 100, MaxArtifactSize: 4096},
		RendererOptions:         []string{},
		ManualInstallationHint:  "Загрузите файл.",
		ManualInstallationSteps: []string{"Загрузите файл."},
	}
	revision := TargetRevision(base)
	if len(revision) != 64 {
		t.Fatalf("revision=%q", revision)
	}

	presentation := map[string]func(*TargetDefinition){
		"id":            func(d *TargetDefinition) { d.ID = "router-alias" },
		"title":         func(d *TargetDefinition) { d.Title = "Другой роутер" },
		"english title": func(d *TargetDefinition) { d.TitleEN = "Another router" },
		"kind":          func(d *TargetDefinition) { d.Kind = TargetKindApp },
		"hint":          func(d *TargetDefinition) { d.ManualInstallationHint = "Откройте панель." },
		"steps": func(d *TargetDefinition) {
			d.ManualInstallationSteps = []string{"Откройте панель.", "Загрузите файл."}
		},
		"english hint":  func(d *TargetDefinition) { d.ManualInstallationHintEN = "Upload the file." },
		"english steps": func(d *TargetDefinition) { d.ManualInstallationStepsEN = []string{"Upload the file."} },
		"nil options":   func(d *TargetDefinition) { d.RendererOptions = nil },
	}
	for name, edit := range presentation {
		changed := base
		edit(&changed)
		if got := TargetRevision(changed); got != revision {
			t.Errorf("editing the %s changed the revision", name)
		}
	}

	artifact := map[string]func(*TargetDefinition){
		"format key":       func(d *TargetDefinition) { d.FormatKey = "router-v2" },
		"renderer":         func(d *TargetDefinition) { d.RendererID = "router-next" },
		"renderer options": func(d *TargetDefinition) { d.RendererOptions = []string{"compact"} },
		"exact domains":    func(d *TargetDefinition) { d.Constraints.SupportsDomainExact = true },
		"domain suffixes":  func(d *TargetDefinition) { d.Constraints.SupportsDomainSuffix = true },
		"dynamic dns sets": func(d *TargetDefinition) { d.Constraints.SupportsDynamicDNSSet = true },
		"ipv4":             func(d *TargetDefinition) { d.Constraints.SupportsIPv4 = false },
		"ipv6":             func(d *TargetDefinition) { d.Constraints.SupportsIPv6 = true },
		"prefixes":         func(d *TargetDefinition) { d.Constraints.SupportsPrefixes = false },
		"rule limit":       func(d *TargetDefinition) { d.Constraints.MaxRules = 101 },
		"artifact size":    func(d *TargetDefinition) { d.Constraints.MaxArtifactSize = 8192 },
		"entries per list": func(d *TargetDefinition) { d.Constraints.MaxEntriesPerList = 10 },
		"number of lists":  func(d *TargetDefinition) { d.Constraints.MaxLists = 10 },
	}
	seen := map[string]string{revision: "base"}
	for name, edit := range artifact {
		changed := base
		edit(&changed)
		got := TargetRevision(changed)
		if previous, collision := seen[got]; collision {
			t.Errorf("editing the %s gave the revision of %s", name, previous)
		}
		seen[got] = name
	}
}

// An output stores the revision of its target when it is created and compares
// it on every build (ADR 0054). The relative tests above hold for any encoding
// of the artifact fields, so a renamed JSON tag, a dropped omitempty or a new
// field with a non-zero default would pass them while giving every newly made
// output a revision that no older release computes. This test pins the value
// itself, for a target that states every artifact field and for one that states
// only the required ones.
// targetRevisionPinMessage is what a failing pin must tell its reader.
const targetRevisionPinMessage = "Changing a pinned target revision strands every existing output: each one stores its revision when it is created and refuses to build once it differs. " +
	"A different value needs a data migration that rebases stored outputs and an ADR (see ADR 0054), not an updated constant. " +
	"If this failed after an encoding change, revert the change instead."

func TestTargetRevisionValueIsPinned(t *testing.T) {
	for _, test := range []struct {
		name   string
		target TargetDefinition
		want   string
	}{
		{
			name: "every artifact field stated",
			target: TargetDefinition{
				ID: "pinned", FormatKey: "pinned-v1", RendererID: "pinned-renderer", Kind: TargetKindRouter,
				RendererOptions: []string{"compact", "sorted"},
				Constraints: TargetConstraints{
					SupportsDomainExact: true, SupportsDomainSuffix: true, SupportsDynamicDNSSet: true,
					SupportsIPv4: true, SupportsIPv6: true, SupportsPrefixes: true,
					MaxRules: 1000, MaxArtifactSize: 65536, MaxEntriesPerList: 250, MaxLists: 64,
				},
			},
			want: "506b2dd1a494e48496b30ea9e9e3ab6563b257162928cda0a545322e4d62e0d3",
		},
		{
			name:   "only the required fields stated",
			target: TargetDefinition{ID: "minimal", FormatKey: "minimal-v1", RendererID: "minimal-renderer"},
			want:   "6512e437a661aedb54399aead7165c7df43ae71fde3e3ff9a9704b0a0b2c8498",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := TargetRevision(test.target); got != test.want {
				t.Errorf("TargetRevision = %s, pinned %s\n%s", got, test.want, targetRevisionPinMessage)
			}
		})
	}
}

func fieldNames(typ reflect.Type) []string {
	names := make([]string, 0, typ.NumField())
	for field := range typ.Fields() {
		names = append(names, field.Name)
	}
	return names
}
