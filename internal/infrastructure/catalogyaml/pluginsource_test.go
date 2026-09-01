package catalogyaml

import (
	"context"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

const pluginSourceServiceYAML = `id: example
title: Example
components:
  core:
    required: true
seeds:
  - kind: domain_suffix
    value: example.test
    component: core
    source: manual
sources:
  - id: static
    type: example-static
    revision: example-static-v1
    component: core
    config:
      names: [static.example.test, edge.example.test]
`

func TestLoadAcceptsAnExternalSourceAndBindsItsRevision(t *testing.T) {
	root := writeCatalogFile(t, "builtin", "example.yaml", []byte(pluginSourceServiceYAML))
	catalog, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	service, found := catalog.Service("example")
	if !found || len(service.Sources) != 1 {
		t.Fatalf("service = %#v", service)
	}
	source := service.Sources[0]
	if source.Type != domain.SourceType("example-static") || source.ImplementationRevision != "example-static-v1" || source.Revision == "" {
		t.Fatalf("external source = %#v", source)
	}
	if source.Class != domain.SourceCommunity {
		t.Fatalf("external source class = %q", source.Class)
	}
	if len(service.DNSNames) != 0 {
		t.Fatalf("external request names leaked into DNS names: %#v", service.DNSNames)
	}

	changed := strings.Replace(pluginSourceServiceYAML, "example-static-v1", "example-static-v2", 1)
	changedRoot := writeCatalogFile(t, "builtin", "example.yaml", []byte(changed))
	changedCatalog, err := Load(context.Background(), changedRoot)
	if err != nil {
		t.Fatal(err)
	}
	changedService, _ := changedCatalog.Service("example")
	if changedService.Sources[0].Revision == source.Revision {
		t.Fatal("changing the plugin implementation revision did not retire old observations")
	}
}

func TestLoadRejectsMalformedExternalSourceConfiguration(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"missing implementation revision", strings.Replace(pluginSourceServiceYAML, "    revision: example-static-v1\n", "", 1)},
		{"invalid type", strings.Replace(pluginSourceServiceYAML, "type: example-static", "type: ExampleStatic", 1)},
		{"invalid revision", strings.Replace(pluginSourceServiceYAML, "example-static-v1", "Version 1", 1)},
		{"missing names", strings.Replace(pluginSourceServiceYAML, "      names: [static.example.test, edge.example.test]\n", "", 1)},
		{"invalid name", strings.Replace(pluginSourceServiceYAML, "static.example.test", "bad name", 1)},
		{"unexpected url", strings.Replace(pluginSourceServiceYAML, "      names:", "      url: https://example.com/feed\n      names:", 1)},
		{"unexpected class", strings.Replace(pluginSourceServiceYAML, "      names:", "      class: official\n      names:", 1)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := writeCatalogFile(t, "builtin", "example.yaml", []byte(testCase.payload))
			if _, err := Load(context.Background(), root); err == nil {
				t.Fatal("invalid external source accepted")
			}
		})
	}
}
