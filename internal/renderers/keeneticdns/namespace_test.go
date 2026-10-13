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

// The expected names below are written out by hand from the naming contract
// (ADR 0049); the hash parts were computed outside this package. Output code
// 8a8366 is SHA-256 of "1"*32, 0d6ba1 of "2"*32.
func TestGroupNameReadsTheListAndFitsTheBound(t *testing.T) {
	head50 := strings.Repeat("a", 50)
	hyphenAtCut := "abcdefghijklmnopq-" + strings.Repeat("r", 45)
	for _, tc := range []struct {
		name, prefix, list, want string
	}{
		{"short list ID under the default prefix", "", "chatgpt", "routevane-chatgpt-8a8366-s1"},
		{"custom prefix", "home", "chatgpt", "home-chatgpt-8a8366-s1"},
		{"list ID exactly at the budget is kept", "", strings.Repeat("b", 42), "routevane-" + strings.Repeat("b", 42) + "-8a8366-s1"},
		{"one byte over the budget is truncated with its hash", "", strings.Repeat("b", 43), "routevane-" + strings.Repeat("b", 33) + "-f01b5722-8a8366-s1"},
		{"long lists sharing a head differ by hash (first)", "", head50 + "-first", "routevane-" + strings.Repeat("a", 33) + "-25da80a0-8a8366-s1"},
		{"long lists sharing a head differ by hash (second)", "", head50 + "-second", "routevane-" + strings.Repeat("a", 33) + "-3d85a4ed-8a8366-s1"},
		{"a cut that ends in a hyphen is trimmed", strings.Repeat("p", MaxPrefixLength), hyphenAtCut, strings.Repeat("p", MaxPrefixLength) + "-abcdefghijklmnopq-3dbedeb7-8a8366-s1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := (Renderer{}).RenderOutput(plan(suffix(t, "example.com", tc.list)), strings.Repeat("1", 32), tc.prefix)
			if err != nil {
				t.Fatal(err)
			}
			groups, err := Parse(payload)
			if err != nil || len(groups) != 1 {
				t.Fatalf("groups=%#v err=%v", groups, err)
			}
			if groups[0].Name != tc.want || len(groups[0].Name) > maxGroupName {
				t.Fatalf("name = %q (%d bytes), want %q", groups[0].Name, len(groups[0].Name), tc.want)
			}
		})
	}
}

// One router can carry the same list from two outputs; the output code keeps
// their groups apart while both still read as that list.
func TestTwoOutputsWithTheSameListGetDistinctReadableNames(t *testing.T) {
	names := map[string]string{}
	for _, output := range []string{strings.Repeat("1", 32), strings.Repeat("2", 32)} {
		payload, err := (Renderer{}).RenderOutput(plan(suffix(t, "chatgpt.com", "chatgpt")), output, "")
		if err != nil {
			t.Fatal(err)
		}
		groups, err := Parse(payload)
		if err != nil || len(groups) != 1 {
			t.Fatalf("groups=%#v err=%v", groups, err)
		}
		names[output[:1]] = groups[0].Name
	}
	if names["1"] != "routevane-chatgpt-8a8366-s1" || names["2"] != "routevane-chatgpt-0d6ba1-s1" {
		t.Fatalf("names = %v", names)
	}
}

// The shard suffix is reserved for the device's largest group count, so the
// 128th shard of the longest list under the longest prefix still fits, and
// every shard of that list carries the same list part.
func TestEveryShardOfALongListSharesItsListPartUpToTheLastGroup(t *testing.T) {
	list := strings.Repeat("c", 63)
	rules := make([]domain.RouteRule, 0, MaxGroups*MaxEntriesPerGroup)
	for i := 0; i < MaxGroups*MaxEntriesPerGroup; i++ {
		rules = append(rules, suffix(t, fmt.Sprintf("n%d.example", i), list))
	}
	prefix := strings.Repeat("p", MaxPrefixLength)
	groups, err := projectOutputPlan(plan(rules...), strings.Repeat("1", 32), prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != MaxGroups {
		t.Fatalf("groups = %d", len(groups))
	}
	stem := prefix + "-" + strings.Repeat("c", 18) + "-93378fde-8a8366-s"
	for i, group := range groups {
		if want := fmt.Sprintf("%s%d", stem, i+1); group.Name != want || !ValidGroupName(group.Name) {
			t.Fatalf("shard %d name = %q, want %q", i+1, group.Name, want)
		}
	}
	if last := groups[len(groups)-1].Name; len(last) != maxGroupName {
		t.Fatalf("the largest shard name %q is %d bytes; the reserve should fill the bound exactly", last, len(last))
	}
}

// Artifacts published under the earlier hashed scheme stay valid: their bytes
// are immutable and the deployer still reads them to clean up on the router.
func TestParseAcceptsAnArtifactWithTheEarlierHashedNames(t *testing.T) {
	legacy := "object-group fqdn routevane-db842808ecae-60965168ce762e94-s1 description \"ChatGPT\"\n" +
		"object-group fqdn routevane-db842808ecae-60965168ce762e94-s1 include chatgpt.com\n" +
		"object-group fqdn routevane-db842808ecae-60965168ce762e94-s1 include openai.com\n"
	groups, err := Parse([]byte(legacy))
	if err != nil || len(groups) != 1 || groups[0].Name != "routevane-db842808ecae-60965168ce762e94-s1" || groups[0].Description != "ChatGPT" || len(groups[0].Entries) != 2 {
		t.Fatalf("legacy artifact groups=%#v err=%v", groups, err)
	}
}
