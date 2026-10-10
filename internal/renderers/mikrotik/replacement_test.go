package mikrotik

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestReplacementAlwaysClearsBothAddressFamilies(t *testing.T) {
	for _, prefix := range []string{"192.0.2.0/24", "2001:db8::/32"} {
		t.Run(prefix, func(t *testing.T) {
			plan := planWith(t, nil, []domain.Sighting{officialPrefix(t, prefix)})
			payload, err := Render(plan)
			if err != nil {
				t.Fatal(err)
			}
			for _, replacement := range []string{
				"/ip firewall address-list\nremove [find list=routevane4 !dynamic]\nremove [find list=routevane4]\n",
				"/ipv6 firewall address-list\nremove [find list=routevane6 !dynamic]\nremove [find list=routevane6]\n",
			} {
				if !strings.Contains(string(payload), replacement) {
					t.Fatalf("replacement leaves a previous family or DNS child behind: %s", payload)
				}
			}
			parsed, err := Parse(payload)
			if err != nil || len(parsed.IPv4)+len(parsed.IPv6) != 1 {
				t.Fatalf("single-family replacement = %#v, err=%v", parsed, err)
			}
		})
	}
}

func TestEntryBudgetDoesNotCountRequiredReplacementCommands(t *testing.T) {
	seeds := make([]domain.Seed, MaxLines/2)
	for i := range seeds {
		seeds[i] = exactSeed(fmt.Sprintf("d%04d.example", i))
	}
	plan := planWith(t, seeds, nil)
	count, err := ProjectedRuleCount(plan)
	if err != nil || count != 4096 {
		t.Fatalf("projected entries=%d err=%v", count, err)
	}
	payload, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(payload)
	if err != nil || len(parsed.IPv4)+len(parsed.IPv6) != 4096 {
		t.Fatalf("advertised entry budget rejected: v4=%d v6=%d err=%v", len(parsed.IPv4), len(parsed.IPv6), err)
	}
	overflow := append(bytes.Clone(payload), []byte("add address=z.example list=routevane6\n")...)
	if _, err := Parse(overflow); err == nil {
		t.Fatal("4097 entries exceeded the budget without being rejected")
	}
}

func TestPublishedV1ScriptsStillRoundTrip(t *testing.T) {
	for _, payload := range []string{
		Header + "\n/ip firewall address-list\nremove [find list=routevane4]\nadd address=192.0.2.0/24 list=routevane4\n",
		Header + "\n/ipv6 firewall address-list\nremove [find list=routevane6]\nadd address=2001:db8::/32 list=routevane6\n",
		validScript(),
	} {
		parsed, err := Parse([]byte(payload))
		if err != nil {
			t.Fatalf("published v1 artifact rejected: %v", err)
		}
		rendered, err := renderScript(parsed)
		if err != nil || string(rendered) != payload {
			t.Fatalf("published bytes changed during validation: %q err=%v", rendered, err)
		}
	}
}

func TestReplacementRefusesMixedOrIncompleteCleanupForms(t *testing.T) {
	payload := Header + "\n/ip firewall address-list\n" +
		"remove [find list=routevane4 !dynamic]\nremove [find list=routevane4]\n" +
		"add address=192.0.2.0/24 list=routevane4\n" +
		"/ipv6 firewall address-list\n" +
		"remove [find list=routevane6 !dynamic]\nremove [find list=routevane6]\n"
	if err := Validate([]byte(payload)); err != nil {
		t.Fatalf("complete replacement rejected: %v", err)
	}
	for name, invalid := range map[string]string{
		"mixed old and new":    strings.Replace(payload, "remove [find list=routevane4 !dynamic]\n", "", 1),
		"missing final clear":  strings.Replace(payload, "remove [find list=routevane4]\n", "", 1),
		"dynamic first":        strings.Replace(payload, "list=routevane4 !dynamic", "list=routevane4 dynamic", 1),
		"empty family omitted": strings.Split(payload, "/ipv6 firewall address-list\n")[0],
	} {
		t.Run(name, func(t *testing.T) {
			if err := Validate([]byte(invalid)); err == nil {
				t.Fatalf("incomplete or mixed replacement accepted: %s", invalid)
			}
		})
	}
}

func TestPublishedV1AtByteBoundRemainsReadable(t *testing.T) {
	// Construct an independently spelled legacy document at the exact bound.
	// Its names are sorted and use labels no longer than the DNS limit.
	nameOfLength := func(index, size int) string {
		prefix := fmt.Sprintf("n%04d.", index)
		remaining := size - len(prefix) - len(".example")
		var labels []string
		for remaining > 63 {
			length := 63
			if remaining == 64 {
				length = 62
			}
			labels = append(labels, strings.Repeat("a", length))
			remaining -= length + 1
		}
		labels = append(labels, strings.Repeat("a", remaining))
		return prefix + strings.Join(labels, ".") + ".example"
	}
	var legacy strings.Builder
	legacy.WriteString(Header + "\n/ip firewall address-list\nremove [find list=routevane4]\n")
	entries := 0
	for {
		line := "add address=" + nameOfLength(entries, 180) + " list=routevane4\n"
		if MaxArtifactSize-legacy.Len()-len(line) < 64 {
			break
		}
		legacy.WriteString(line)
		entries++
	}
	lastNameSize := MaxArtifactSize - legacy.Len() - len("add address= list=routevane4\n")
	legacy.WriteString("add address=" + nameOfLength(entries, lastNameSize) + " list=routevane4\n")
	payload := []byte(legacy.String())
	if len(payload) != MaxArtifactSize || entries+1 > MaxLines {
		t.Fatalf("legacy fixture size=%d entries=%d", len(payload), entries+1)
	}
	parsed, err := Parse(payload)
	if err != nil {
		t.Fatalf("previously publishable boundary document rejected: %v", err)
	}
	rendered, err := renderScript(parsed)
	if err != nil || !bytes.Equal(rendered, payload) {
		t.Fatalf("boundary document did not preserve exact bytes: %v", err)
	}
	parsed.legacy = false
	if _, err := renderScript(parsed); err == nil {
		t.Fatal("fixture did not exercise the new replacement overhead at the byte bound")
	}
}
