package keenetic

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

type fqdnLedger struct {
	states map[string]application.ManagedFQDNOwnership
	fail   bool
}

func (m *fqdnLedger) ManagedFQDNOwnership(_ context.Context, endpoint, id string) (application.ManagedFQDNOwnership, error) {
	if s, ok := m.states[endpoint+id]; ok {
		return s, nil
	}
	return application.ManagedFQDNOwnership{Endpoint: endpoint, OutputID: id}, nil
}
func (m *fqdnLedger) ReplaceManagedFQDNOwnership(_ context.Context, s application.ManagedFQDNOwnership) error {
	if m.fail {
		return errors.New("disk failure")
	}
	m.states[s.Endpoint+s.OutputID] = s
	return nil
}
func (m *fqdnLedger) RetireManagedFQDNOwnership(context.Context, string, string) error { return nil }

type fqdnBackups struct{}

func (fqdnBackups) PutBackup(_ context.Context, _ string, at time.Time, payload []byte) (application.BackupRef, error) {
	return application.BackupRef{ID: "fixture-backup", Path: "fixture/backup", Hash: strings.Repeat("c", 64), SizeBytes: int64(len(payload)), CreatedAt: at}, nil
}

func outputFQDNArtifact(t *testing.T, outputID, prefix, list, value string) application.DeployArtifact {
	t.Helper()
	rule, err := domain.NewDomainRule(domain.RuleDomainSuffix, value, list, "web", domain.SourceCommunity, []string{"official_rule"}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := (keeneticdns.Renderer{}).RenderOutput(domain.RoutingPlan{Rules: []domain.RouteRule{rule}}, outputID, prefix)
	if err != nil {
		t.Fatal(err)
	}
	return application.DeployArtifact{ArtifactID: strings.Repeat("a", 32), RendererID: keeneticdns.ID, ArtifactHash: strings.Repeat("b", 64), Payload: payload}
}
func applyFQDNOutput(f *fqdnFixture, ledger *fqdnLedger, id string, artifact application.DeployArtifact) (application.DeployResult, error) {
	return application.DeployToDevice(context.Background(), application.DeployRequest{Target: domain.TargetDefinition{ID: "keenetic-dns", RendererID: keeneticdns.ID, FormatKey: keeneticdns.Version}, Connection: f.connection, OutputID: id, ManagedFQDN: ledger, Artifact: artifact}, application.DeployerRegistry{keeneticdns.ID: f.deployer}, fqdnBackups{}, application.ClockFunc(time.Now))
}

func TestFQDNOutputsCoexistAndPrefixMigrationOnlyRemovesTheirOwnObjects(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	f.device.fqdn["routevane-manual"] = []string{"manual.example"}
	f.device.dnsRoutes["routevane-manual"] = "ISP"
	a, b := strings.Repeat("1", 32), strings.Repeat("2", 32)
	first := outputFQDNArtifact(t, a, "", "same-list", "a.example")
	other := outputFQDNArtifact(t, b, "", "same-list", "b.example")
	for _, item := range []struct {
		id       string
		artifact application.DeployArtifact
	}{{a, first}, {b, other}} {
		if result, err := applyFQDNOutput(f, ledger, item.id, item.artifact); err != nil || !result.Applied {
			t.Fatalf("apply=%#v %v", result, err)
		}
	}
	before, _ := f.device.groupSnapshot()
	if len(before) != 3 {
		t.Fatalf("outputs clobbered groups: %v", before)
	}
	old, _ := keeneticdns.Parse(first.Payload)
	otherGroups, _ := keeneticdns.Parse(other.Payload)
	changed := outputFQDNArtifact(t, a, "custom", "same-list", "a.example")
	prior, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", a)
	changed.OwnedFQDNGroups = prior.Groups
	preview, err := f.deployer.PreviewFQDNGroups(context.Background(), f.probe(t), f.connection, changed)
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(preview, "no object-group fqdn "+old[0].Name) < 0 {
		t.Fatalf("migration did not preview owned deletion: %v", preview)
	}
	if result, err := applyFQDNOutput(f, ledger, a, changed); err != nil || !result.Applied {
		t.Fatalf("migration=%#v %v", result, err)
	}
	after, _ := f.device.groupSnapshot()
	if len(after) != 3 || !reflect.DeepEqual(after[otherGroups[0].Name], []string{"b.example"}) || !reflect.DeepEqual(after["routevane-manual"], []string{"manual.example"}) {
		t.Fatalf("migration touched foreign groups: %v", after)
	}
	if _, exists := after[old[0].Name]; exists {
		t.Fatal("owned old prefix was left behind")
	}
	count := len(f.device.commandLog())
	if _, err := applyFQDNOutput(f, ledger, a, changed); err != nil {
		t.Fatal(err)
	}
	if len(f.device.commandLog()) != count {
		t.Fatal("repeated deployment was not idempotent")
	}
}

func TestFQDNUnownedCollisionAndInterruptedApplyFailClosed(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	id := strings.Repeat("1", 32)
	artifact := outputFQDNArtifact(t, id, "", "example", "example.com")
	groups, _ := keeneticdns.Parse(artifact.Payload)
	for _, legacy := range []bool{false, true} {
		if legacy {
			artifact = fqdnArtifact(t, "example=example.com")
			groups, _ = keeneticdns.Parse(artifact.Payload)
		}
		f.device.fqdn[groups[0].Name] = []string{"example.com"}
		before := len(f.device.commandLog())
		result, err := applyFQDNOutput(f, ledger, id, artifact)
		if err == nil || result.Applied || result.RolledBack || len(f.device.commandLog()) != before {
			t.Fatalf("unowned collision wrote: %#v %v", result, err)
		}
	}
	if len(ledger.states) != 0 {
		t.Fatal("collision adopted unowned objects")
	}
}

func TestFQDNReadbackDetectsAndReconcilesOwnedAutoDrift(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	artifact := fqdnArtifact(t, "example=example.com")
	f.apply(t, artifact)
	artifact.OwnedFQDNGroups = f.owned
	f.device.mu.Lock()
	f.device.dnsAuto["routevane-example"] = false
	f.device.mu.Unlock()
	if err := f.deployer.Verify(context.Background(), f.probe(t), f.connection, artifact); !errors.Is(err, ErrDeviceAnswer) {
		t.Fatalf("auto drift was accepted: %v", err)
	}
	f.apply(t, artifact)
	f.device.mu.Lock()
	restored := f.device.dnsAuto["routevane-example"]
	f.device.mu.Unlock()
	if !restored {
		t.Fatal("owned auto drift not repaired")
	}
}

func TestFQDNLedgerFailureRollsBackAndDoesNotCommitOwnership(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}, fail: true}
	f.device.fqdn["routevane-manual"] = []string{"manual.example"}
	before, _ := f.device.groupSnapshot()
	id := strings.Repeat("1", 32)
	result, err := applyFQDNOutput(f, ledger, id, outputFQDNArtifact(t, id, "", "example", "example.com"))
	if !errors.Is(err, application.ErrOwnershipPersist) || result.Applied || !result.RolledBack || len(ledger.states) != 0 {
		t.Fatalf("ledger failure=%#v %v", result, err)
	}
	after, _ := f.device.groupSnapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("recovery lost foreign state: %v", after)
	}
}

