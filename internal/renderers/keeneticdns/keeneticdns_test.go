package keeneticdns

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func suffix(t *testing.T, value, list string) domain.RouteRule {
	t.Helper()
	rule, err := domain.NewDomainRule(domain.RuleDomainSuffix, value, list, "web", domain.SourceCommunity, []string{"official_rule"}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func plan(rules ...domain.RouteRule) domain.RoutingPlan {
	return domain.RoutingPlan{TargetID: "keenetic-dns", FormatKey: Version, Rules: rules}
}

// One group per list, so a group on the router says what it is and what
// removing it costs.
func TestRenderMakesOneGroupPerList(t *testing.T) {
	payload, err := Render(plan(
		suffix(t, "youtube.com", "youtube"),
		suffix(t, "discord.com", "discord"),
		suffix(t, "googlevideo.com", "youtube"),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := "object-group fqdn routevane-discord include discord.com\n" +
		"object-group fqdn routevane-youtube include googlevideo.com\n" +
		"object-group fqdn routevane-youtube include youtube.com\n"
	if string(payload) != want {
		t.Fatalf("payload =\n%s", payload)
	}
	if err := Validate(payload); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// A list over the entry bound is split under the hood: the operator asked
// for one list, and the numbered sub-groups are the adapter's business.
func TestAListOverTheBoundIsSplitIntoSubGroups(t *testing.T) {
	rules := make([]domain.RouteRule, 0, MaxEntriesPerGroup+5)
	for index := 0; index < MaxEntriesPerGroup+5; index++ {
		rules = append(rules, suffix(t, fmt.Sprintf("n%d.example.com", index), "youtube"))
	}
	payload, err := Render(plan(rules...))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := Parse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d", len(groups))
	}
	if groups[0].Name != "routevane-youtube" || len(groups[0].Entries) != MaxEntriesPerGroup {
		t.Fatalf("first group = %s/%d", groups[0].Name, len(groups[0].Entries))
	}
	if groups[1].Name != "routevane-youtube-2" || len(groups[1].Entries) != 5 {
		t.Fatalf("second group = %s/%d", groups[1].Name, len(groups[1].Entries))
	}
}

// Splitting makes room inside a file format and never on the device, so more
// groups than the device holds is a refusal that names the budget.
func TestMoreGroupsThanTheDeviceHoldsIsRefused(t *testing.T) {
	rules := make([]domain.RouteRule, 0, MaxGroups+1)
	for index := 0; index <= MaxGroups; index++ {
		rules = append(rules, suffix(t, "example.com", fmt.Sprintf("list-%d", index)))
	}
	_, err := Render(plan(rules...))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d", MaxGroups)) {
		t.Fatalf("err = %v", err)
	}
}

// Every group this product writes carries its own prefix: the budget is shared
// with whatever the operator created by hand.
func TestEveryGroupNameIsNamespaced(t *testing.T) {
	payload, err := Render(plan(suffix(t, "example.com", "example")))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(payload), "\n"), "\n") {
		if !strings.HasPrefix(line, "object-group fqdn "+GroupPrefix) {
			t.Fatalf("unnamespaced line %q", line)
		}
	}
}

// Addresses and networks belong in a group too, so an operator with a
// domain-first list and one hard-coded range gets both.
func TestAddressesAndPrefixesAreCarried(t *testing.T) {
	addr, err := domain.NewAddrRule(mustAddr(t, "192.0.2.1"), "example", "web", domain.SourceOfficial, []string{"official_rule"}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := domain.NewPrefixRule(mustPrefix(t, "198.51.100.0/24"), "example", "web", domain.SourceOfficial, []string{"official_rule"}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := Render(plan(addr, prefix, suffix(t, "example.com", "example")))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"192.0.2.1", "198.51.100.0/24", "example.com"} {
		if !strings.Contains(string(payload), " include "+want+"\n") {
			t.Fatalf("payload missing %q:\n%s", want, payload)
		}
	}
}

// An exact name cannot be expressed: the device includes every subdomain of a
// listed name and cannot be told not to.
func TestAnExactDomainIsRefused(t *testing.T) {
	rule, err := domain.NewDomainRule(domain.RuleDomainExact, "example.com", "example", "web", domain.SourceOfficial, []string{"official_rule"}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Render(plan(rule)); err == nil {
		t.Fatal("expected an exact domain to be refused")
	}
}

func TestValidatorRefusesForeignContent(t *testing.T) {
	cases := map[string]string{
		"foreign group":   "object-group fqdn homelab include example.com\n",
		"unknown command": "dns-proxy route object-group routevane-example Wireguard0\n",
		"no newline":      "object-group fqdn routevane-example include example.com",
		"duplicate":       "object-group fqdn routevane-example include example.com\nobject-group fqdn routevane-example include example.com\n",
		"unsorted":        "object-group fqdn routevane-example include zeta.example\nobject-group fqdn routevane-example include alpha.example\n",
		"interleaved":     "object-group fqdn routevane-a include a.example\nobject-group fqdn routevane-b include b.example\nobject-group fqdn routevane-a include c.example\n",
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate([]byte(payload)); err == nil {
				t.Fatalf("expected %s to be refused", name)
			}
		})
	}
}

func TestProjectedRuleCountMatchesTheEntries(t *testing.T) {
	count, err := Renderer{}.ProjectedRuleCount(plan(
		suffix(t, "youtube.com", "youtube"),
		suffix(t, "discord.com", "discord"),
	))
	if err != nil || count != 2 {
		t.Fatalf("count = %d err = %v", count, err)
	}
}

// TestRenderProducesTheGoldenGroupFile pins the canonical byte form across a
// mixed list set: two suffixes sharing one group, an address and a prefix
// carried alongside a suffix in another group. The golden file is
// regenerated with ROUTEVANE_UPDATE_GOLDEN=1, the same mechanism the other
// renderer packages use.
func TestRenderProducesTheGoldenGroupFile(t *testing.T) {
	addr, err := domain.NewAddrRule(mustAddr(t, "192.0.2.1"), "youtube", "web", domain.SourceOfficial, []string{"official_rule"}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := domain.NewPrefixRule(mustPrefix(t, "198.51.100.0/24"), "youtube", "web", domain.SourceOfficial, []string{"official_rule"}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := Render(plan(
		suffix(t, "youtube.com", "youtube"),
		suffix(t, "googlevideo.com", "youtube"),
		suffix(t, "discord.com", "discord"),
		addr,
		prefix,
	))
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "groups.golden.txt")
	if os.Getenv("ROUTEVANE_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture is checked out with the platform's line endings, so it is
	// normalized to this format's own before the comparison. Without this a
	// golden file matches only on the platform that happened to write it.
	if string(payload) != strings.ReplaceAll(string(golden), "\r\n", "\n") {
		t.Fatalf("rendered groups do not match the golden file:\n--- got ---\n%s\n--- want ---\n%s", payload, golden)
	}
	if err := Validate(payload); err != nil {
		t.Fatalf("golden groups failed their own validator: %v", err)
	}
}

// FuzzParseNeverAcceptsANonCanonicalFile proves the real Parse contract: it
// decodes independently of Render, and any file it accepts must render back
// to the exact same bytes through renderGroups. A file that merely looks
// like a valid group file but is reordered, padded, or duplicated must be
// refused, never silently corrected.
func FuzzParseNeverAcceptsANonCanonicalFile(f *testing.F) {
	f.Add("object-group fqdn routevane-discord include discord.com\n" +
		"object-group fqdn routevane-youtube include googlevideo.com\n" +
		"object-group fqdn routevane-youtube include youtube.com\n")
	f.Add("object-group fqdn routevane-example include 192.0.2.1\n")
	f.Add("object-group fqdn routevane-example include 198.51.100.0/24\n")
	f.Add("")
	f.Add("object-group fqdn homelab include example.com\n")
	f.Fuzz(func(t *testing.T, payload string) {
		groups, err := Parse([]byte(payload))
		if err != nil {
			return
		}
		if len(groups) == 0 {
			t.Fatalf("accepted a file with no group: %q", payload)
		}
		rendered, renderErr := renderGroups(groups)
		if renderErr != nil {
			t.Fatalf("accepted a file its own projection cannot render: %v", renderErr)
		}
		if string(rendered) != payload {
			t.Fatalf("accepted a non-canonical file: %q", payload)
		}
	})
}

func mustAddr(t *testing.T, value string) netip.Addr {
	t.Helper()
	address, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func mustPrefix(t *testing.T, value string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		t.Fatal(err)
	}
	return prefix
}
