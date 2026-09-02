package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const pinned = "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"

func stepWorkflow(step string) string {
	return "name: test\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n" + step + "\n"
}

func TestReferenceSyntax(t *testing.T) {
	for _, test := range []struct {
		name, step string
		valid      bool
	}{
		{"plain", "      - uses: " + pinned, true},
		{"quoted value", "      - uses: \"" + pinned + "\"", true},
		{"quoted key", "      - \"uses\": '" + pinned + "'", true},
		{"space before colon", "      - uses : " + pinned, true},
		{"flow mapping", "      - {uses: '" + pinned + "'}", true},
		{"folded scalar", "      - uses: >-\n          " + pinned, true},
		{"comment", "      # uses: actions/checkout@v4\n      - uses: " + pinned, true},
		{"run and metadata", "      - name: 'uses: example@main'\n        run: |\n          echo 'uses: example@main'\n        env:\n          uses: example@main", true},
		{"anchor", "      - uses: &checkout " + pinned + "\n      - uses: *checkout", true},
		{"local action", "      - uses: ./.github/actions/check", true},
		{"root local action", "      - uses: $/.github/actions/check", true},
		{"nested remote action", "      - uses: owner/repo/path/action@" + strings.Repeat("A", 40), true},
		{"image digest", "      - uses: docker://alpine@sha256:" + strings.Repeat("a", 64), true},
		{"tag", "      - uses: actions/checkout@v4", false},
		{"quoted key tag", "      - \"uses\": actions/checkout@v4", false},
		{"space before colon tag", "      - uses : actions/checkout@v4", false},
		{"flow tag", "      - {uses: actions/checkout@v4}", false},
		{"anchored tag", "      - uses: &checkout actions/checkout@v4\n      - uses: *checkout", false},
		{"short SHA", "      - uses: actions/checkout@3d3c42e", false},
		{"image tag", "      - uses: docker://alpine:latest", false},
		{"local escape", "      - uses: ./../outside", false},
		{"local revision", "      - uses: ./.github/actions/check@main", false},
		{"null", "      - uses: null", false},
		{"number", "      - uses: 42", false},
		{"mapping", "      - uses: {action: test}", false},
		{"duplicate keys", "      - uses: " + pinned + "\n        uses: actions/checkout@v4", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			problems := check([]byte(stepWorkflow(test.step)))
			if (len(problems) == 0) != test.valid {
				t.Fatalf("valid=%v, problems=%v", test.valid, problems)
			}
		})
	}
}

func TestReusableWorkflows(t *testing.T) {
	for _, reference := range []string{
		"./.github/workflows/check.yml", "$/.github/workflows/check.yml",
		"owner/repo/.github/workflows/check.yml@" + strings.Repeat("1", 40),
	} {
		if problems := check([]byte("jobs:\n  check:\n    uses: " + reference)); len(problems) != 0 {
			t.Fatal(problems)
		}
	}
	problems := check([]byte("jobs:\n  check:\n    'uses': owner/repo/.github/workflows/check.yml@main"))
	if len(problems) != 1 || !strings.Contains(problems[0], "3: jobs.check.uses:") {
		t.Fatalf("missing located diagnostic: %v", problems)
	}
}

func TestMalformedYAML(t *testing.T) {
	for _, input := range []string{
		"", "jobs: [", "jobs: []", "jobs: {}", "jobs:\n  a: {}\n  a: {}",
		stepWorkflow("      - uses: "+pinned) + "---\njobs: {}\n",
		stepWorkflow("      - uses: *missing"),
		stepWorkflow("      - uses: &loop [*loop]"),
		stepWorkflow("      - uses: "+pinned) + "\n#" + strings.Repeat("x", maxWorkflowBytes),
	} {
		if problems := check([]byte(input)); len(problems) == 0 {
			t.Fatal("invalid or oversized YAML accepted")
		}
	}
}

func TestCommandDiagnosticsAndExitStatus(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("valid workflow.yml", []byte(stepWorkflow("      - uses: '"+pinned+"'")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("invalid.yml", []byte(stepWorkflow("      - 'uses': actions/checkout@v4")), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		files  []string
		status int
		text   string
	}{
		{[]string{"valid workflow.yml"}, 0, ""},
		{[]string{"valid workflow.yml", "invalid.yml"}, 1, "invalid.yml:7: jobs.build.steps[0].uses:"},
		{[]string{"missing.yml"}, 1, "missing.yml:"},
		{[]string{"../outside.yml"}, 1, "must stay inside"},
		{nil, 2, "usage:"},
	} {
		var output bytes.Buffer
		if status := run(test.files, &output); status != test.status || !strings.Contains(output.String(), test.text) {
			t.Fatalf("files=%v status=%d output=%s", test.files, status, output.String())
		}
	}
}

func FuzzWorkflow(f *testing.F) {
	for _, input := range []string{stepWorkflow("      - uses: " + pinned), "jobs: [", "jobs: {}", "&loop [*loop]"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 64<<10 {
			t.Skip()
		}
		first := strings.Join(check([]byte(input)), "\n")
		if second := strings.Join(check([]byte(input)), "\n"); first != second {
			t.Fatalf("unstable diagnostics: %q != %q", first, second)
		}
	})
}
