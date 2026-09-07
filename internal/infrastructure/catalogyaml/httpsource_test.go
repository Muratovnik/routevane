package catalogyaml

import (
	"context"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

const feedListYAML = `id: example
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
  - id: official-feed
    type: http
    component: web
    config:
      url: https://feeds.example.com/ranges.json
      format: json
`

func TestLoadAcceptsAnHTTPFeedSourceAndKeepsItsConfigurationSeparate(t *testing.T) {
	root := writeCatalogFile(t, "builtin", "example.yaml", []byte(feedListYAML))
	catalog, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	list, found := catalog.List("example")
	if !found || len(list.Sources) != 2 {
		t.Fatalf("service = %#v", list)
	}
	dnsSource, feedSource := list.Sources[0], list.Sources[1]
	if dnsSource.ID != "dns-main" || dnsSource.Type != domain.SourceDNS {
		t.Fatalf("dns source = %#v", dnsSource)
	}
	if dnsSource.URL != "" || dnsSource.Format != "" {
		t.Fatalf("a DNS source must not carry feed configuration: %#v", dnsSource)
	}
	if feedSource.ID != "official-feed" || feedSource.Type != domain.SourceHTTP {
		t.Fatalf("feed source = %#v", feedSource)
	}
	if feedSource.URL != "https://feeds.example.com/ranges.json" || feedSource.Format != domain.FeedFormatJSON {
		t.Fatalf("feed configuration = %#v", feedSource)
	}
	if len(feedSource.Names) != 0 {
		t.Fatalf("a feed source must not carry DNS names: %#v", feedSource)
	}
	// The DNS name set stays DNS-only so a feed URL never becomes a lookup.
	if len(list.DNSNames) != 1 || list.DNSNames[0] != "example.com" {
		t.Fatalf("dns names = %#v", list.DNSNames)
	}
	if feedSource.Revision == "" || feedSource.Revision == dnsSource.Revision {
		t.Fatalf("each source identity needs its own revision: %q %q", feedSource.Revision, dnsSource.Revision)
	}
	revisions := catalog.ActiveSourceRevisions("example")
	if revisions["official-feed"] != feedSource.Revision || revisions["dns-main"] != dnsSource.Revision {
		t.Fatalf("active revisions = %#v", revisions)
	}
}

func TestFeedSourceRevisionTracksURLAndFormat(t *testing.T) {
	base := writeCatalogFile(t, "builtin", "example.yaml", []byte(feedListYAML))
	baseCatalog, err := Load(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	baseList, _ := baseCatalog.List("example")

	cases := []struct {
		name    string
		payload string
	}{
		{"different url", strings.Replace(feedListYAML, "ranges.json", "other.json", 1)},
		{"different format", strings.Replace(feedListYAML, "format: json", "format: text", 1)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := writeCatalogFile(t, "builtin", "example.yaml", []byte(testCase.payload))
			catalog, err := Load(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			list, _ := catalog.List("example")
			if list.Sources[1].Revision == baseList.Sources[1].Revision {
				t.Fatal("changing the feed configuration must change its source revision")
			}
		})
	}
}

func TestLoadRejectsUnsafeOrMalformedFeedSources(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"plaintext url", strings.Replace(feedListYAML, "https://feeds.example.com/ranges.json", "http://feeds.example.com/ranges.json", 1)},
		{"loopback url", strings.Replace(feedListYAML, "https://feeds.example.com/ranges.json", "https://127.0.0.1/ranges.json", 1)},
		{"metadata url", strings.Replace(feedListYAML, "https://feeds.example.com/ranges.json", "https://169.254.169.254/latest", 1)},
		{"private url", strings.Replace(feedListYAML, "https://feeds.example.com/ranges.json", "https://10.1.2.3/ranges.json", 1)},
		{"credentials in url", strings.Replace(feedListYAML, "https://feeds.example.com/ranges.json", "https://user:secret@feeds.example.com/ranges.json", 1)},
		{"missing url", strings.Replace(feedListYAML, "      url: https://feeds.example.com/ranges.json\n", "", 1)},
		{"unknown format", strings.Replace(feedListYAML, "format: json", "format: xml", 1)},
		{"missing format", strings.Replace(feedListYAML, "      format: json\n", "", 1)},
		{"feed with dns names", strings.Replace(feedListYAML, "      format: json\n", "      format: json\n      names: [example.com]\n", 1)},
		{"dns source with url", strings.Replace(feedListYAML, "      names: [example.com]\n", "      names: [example.com]\n      url: https://feeds.example.com/ranges.json\n", 1)},
		{"unsupported type", strings.Replace(feedListYAML, "type: http", "type: rdap", 1)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := writeCatalogFile(t, "builtin", "example.yaml", []byte(testCase.payload))
			if _, err := Load(context.Background(), root); err == nil {
				t.Fatal("invalid feed source accepted")
			}
		})
	}
}
