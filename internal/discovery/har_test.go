package discovery

import (
	"errors"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

const harFixture = `{
  "log": {
    "version": "1.2",
    "pages": [
      {"id": "page-1", "title": "Home", "comment": "core"},
      {"id": "page-2", "title": "auth"},
      {"id": "page-3", "title": "Player", "comment": "media"},
      {"id": "page-4", "title": "unlabelled section"}
    ],
    "entries": [
      {"pageref": "page-1", "request": {"method": "GET", "url": "https://app.example.co.uk/", "headers": [{"name": "Accept", "value": "text/html,application/xhtml+xml"}]}, "response": {"status": 200}},
      {"pageref": "page-1", "request": {"method": "GET", "url": "https://static.example.co.uk/app.css", "headers": []}, "response": {"status": 200}},
      {"pageref": "page-1", "request": {"method": "GET", "url": "https://metrics.example.co.uk/beacon", "headers": []}, "response": {"status": 204}},
      {"pageref": "page-2", "request": {"method": "GET", "url": "https://app.example.co.uk/login", "headers": [{"name": "accept", "value": "text/html"}]}, "response": {"status": 302, "redirectURL": "https://auth.example.co.uk/authorize"}},
      {"pageref": "page-2", "request": {"method": "GET", "url": "https://auth.example.co.uk/authorize", "headers": []}, "response": {"status": 200}},
      {"pageref": "page-3", "request": {"method": "GET", "url": "https://app.example.co.uk/watch", "headers": [{"name": "Accept", "value": "text/html"}]}, "response": {"status": 200}},
      {"pageref": "page-3", "request": {"method": "GET", "url": "https://media.example.co.uk/segment.ts", "headers": []}, "response": {"status": 200}},
      {"pageref": "page-3", "request": {"method": "GET", "url": "https://cdn.thirdparty.test/player.js", "headers": []}, "response": {"status": 200}},
      {"pageref": "page-4", "request": {"method": "GET", "url": "https://unlabelled.example.co.uk/x", "headers": []}, "response": {"status": 200}},
      {"pageref": "page-1", "request": {"method": "GET", "url": "not a url", "headers": []}, "response": {"status": 200}},
      {"pageref": "page-1", "request": {"method": "GET", "url": "https://app.example.co.uk/relative", "headers": []}, "response": {"status": 302, "redirectURL": "/next"}}
    ]
  }
}`

func TestImportHARAttributesComponentsAndProvenance(t *testing.T) {
	target := testTarget(t, "https://app.example.co.uk/")
	evidence, err := ImportHAR(target, []byte(harFixture))
	if err != nil {
		t.Fatal(err)
	}
	byHost := map[string]HostEvidence{}
	for _, host := range evidence.Hosts {
		byHost[host.Host] = host
	}
	// A page whose comment or title names a component attributes its requests.
	if byHost["static.example.co.uk"].Component != domain.ComponentCore {
		t.Fatalf("static = %#v", byHost["static.example.co.uk"])
	}
	if byHost["auth.example.co.uk"].Component != domain.ComponentAuth {
		t.Fatalf("auth = %#v", byHost["auth.example.co.uk"])
	}
	if byHost["media.example.co.uk"].Component != domain.ComponentMedia {
		t.Fatalf("media = %#v", byHost["media.example.co.uk"])
	}
	if byHost["metrics.example.co.uk"].Component != domain.ComponentCore {
		t.Fatalf("a request on the core page is attributed to core: %#v", byHost["metrics.example.co.uk"])
	}
	// A page with no taxonomy label leaves its hosts unattributed rather than
	// guessing, so the activation policy keeps them as dependencies.
	if byHost["unlabelled.example.co.uk"].Component != "" {
		t.Fatalf("unlabelled = %#v", byHost["unlabelled.example.co.uk"])
	}
	// The page's own document host is the loader of its subresources.
	if strings.Join(byHost["static.example.co.uk"].LoadedBy, ",") != "app.example.co.uk" {
		t.Fatalf("loaded_by = %v", byHost["static.example.co.uk"].LoadedBy)
	}
	if len(byHost["app.example.co.uk"].LoadedBy) != 0 {
		t.Fatalf("a document must not load itself: %#v", byHost["app.example.co.uk"])
	}
	// An absolute redirect is a dependency; a relative one adds nothing.
	if strings.Join(byHost["app.example.co.uk"].RedirectsTo, ",") != "auth.example.co.uk" {
		t.Fatalf("redirects_to = %v", byHost["app.example.co.uk"].RedirectsTo)
	}
	if strings.Join(evidence.Steps, ",") != "auth,core,media" {
		t.Fatalf("steps = %v", evidence.Steps)
	}
	if _, present := byHost["not a url"]; present {
		t.Fatal("an unusable entry must not become a host")
	}

	decisions := Classify(evidence, evidence.Steps)
	outcomes := map[string]string{}
	for _, decision := range decisions {
		outcomes[decision.Host] = decision.Outcome
	}
	for _, host := range []string{"app.example.co.uk", "static.example.co.uk", "auth.example.co.uk", "media.example.co.uk"} {
		if outcomes[host] != ActivationAccepted {
			t.Fatalf("%q = %q, want accepted", host, outcomes[host])
		}
	}
	for _, host := range []string{"cdn.thirdparty.test", "unlabelled.example.co.uk"} {
		if outcomes[host] != ActivationDependency {
			t.Fatalf("%q = %q, want dependency", host, outcomes[host])
		}
	}
}

func TestImportHARRefusesUnusableArchives(t *testing.T) {
	target := testTarget(t, "https://app.example.co.uk/")
	cases := map[string]string{
		"empty":            "",
		"not json":         "{not json",
		"no log":           `{}`,
		"no entries":       `{"log":{"version":"1.2","entries":[]}}`,
		"no usable entry":  `{"log":{"version":"1.2","entries":[{"request":{"method":"GET","url":"not a url","headers":[]},"response":{"status":200}}]}}`,
		"unusable schemes": `{"log":{"version":"1.2","entries":[{"request":{"method":"GET","url":"data:text/plain,hello","headers":[]},"response":{"status":200}}]}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ImportHAR(target, []byte(payload)); !errors.Is(err, ErrInvalidHAR) {
				t.Fatalf("err = %v, want ErrInvalidHAR", err)
			}
		})
	}
}

func TestImportHARIsDeterministic(t *testing.T) {
	target := testTarget(t, "https://app.example.co.uk/")
	first, err := ImportHAR(target, []byte(harFixture))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ImportHAR(target, []byte(harFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Hosts) != len(second.Hosts) {
		t.Fatalf("host counts differ: %d vs %d", len(first.Hosts), len(second.Hosts))
	}
	for index := range first.Hosts {
		if first.Hosts[index].Host != second.Hosts[index].Host || first.Hosts[index].Component != second.Hosts[index].Component {
			t.Fatalf("import is not deterministic at %d: %#v vs %#v", index, first.Hosts[index], second.Hosts[index])
		}
	}
}

func FuzzImportHARNeverYieldsAnInvalidHost(f *testing.F) {
	f.Add(harFixture)
	f.Add(`{"log":{"entries":[{"request":{"url":"https://a.example.com/"}}]}}`)
	f.Add("{}")
	f.Fuzz(func(t *testing.T, payload string) {
		target, err := NormalizeTarget("https://app.example.com/")
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := ImportHAR(target, []byte(payload))
		if err != nil {
			return
		}
		if len(evidence.Hosts) == 0 {
			t.Fatal("a successful import must yield at least one host")
		}
		for _, host := range evidence.Hosts {
			if _, err := domain.NormalizeDomain(host.Host); err != nil {
				t.Fatalf("imported an invalid host %q", host.Host)
			}
			if host.Component != "" && !domain.KnownComponent(host.Component) {
				t.Fatalf("imported an unknown component %q", host.Component)
			}
		}
	})
}
