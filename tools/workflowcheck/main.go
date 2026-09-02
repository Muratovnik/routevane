// workflowcheck supplies doctor's YAML-aware action pinning check. It is a
// development tool, not a product executable or a complete workflow schema linter.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Bound parser memory for repository-controlled input, well above our workflows.
const maxWorkflowBytes = 16 << 20

var (
	remotePin = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*@[a-fA-F0-9]{40}$`)
	imagePin  = regexp.MustCompile(`^docker://[^\s@]+@sha256:[a-fA-F0-9]{64}$`)
)

type workflow struct {
	Jobs map[string]struct {
		Uses  yaml.Node `yaml:"uses"`
		Steps []struct {
			Uses yaml.Node `yaml:"uses"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func referenceProblem(node *yaml.Node) string {
	if node.Kind == 0 {
		return "" // A run-only step has no action reference.
	}
	seen := make(map[*yaml.Node]bool)
	for node.Kind == yaml.AliasNode {
		if node.Alias == nil || seen[node] {
			return "invalid action alias"
		}
		seen[node] = true
		node = node.Alias
	}
	if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" || node.Value == "" {
		return "uses must be a non-empty string"
	}
	value := node.Value
	if strings.HasPrefix(value, "./") || strings.HasPrefix(value, "$/") {
		local := strings.TrimPrefix(strings.TrimPrefix(value, "./"), "$/")
		clean := path.Clean(local)
		if clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") &&
			!strings.HasPrefix(clean, "/") && !strings.ContainsAny(local, "@\\\r\n") {
			return "" // Same-repository actions/workflows do not name a remote revision.
		}
		return "local action reference must stay inside the repository without a revision suffix"
	}
	if strings.HasPrefix(value, "docker://") {
		if imagePin.MatchString(value) {
			return ""
		}
		return "container action must be pinned to a full sha256 digest"
	}
	if !remotePin.MatchString(value) {
		return "external action or reusable workflow must be pinned to a full commit SHA"
	}
	return ""
}

func check(data []byte) []string {
	if len(data) > maxWorkflowBytes {
		return []string{"workflow exceeds the 16 MiB parser input limit"}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var value workflow
	if err := decoder.Decode(&value); err != nil {
		return []string{fmt.Sprintf("invalid workflow YAML: %v", err)}
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return []string{"workflow must contain exactly one YAML document"}
	}
	if len(value.Jobs) == 0 {
		return []string{"workflow must contain jobs"}
	}
	names := make([]string, 0, len(value.Jobs))
	for name := range value.Jobs {
		names = append(names, name)
	}
	sort.Strings(names)
	var problems []string
	for _, name := range names {
		job := value.Jobs[name]
		if problem := referenceProblem(&job.Uses); problem != "" {
			problems = append(problems, fmt.Sprintf("%d: jobs.%s.uses: %s", job.Uses.Line, name, problem))
		}
		for index, step := range job.Steps {
			if problem := referenceProblem(&step.Uses); problem != "" {
				problems = append(problems, fmt.Sprintf("%d: jobs.%s.steps[%d].uses: %s", step.Uses.Line, name, index, problem))
			}
		}
	}
	return problems
}

func readWorkflow(root *os.Root, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, maxWorkflowBytes+1))
}

func run(files []string, output io.Writer) int {
	if len(files) == 0 {
		fmt.Fprintln(output, "usage: workflowcheck <repository-relative workflow.yml> ...")
		return 2
	}
	root, err := os.OpenRoot(".")
	if err != nil {
		fmt.Fprintln(output, err)
		return 2
	}
	defer root.Close()
	status := 0
	for _, name := range files {
		if !filepath.IsLocal(name) {
			fmt.Fprintf(output, "%s: workflow path must stay inside the repository\n", name)
			status = 1
			continue
		}
		data, err := readWorkflow(root, name)
		if err != nil {
			fmt.Fprintf(output, "%s: %v\n", name, err)
			status = 1
			continue
		}
		for _, problem := range check(data) {
			fmt.Fprintf(output, "%s:%s\n", name, problem)
			status = 1
		}
	}
	return status
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}