func TestFQDNPartialDeviceFailureRestoresAndCanRetry(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	id := strings.Repeat("1", 32)
	artifact := outputFQDNArtifact(t, id, "", "example", "example.com")
	f.device.failParseAfter = 1
	result, err := applyFQDNOutput(f, ledger, id, artifact)
	if err == nil || !result.RolledBack || result.Applied || len(ledger.states) != 0 {
		t.Fatalf("partial failure=%#v %v", result, err)
	}
	groups, _ := f.device.groupSnapshot()
	if len(groups) != 0 {
		t.Fatalf("partial group not recovered: %v", groups)
	}
	f.device.failParseAfter = 0
	if result, err := applyFQDNOutput(f, ledger, id, artifact); err != nil || !result.Applied {
		t.Fatalf("retry=%#v %v", result, err)
	}
}

// Existing immutable include-only artifacts remain valid. A new build labels
// the exact owned object in place; its title is verified and included in recovery.
func TestFQDNDescriptionUpdatesOwnedLegacyGroupAndRollsBackWithItsLedger(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	id := strings.Repeat("1", 32)
	current := outputFQDNArtifact(t, id, "", "claude", "claude.ai")
	current.Payload = []byte(strings.ReplaceAll(string(current.Payload), `description "claude"`, `description "Claude"`))
	parsed, err := keeneticdns.Parse(current.Payload)
	if err != nil {
		t.Fatal(err)
	}
	name := parsed[0].Name
	legacy := current
	legacy.Payload = []byte("object-group fqdn " + name + " include claude.ai\n")
	f.device.fqdn["manual"] = []string{"manual.example"}
	f.device.fqdnDescriptions["manual"] = "Operator list"
	if result, err := applyFQDNOutput(f, ledger, id, legacy); err != nil || !result.Applied {
		t.Fatalf("legacy apply=%#v %v", result, err)
	}
	before := len(f.device.commandLog())
	if result, err := applyFQDNOutput(f, ledger, id, current); err != nil || !result.Applied {
		t.Fatalf("label migration=%#v %v", result, err)
	}
	want := []string{"object-group fqdn " + name + ` description "Claude"`, "system configuration save"}
	if got := f.device.commandLog()[before:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("label migration recreated or rerouted a group: %v", got)
	}
	groups, _ := f.device.groupSnapshot()
	if len(groups) != 2 {
		t.Fatalf("migration created duplicate groups: %v", groups)
	}
	before = len(f.device.commandLog())
	for _, artifact := range []application.DeployArtifact{legacy, current, current} {
		if _, err := applyFQDNOutput(f, ledger, id, artifact); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.device.commandLog()) != before {
		t.Fatal("same or legacy artifact rewrote a correctly labelled group")
	}
	prior, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", id)
	changed := current
	changed.Payload = []byte(strings.ReplaceAll(string(current.Payload), `description "Claude"`, `description "Claude work"`))
	ledger.fail = true
	result, err := applyFQDNOutput(f, ledger, id, changed)
	if !errors.Is(err, application.ErrOwnershipPersist) || result.Applied || !result.RolledBack {
		t.Fatalf("label ledger failure=%#v %v", result, err)
	}
	f.device.mu.Lock()
	label, foreign := f.device.fqdnDescriptions[name], f.device.fqdnDescriptions["manual"]
	f.device.mu.Unlock()
	after, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", id)
	if label != "Claude" || foreign != "Operator list" || !reflect.DeepEqual(prior, after) {
		t.Fatalf("label recovery lost prior state: label=%q foreign=%q ledger=%#v", label, foreign, after)
	}
	current.OwnedFQDNGroups = prior.Groups
	f.device.mu.Lock()
	f.device.fqdnDescriptions[name] = ""
	f.device.mu.Unlock()
	if err := f.deployer.Verify(context.Background(), f.probe(t), f.connection, current); !errors.Is(err, ErrDeviceAnswer) {
		t.Fatalf("missing readable label passed read-back: %v", err)
	}
}

