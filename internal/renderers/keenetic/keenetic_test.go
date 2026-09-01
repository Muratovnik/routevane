package keenetic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestRenderIsDeterministicDeduplicatedAndDoesNotMutateBacking(t *testing.T) {
	prefix, _ := domain.NewPrefixRule(netip.MustParsePrefix("10.0.0.0/24"), "alpha", "web", domain.SourceManual, []string{"manual_rule"}, []string{"manual:a"})
	address, _ := domain.NewAddrRule(netip.MustParseAddr("192.0.2.1"), "alpha", "web", domain.SourceObserved, []string{"fresh_dns_observation"}, []string{"dns:a"})
	duplicate, _ := domain.NewPrefixRule(netip.MustParsePrefix("192.0.2.1/32"), "beta", "web", domain.SourceObserved, []string{"fresh_dns_observation"}, []string{"dns:b"})
	backing := make([]domain.RouteRule, 5)
	backing[0], backing[1], backing[2] = address, prefix, duplicate
	backing[3], backing[4] = prefix, address
	servicesBacking := []string{"beta", "alpha", "hidden"}
	plan := domain.RoutingPlan{Rules: backing[:3], Services: servicesBacking[:2]}
	wantBacking := cloneRules(backing)
	wantServices := append([]string(nil), servicesBacking...)

	first, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	reordered := plan
	reordered.Rules = []domain.RouteRule{duplicate, prefix, address, prefix}
	second, err := Render(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("permutation changed bytes:\n%q\n%q", first, second)
	}
	if !reflect.DeepEqual(backing, wantBacking) || !reflect.DeepEqual(servicesBacking, wantServices) {
		t.Fatalf("renderer mutated input backing: rules=%#v services=%#v", backing, servicesBacking)
	}
	count, err := ProjectedRuleCount(plan)
	if err != nil || count != 2 {
		t.Fatalf("projected equivalent IPv4 and /32 count=%d err=%v", count, err)
	}
	parsed, err := Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if got := prefixStrings(parsed); !reflect.DeepEqual(got, []string{"10.0.0.0/24", "192.0.2.1/32"}) {
		t.Fatalf("parsed projection = %v", got)
	}
}

func TestValidateRejectsBrokenTruncatedOversizedControlAndUnknownInputs(t *testing.T) {
	valid := []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n")
	tests := map[string][]byte{
		"empty":              {},
		"truncated":          valid[:len(valid)-1],
		"oversized":          bytes.Repeat([]byte{'x'}, MaxArtifactSize+1),
		"control":            []byte("route ADD 192.0.2.1\tMASK 255.255.255.255 0.0.0.0\r\n"),
		"unknown":            []byte("route DELETE 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n"),
		"lowercase":          []byte("route add 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n"),
		"comment":            []byte("# route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n"),
		"domain":             []byte("route ADD example.com MASK 255.255.255.255 0.0.0.0\r\n"),
		"ipv6":               []byte("route ADD 2001:db8::1 MASK 255.255.255.255 0.0.0.0\r\n"),
		"metric":             []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0 METRIC 1\r\n"),
		"bom":                append([]byte{0xef, 0xbb, 0xbf}, valid...),
		"bare lf":            bytes.ReplaceAll(valid, []byte("\r\n"), []byte("\n")),
		"bare cr":            bytes.ReplaceAll(valid, []byte("\r\n"), []byte("\r")),
		"blank line":         append(append([]byte(nil), valid...), []byte("\r\n")...),
		"noncontiguous mask": []byte("route ADD 192.0.2.0 MASK 255.0.255.0 0.0.0.0\r\n"),
		"default":            []byte("route ADD 0.0.0.0 MASK 0.0.0.0 0.0.0.0\r\n"),
		"unmasked":           []byte("route ADD 192.0.2.1 MASK 255.255.255.0 0.0.0.0\r\n"),
		"gateway":            []byte("route ADD 192.0.2.1 MASK 255.255.255.255 192.0.2.254\r\n"),
		"duplicate":          append(append([]byte(nil), valid...), valid...),
		"unsorted": []byte("route ADD 192.0.2.2 MASK 255.255.255.255 0.0.0.0\r\n" +
			"route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n"),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if err := Validate(payload); err == nil {
				t.Fatalf("Validate accepted %q", payload)
			}
		})
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid canonical BAT rejected: %v", err)
	}
}

