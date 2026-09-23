package discovery

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// newScenarioFixture serves a site whose areas live on different same-site
// hosts, so a step can be attributed to the action that reached it.
func newScenarioFixture(t *testing.T) *pageFixture {
	t.Helper()
	certificate, pin := selfSignedCertificate(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body><img src="https://static.page.test/logo.png" alt=""></body></html>`))
		case "/login":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body><img src="https://auth.page.test/token.png" alt=""></body></html>`))
		case "/watch":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>` +
				`<img src="https://media.page.test/segment.png" alt="">` +
				`<img src="https://beacon.page.test/t.png" alt="">` +
				`<img src="https://cdn.thirdparty.test/player.png" alt="">` +
				`</body></html>`))
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
		}
	})
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	return &pageFixture{
		server: server,
		pin:    pin,
		hosts: map[string][]netip.Addr{
			"page.test":                {netip.MustParseAddr("203.0.113.10")},
			"static.page.test":         {netip.MustParseAddr("203.0.113.11")},
			"auth.page.test":           {netip.MustParseAddr("203.0.113.12")},
			"media.page.test":          {netip.MustParseAddr("203.0.113.13")},
			"beacon.page.test":         {netip.MustParseAddr("203.0.113.14")},
			"cdn.thirdparty.test":      {netip.MustParseAddr("203.0.113.20")},
			"internal.thirdparty.test": {netip.MustParseAddr("10.1.2.3")},
		},
	}
}

func TestRunScenarioAttributesHostsToTheStepThatReachedThem(t *testing.T) {
	fixture := newScenarioFixture(t)
	options := fixture.options(t)
	scenario := Scenario{
		Target: "https://page.test/",
		Steps: []Step{
			{ID: "open", Component: domain.ComponentCore, URL: "https://page.test/", SettleSeconds: 1},
			{ID: "sign-in", Component: domain.ComponentAuth, URL: "https://page.test/login", SettleSeconds: 1},
			{ID: "play", Component: domain.ComponentMedia, URL: "https://page.test/watch", SettleSeconds: 2},
		},
	}
	evidence, err := RunScenario(context.Background(), scenario, options)
	if err != nil {
		t.Fatalf("scenario = %v (hosts=%#v)", err, evidence.Hosts)
	}
	byHost := map[string]HostEvidence{}
	for _, host := range evidence.Hosts {
		byHost[host.Host] = host
	}
	// Each host carries the component of the step during which it appeared.
	expected := map[string]string{
		"static.page.test":    domain.ComponentCore,
		"auth.page.test":      domain.ComponentAuth,
		"media.page.test":     domain.ComponentMedia,
		"beacon.page.test":    domain.ComponentMedia,
		"cdn.thirdparty.test": domain.ComponentMedia,
	}
	for host, component := range expected {
		if byHost[host].Component != component {
			t.Fatalf("%q = %#v, want component %q", host, byHost[host], component)
		}
	}
	if strings.Join(byHost["media.page.test"].StepIDs, ",") != "play" {
		t.Fatalf("media steps = %v", byHost["media.page.test"].StepIDs)
	}
	// The document host of the step is recorded as the loader.
	if !containsHost(byHost["media.page.test"].LoadedBy, "page.test") {
		t.Fatalf("loaded_by = %v", byHost["media.page.test"].LoadedBy)
	}
	if strings.Join(evidence.Steps, ",") != "open,play,sign-in" {
		t.Fatalf("steps = %v", evidence.Steps)
	}

	decisions := Classify(evidence, scenario.ExercisedComponents())
	outcomes := map[string]string{}
	for _, decision := range decisions {
		outcomes[decision.Host] = decision.Outcome
	}
	for _, host := range []string{"page.test", "static.page.test", "auth.page.test", "media.page.test"} {
		if outcomes[host] != ActivationAccepted {
			t.Fatalf("%q = %q, want accepted", host, outcomes[host])
		}
	}
	// A third party stays a dependency even though it was reached during a
	// required step.
	if outcomes["cdn.thirdparty.test"] != ActivationDependency {
		t.Fatalf("third party = %q", outcomes["cdn.thirdparty.test"])
	}

	draft, err := BuildLearnedDraft(LearnedDraftRequest{Target: evidence.Target, ListID: "page", Evidence: evidence, Exercised: scenario.ExercisedComponents()})
	if err != nil {
		t.Fatal(err)
	}
	components := make([]string, 0, len(draft.Definition.Components))
	for _, component := range draft.Definition.Components {
		components = append(components, component.ID)
	}
	if strings.Join(components, ",") != "core,auth,media" {
		t.Fatalf("components = %v", components)
	}
}

func TestRunScenarioRemovesItsProfileAndRefusesUnsafeScenarios(t *testing.T) {
	fixture := newScenarioFixture(t)
	options := fixture.options(t)
	scenario := Scenario{Target: "https://unmapped.test/", Steps: []Step{
		{ID: "open", Component: domain.ComponentCore, URL: "https://unmapped.test/", SettleSeconds: 1},
	}}
	if _, err := RunScenario(context.Background(), scenario, options); err == nil {
		t.Fatal("a scenario whose first step cannot load must report an error")
	}
	entries, err := os.ReadDir(options.UserDataParent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a failed session left %d entries behind", len(entries))
	}

	local := Scenario{Target: "https://page.test/", Steps: []Step{
		{ID: "open", Component: domain.ComponentCore, URL: "http://localhost:9/", SettleSeconds: 1},
	}}
	if _, err := RunScenario(context.Background(), local, options); !errors.Is(err, ErrInvalidScenario) {
		t.Fatalf("err = %v, want ErrInvalidScenario", err)
	}
}

func TestRunScenarioRefusesAnUnconfiguredBrowser(t *testing.T) {
	scenario := Scenario{Target: "https://page.test/", Steps: []Step{
		{ID: "open", Component: domain.ComponentCore, URL: "https://page.test/", SettleSeconds: 1},
	}}
	for _, unusable := range unusableBrowserPaths(t) {
		profiles := t.TempDir()
		_, err := RunScenario(context.Background(), scenario, BrowserOptions{ExecPath: unusable.path, UserDataParent: profiles})
		assertBrowserRefused(t, err, unusable, profiles)
	}
}

func TestRunScenarioHonorsCancellation(t *testing.T) {
	fixture := newScenarioFixture(t)
	options := fixture.options(t)
	options.Timeout = 60 * time.Second
	scenario := Scenario{Target: "https://page.test/", Steps: []Step{
		{ID: "open", Component: domain.ComponentCore, URL: "https://page.test/", SettleSeconds: 1},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunScenario(ctx, scenario, options); err == nil {
		t.Fatal("a cancelled scenario must report an error")
	}
	entries, err := os.ReadDir(options.UserDataParent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a cancelled session left %d entries behind", len(entries))
	}
}