// A router configured before readable names holds hashed names. The next
// deployment of an output replaces the hashed groups its own ledger records
// with the readable ones and leaves another output's hashed group and a manual
// group alone, even when both carry the same list.
func TestFQDNReadableNamesReplaceOnlyTheOutputsOwnHashedGroups(t *testing.T) {
	f := newFQDNFixture(t, "5.1.2")
	ledger := &fqdnLedger{states: map[string]application.ManagedFQDNOwnership{}}
	f.device.fqdn["routevane-manual"] = []string{"manual.example"}
	f.device.dnsRoutes["routevane-manual"] = "ISP"
	a, b := strings.Repeat("1", 32), strings.Repeat("2", 32)
	ownHashed, otherHashed := "routevane-db842808ecae-60965168ce762e94-s1", "routevane-0d6ba19b6253-60965168ce762e94-s1"
	for _, earlier := range []struct{ id, name string }{{a, ownHashed}, {b, otherHashed}} {
		artifact := outputFQDNArtifact(t, earlier.id, "", "chatgpt", "chatgpt.com")
		artifact.Payload = []byte("object-group fqdn " + earlier.name + " description \"ChatGPT\"\n" +
			"object-group fqdn " + earlier.name + " include chatgpt.com\n")
		if result, err := applyFQDNOutput(f, ledger, earlier.id, artifact); err != nil || !result.Applied {
			t.Fatalf("earlier hashed build of %s: %#v %v", earlier.id, result, err)
		}
	}
	otherBefore, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", b)

	current := outputFQDNArtifact(t, a, "", "chatgpt", "chatgpt.com")
	readable := "routevane-chatgpt-8a8366-s1"
	if groups, err := keeneticdns.Parse(current.Payload); err != nil || len(groups) != 1 || groups[0].Name != readable {
		t.Fatalf("current build groups=%#v err=%v", groups, err)
	}
	prior, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", a)
	current.OwnedFQDNGroups = prior.Groups
	preview, err := f.deployer.PreviewFQDNGroups(context.Background(), f.probe(t), f.connection, current)
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(preview, "no object-group fqdn "+ownHashed) < 0 {
		t.Fatalf("preview does not remove the output's own hashed group: %v", preview)
	}
	for _, command := range preview {
		if strings.Contains(command, otherHashed) || strings.Contains(command, "routevane-manual") {
			t.Fatalf("preview touches a group this output does not own: %q", command)
		}
	}
	current.OwnedFQDNGroups = nil
	if result, err := applyFQDNOutput(f, ledger, a, current); err != nil || !result.Applied {
		t.Fatalf("readable build=%#v %v", result, err)
	}

	groups, routes := f.device.groupSnapshot()
	wantGroups := map[string][]string{
		readable:           {"chatgpt.com"},
		otherHashed:        {"chatgpt.com"},
		"routevane-manual": {"manual.example"},
	}
	wantRoutes := map[string]string{readable: deviceInterface, otherHashed: deviceInterface, "routevane-manual": "ISP"}
	if !reflect.DeepEqual(groups, wantGroups) || !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatalf("device groups=%v routes=%v", groups, routes)
	}
	owned, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", a)
	if len(owned.Groups) != 1 || owned.Groups[0].Name != readable {
		t.Fatalf("ledger of the deployed output = %#v", owned.Groups)
	}
	otherAfter, _ := ledger.ManagedFQDNOwnership(context.Background(), "http://10.255.0.1", b)
	if !reflect.DeepEqual(otherBefore, otherAfter) {
		t.Fatalf("another output's ledger changed: %#v", otherAfter)
	}
}