func TestValidateEnforcesOfficialLineLimit(t *testing.T) {
	var payload bytes.Buffer
	for i := 0; i < MaxLines; i++ {
		address := fmt.Sprintf("198.18.%d.%d", i/256, i%256)
		fmt.Fprintf(&payload, "route ADD %s MASK 255.255.255.255 0.0.0.0\r\n", address)
	}
	if err := Validate(payload.Bytes()); err != nil {
		t.Fatalf("exactly %d canonical lines rejected: %v", MaxLines, err)
	}
	payload.WriteString("route ADD 198.22.0.0 MASK 255.255.255.255 0.0.0.0\r\n")
	if err := Validate(payload.Bytes()); err == nil {
		t.Fatalf("%d lines accepted", MaxLines+1)
	}
}

func TestGoldenProjectionMatchesPlanRules(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "golden", "keenetic")
	inputBytes, err := os.ReadFile(filepath.Join(root, "basic.input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		TargetID   string   `json:"target_id"`
		ProfileKey string   `json:"profile_key"`
		Services   []string `json:"services"`
		Rules      []struct {
			Kind      domain.RuleKind `json:"kind"`
			Value     string          `json:"value"`
			Service   string          `json:"service"`
			Component string          `json:"component"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(inputBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	plan := domain.RoutingPlan{TargetID: fixture.TargetID, ProfileKey: fixture.ProfileKey, Services: fixture.Services}
	wantProjection := make([]string, 0, len(fixture.Rules))
	for _, item := range fixture.Rules {
		var rule domain.RouteRule
		switch item.Kind {
		case domain.RuleIPv4:
			rule, err = domain.NewAddrRule(netip.MustParseAddr(item.Value), item.Service, item.Component, domain.SourceManual, []string{"manual_rule"}, []string{"golden"})
			wantProjection = append(wantProjection, item.Value+"/32")
		case domain.RulePrefix4:
			rule, err = domain.NewPrefixRule(netip.MustParsePrefix(item.Value), item.Service, item.Component, domain.SourceManual, []string{"manual_rule"}, []string{"golden"})
			wantProjection = append(wantProjection, item.Value)
		default:
			t.Fatalf("unsupported golden kind %q", item.Kind)
		}
		if err != nil {
			t.Fatal(err)
		}
		plan.Rules = append(plan.Rules, rule)
	}
	payload, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join(root, "basic.expected"))
	if err != nil {
		t.Fatal(err)
	}
	expected = []byte(strings.ReplaceAll(strings.ReplaceAll(string(expected), "\r\n", "\n"), "\n", "\r\n"))
	if !bytes.Equal(payload, expected) {
		t.Fatalf("golden changed:\n got %q\nwant %q", payload, expected)
	}
	parsed, err := Parse(payload)
	if err != nil {
		t.Fatal(err)
	}
	sortStrings(wantProjection)
	if got := prefixStrings(parsed); !reflect.DeepEqual(got, wantProjection) {
		t.Fatalf("parsed rules = %v, exact plan projection = %v", got, wantProjection)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n"))
	f.Add([]byte("route ADD 0.0.0.0 MASK 0.0.0.0 0.0.0.0\r\n"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > MaxArtifactSize+1 {
			t.Skip()
		}
		rules, err := Parse(payload)
		if err == nil {
			rendered, renderErr := renderRules(rules)
			if renderErr != nil || !bytes.Equal(rendered, payload) {
				t.Fatalf("accepted payload is not canonical: renderErr=%v", renderErr)
			}
		}
	})
}

func prefixStrings(rules []Rule) []string {
	values := make([]string, len(rules))
	for i := range rules {
		values[i] = rules[i].Prefix.String()
	}
	return values
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func cloneRules(input []domain.RouteRule) []domain.RouteRule {
	result := append([]domain.RouteRule(nil), input...)
	for i := range result {
		result[i].ReasonCodes = append([]string(nil), input[i].ReasonCodes...)
		result[i].ProvenanceRefs = append([]string(nil), input[i].ProvenanceRefs...)
	}
	return result
}
