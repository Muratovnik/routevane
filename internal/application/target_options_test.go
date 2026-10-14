package application

import (
	"reflect"
	"testing"
)

// A target definition assembled in code, as a plugin does, may carry only its
// sentence. The listing still answers a list of steps, so a screen never has to
// tell the two shapes apart.
func TestInstallationStepsAreAlwaysAList(t *testing.T) {
	for _, test := range []struct {
		name  string
		steps []string
		hint  string
		want  []string
	}{
		{"listed steps win over the sentence", []string{"first", "second"}, "first second", []string{"first", "second"}},
		{"a sentence is one step", nil, "Import the file.", []string{"Import the file."}},
		{"no instruction is an empty list", nil, "", []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := installationSteps(test.steps, test.hint)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("installationSteps(%q, %q) = %#v, want %#v", test.steps, test.hint, got, test.want)
			}
			if len(test.steps) > 0 {
				got[0] = "changed"
				if test.steps[0] == "changed" {
					t.Fatal("the listing aliases the catalog's steps")
				}
			}
		})
	}
}
