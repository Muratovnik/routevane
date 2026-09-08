package keeneticdns

import (
	"bytes"
	"fmt"
	"github.com/Muratovnik/routevane/internal/domain"
	"strings"
	"testing"
)

func TestOutputListAndShardNamespacesAreIndependentAndBounded(t *testing.T) {
	rules := []domain.RouteRule{suffix(t, "other.example", "alpha-2"), suffix(t, "long.example", strings.Repeat("a", 48))}
	for i := 0; i < 301; i++ {
		rules = append(rules, suffix(t, fmt.Sprintf("n%d.example", i), "alpha"))
	}
	renderer := Renderer{}
	first, err := renderer.RenderOutput(plan(rules...), strings.Repeat("1", 32), strings.Repeat("a", MaxPrefixLength))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := Parse(first)
	if err != nil || len(groups) != 4 {
		t.Fatalf("groups=%v err=%v", groups, err)
	}
	seen := map[string]bool{}
	for _, g := range groups {
		if seen[g.Name] || len(g.Name) > 64 {
			t.Fatalf("colliding/unbounded group %q", g.Name)
		}
		seen[g.Name] = true
	}
	again, err := renderer.RenderOutput(plan(rules...), strings.Repeat("1", 32), strings.Repeat("a", MaxPrefixLength))
	if err != nil || !bytes.Equal(first, again) {
		t.Fatal("same output was not deterministic")
	}
	second, err := renderer.RenderOutput(plan(rules...), strings.Repeat("2", 32), strings.Repeat("a", MaxPrefixLength))
	if err != nil {
		t.Fatal(err)
	}
	other, err := Parse(second)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range other {
		if seen[g.Name] {
			t.Fatalf("outputs shared %s", g.Name)
		}
	}
}

func TestOutputPrefixRejectsUnsafeOrUnboundedValues(t *testing.T) {
	for _, prefix := range []string{"Upper", "bad prefix", "-bad", "bad-", "bad\ncommand", strings.Repeat("x", 25)} {
		if _, err := (Renderer{}).RenderOutput(plan(suffix(t, "example.com", "example")), "owner", prefix); err == nil {
			t.Fatalf("accepted %q", prefix)
		}
	}
	for _, prefix := range []string{"", "custom", "home-2", strings.Repeat("x", 24)} {
		if _, err := (Renderer{}).RenderOutput(plan(suffix(t, "example.com", "example")), "owner", prefix); err != nil {
			t.Fatalf("refused %q: %v", prefix, err)
		}
	}
}
