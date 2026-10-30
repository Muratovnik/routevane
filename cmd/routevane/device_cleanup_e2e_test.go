package main

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

// ADR 0060 end to end: retiring a format or a profile can first remove from
// the router what it delivered there. These run the whole service — saved
// connections and passwords, the real Keenetic deployers, SQLite ledgers and
// journals — against the device doubles.

// cleanupSecrets keeps saved passwords in the test, never in the workstation's
// credential store.
type cleanupSecrets struct {
	mu      sync.Mutex
	secrets map[string]string
}

func newCleanupSecrets() *cleanupSecrets { return &cleanupSecrets{secrets: map[string]string{}} }

func (*cleanupSecrets) Available() bool { return true }
func (s *cleanupSecrets) PutSecret(_ context.Context, key, _, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[key] = secret
	return nil
}
func (s *cleanupSecrets) Secret(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.secrets[key]
	if !ok {
		return "", application.ErrNotFound
	}
	return secret, nil
}
func (s *cleanupSecrets) DeleteSecret(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, key)
	return nil
}

// cleanupReply is every field a retirement answers with, success or refusal.
type cleanupReply struct {
	Error     string                             `json:"error"`
	Code      string                             `json:"code"`
	Step      string                             `json:"step"`
	Field     string                             `json:"field"`
	Retired   *bool                              `json:"retired"`
	RemovedAt string                             `json:"removed_at"`
	DeletedAt string                             `json:"deleted_at"`
	Cleanups  []application.DeviceCleanupAttempt `json:"device_cleanup"`
	Blocked   []application.DeviceObjects        `json:"device_objects"`
	Object    *application.DeviceObjects         `json:"device_object"`
}

func retire(t *testing.T, endpoint, body string) (int, cleanupReply) {
	t.Helper()
	response := postGuardedBody(t, endpoint, body)
	var reply cleanupReply
	if err := json.Unmarshal(response.body, &reply); err != nil {
		t.Fatalf("POST %s status=%d body=%s err=%v", endpoint, response.status, response.body, err)
	}
	return response.status, reply
}

type deviceObjectsReply struct {
	Objects   []application.DeviceObjects `json:"device_objects"`
	Cleanable bool                        `json:"cleanable"`
}

func readDeviceObjects(t *testing.T, endpoint string) deviceObjectsReply {
	t.Helper()
	response := httpGet(t, endpoint, nil)
	var reply deviceObjectsReply
	if response.status != http.StatusOK || json.Unmarshal(response.body, &reply) != nil {
		t.Fatalf("GET %s status=%d body=%s", endpoint, response.status, response.body)
	}
	return reply
}

func registerSavedConnection(t *testing.T, origin, targetID, name string) string {
	t.Helper()
	var created struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	}
	body := postJSON(t, origin+"/v1/devices", `{"target_id":`+mustQuote(t, targetID)+`,"name":`+mustQuote(t, name)+`,"address":"http://10.255.0.1","account":"`+deviceUser+`","interface":"`+deviceInterfaceName+`"}`)
	if err := json.Unmarshal(body, &created); err != nil || len(created.Device.ID) != 32 {
		t.Fatalf("register connection=%s err=%v", body, err)
	}
	return created.Device.ID
}

// sendThroughSavedConnection delivers one artifact through a saved connection,
// with a password typed for this send when one is given.
func sendThroughSavedConnection(t *testing.T, origin, artifactID, deviceID, attemptID, password string) {
	t.Helper()
	request := map[string]any{
		"attempt_id": attemptID, "confirm": true, "device_id": deviceID,
		"expected_connection": map[string]string{"device": "http://10.255.0.1", "username": deviceUser, "interface": deviceInterfaceName},
	}
	if password != "" {
		request["password"] = password
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response := postGuardedBody(t, origin+"/v1/artifacts/"+artifactID+"/deploy", string(body))
	if response.status != http.StatusOK || !strings.Contains(string(response.body), `"status":"succeeded"`) {
		t.Fatalf("send status=%d body=%s", response.status, response.body)
	}
}

func cleanupSteps(attempt application.DeviceCleanupAttempt) string {
	steps := make([]string, 0, len(attempt.Result.Events))
	for _, event := range attempt.Result.Events {
		steps = append(steps, event.Step+"="+event.Outcome)
	}
	return strings.Join(steps, ",")
}

func TestRemovingAFormatCanTakeItsFQDNGroupsOffTheRouterAndNothingElse(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	target, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", "keenetic-dns.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalog, "targets", "keenetic-dns.yaml"), target, 0o600); err != nil {
		t.Fatal(err)
	}
	device := newFQDNE2EDevice()
	// The operator's own group and its route are on the router before
	// Routevane writes anything.
	device.groups["manual"] = fqdnE2EGroup{Description: "Operator list", Entries: []string{"manual.example"}}
	device.routes["manual"] = deviceInterfaceName
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()
	data := filepath.Join(t.TempDir(), "data")
	deps := runtimeDeps{DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()}, Secrets: newCleanupSecrets()}
	origin, stop, done, stderr := startServeServer(t, catalog, data, deps)
	serverStopped := false
	stopServer := func() {
		if serverStopped {
			return
		}
		serverStopped = true
		stop()
		if code := <-done; code != 0 {
			t.Errorf("serve=%d stderr=%s", code, stderr.String())
		}
	}
	defer stopServer()

	customList := func(title, domain string) string {
		t.Helper()
		var created struct {
			List struct{ ID string } `json:"list"`
		}
		body := postJSON(t, origin+"/v1/lists", `{"title":`+mustQuote(t, title)+`,"domains":[`+mustQuote(t, domain)+`]}`)
		if err := json.Unmarshal(body, &created); err != nil || created.List.ID == "" {
			t.Fatalf("create list=%s err=%v", body, err)
		}
		return created.List.ID
	}
	removedProfile, removed, _ := createProfileOutput(t, origin, "Removed", "keenetic-dns", customList("Claude", "claude.ai"))
	keptProfile, kept, _ := createProfileOutput(t, origin, "Kept", "keenetic-dns", customList("Other", "other.example"))
	plainProfile, plain, _ := createProfileOutput(t, origin, "Plain", "keenetic-dns", customList("Plain", "plain.example"))
	connection := registerSavedConnection(t, origin, "keenetic-dns", "Home router")
	postJSON(t, origin+"/v1/devices/"+connection+"/credential", `{"credential":"`+devicePassword+`"}`)
	for index, delivered := range [][2]string{{removedProfile, removed}, {keptProfile, kept}, {plainProfile, plain}} {
		artifactID := refreshAndBuild(t, origin, delivered[0], delivered[1]).Artifact.ID
		sendThroughSavedConnection(t, origin, artifactID, connection, strings.Repeat(string(rune('1'+index)), 32), "")
	}
	groupsBefore, routesBefore, _ := device.snapshot()
	if len(groupsBefore) != 4 || len(routesBefore) != 4 {
		t.Fatalf("delivered state: groups=%v routes=%v", groupsBefore, routesBefore)
	}

	// Without the opt-in a removal stays what it was: nothing on the router
	// changes, and the ledger keeps its rows as evidence (checked below).
	if status, reply := retire(t, origin+"/v1/outputs/"+plain+"/remove", `{}`); status != http.StatusOK || reply.RemovedAt == "" || reply.Cleanups != nil {
		t.Fatalf("plain removal status=%d reply=%+v", status, reply)
	}
	if groups, routes, _ := device.snapshot(); !reflect.DeepEqual(groups, groupsBefore) || !reflect.DeepEqual(routes, routesBefore) {
		t.Fatalf("a removal without the opt-in changed the router: groups=%v routes=%v", groups, routes)
	}

	// The dialog reads what the format owns, on which saved connection, before
	// anything is decided.
	objects := readDeviceObjects(t, origin+"/v1/outputs/"+removed+"/device-objects")
	if len(objects.Objects) != 1 || !objects.Cleanable {
		t.Fatalf("device objects = %+v", objects)
	}
	owned := objects.Objects[0]
	if owned.OutputID != removed || owned.TargetID != "keenetic-dns" || owned.Endpoint != "http://10.255.0.1" || owned.Interface != deviceInterfaceName ||
		owned.Connection == nil || owned.Connection.ID != connection || owned.Connection.Name != "Home router" || !owned.Connection.CredentialStored ||
		!owned.Cleanable || owned.BlockedBy != "" || len(owned.FQDNGroups) != 1 || owned.Routes != nil {
		t.Fatalf("owned objects = %+v connection=%+v", owned, owned.Connection)
	}
	removedGroup := owned.FQDNGroups[0]
	if _, delivered := groupsBefore[removedGroup]; !delivered || routesBefore[removedGroup] != deviceInterfaceName {
		t.Fatalf("the listed group %q is not the delivered one: %v", removedGroup, groupsBefore)
	}
	assertUntouched := func(stage string) {
		t.Helper()
		groups, routes, _ := device.snapshot()
		if !reflect.DeepEqual(groups, groupsBefore) || !reflect.DeepEqual(routes, routesBefore) {
			t.Fatalf("%s: router changed: groups=%v routes=%v", stage, groups, routes)
		}
		if live := httpGet(t, origin+"/v1/outputs/"+removed, nil); live.status != http.StatusOK {
			t.Fatalf("%s: the format was retired: %d %s", stage, live.status, live.body)
		}
		if again := readDeviceObjects(t, origin+"/v1/outputs/"+removed+"/device-objects"); len(again.Objects) != 1 || !slices.Equal(again.Objects[0].FQDNGroups, owned.FQDNGroups) {
			t.Fatalf("%s: the ledger changed: %+v", stage, again)
		}
	}

	// A router that no longer takes the saved password refuses the whole act
	// at the probe: nothing is written and nothing is retired.
	device.base.mu.Lock()
	device.base.authenticated, device.base.refuseLogin = false, true
	device.base.mu.Unlock()
	_, _, commandsBefore := device.snapshot()
	status, reply := retire(t, origin+"/v1/outputs/"+removed+"/remove", `{"remove_from_devices":true}`)
	if status != http.StatusUnprocessableEntity || reply.Error != "device_auth_rejected" || reply.Code != "device_auth_rejected" || reply.Field != "credential" ||
		reply.Step != "probe" || reply.Retired == nil || *reply.Retired || len(reply.Cleanups) != 0 || reply.Object == nil || reply.Object.OutputID != removed {
		t.Fatalf("refused login status=%d reply=%+v", status, reply)
	}
	if _, _, commands := device.snapshot(); commands != commandsBefore {
		t.Fatal("a refused login submitted commands")
	}
	assertUntouched("refused login")
	device.base.mu.Lock()
	device.base.refuseLogin = false
	device.base.mu.Unlock()

	// A removal the router does not show afterwards is rolled back from the
	// verified backup, and nothing is retired.
	device.mu.Lock()
	device.keepDeletedGroups = true
	device.mu.Unlock()
	status, reply = retire(t, origin+"/v1/outputs/"+removed+"/remove", `{"remove_from_devices":true}`)
	if status != http.StatusConflict || reply.Error != "verify_failed" || reply.Step != "verify" || reply.Retired == nil || *reply.Retired ||
		len(reply.Cleanups) != 1 || reply.Cleanups[0].Status != application.DeploymentAttemptFailed || reply.Cleanups[0].Error != "verify_failed" ||
		!reply.Cleanups[0].Result.RolledBack || reply.Cleanups[0].Result.Applied || reply.Cleanups[0].DeviceID != connection {
		t.Fatalf("failed verification status=%d reply=%+v", status, reply)
	}
	if steps := cleanupSteps(reply.Cleanups[0]); steps != "probe=success,backup=success,remove=success,verify=failed,rollback=success" {
		t.Fatalf("failed verification steps = %s", steps)
	}
	assertUntouched("failed verification")
	device.mu.Lock()
	device.keepDeletedGroups = false
	device.mu.Unlock()

	// The format's own group leaves the router with its route; another
	// format's group and the operator's group stay exactly as they were.
	status, reply = retire(t, origin+"/v1/outputs/"+removed+"/remove", `{"remove_from_devices":true}`)
	if status != http.StatusOK || reply.RemovedAt == "" || reply.Retired != nil || len(reply.Cleanups) != 1 ||
		reply.Cleanups[0].Status != application.DeploymentAttemptSucceeded || !reply.Cleanups[0].Result.Applied ||
		reply.Cleanups[0].OutputID != removed || reply.Cleanups[0].DeviceID != connection || reply.Cleanups[0].Endpoint != "http://10.255.0.1" || len(reply.Cleanups[0].ID) != 32 {
		t.Fatalf("removal status=%d reply=%+v", status, reply)
	}
	if steps := cleanupSteps(reply.Cleanups[0]); steps != "probe=success,backup=success,remove=success,verify=success,ownership=success" {
		t.Fatalf("removal steps = %s", steps)
	}
	wantGroups, wantRoutes := maps.Clone(groupsBefore), maps.Clone(routesBefore)
	delete(wantGroups, removedGroup)
	delete(wantRoutes, removedGroup)
	if groups, routes, _ := device.snapshot(); !reflect.DeepEqual(groups, wantGroups) || !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatalf("after removal: groups=%v routes=%v, want groups=%v routes=%v", groups, routes, wantGroups, wantRoutes)
	}
	for _, path := range []string{"/v1/outputs/" + removed, "/v1/outputs/" + removed + "/device-objects"} {
		if gone := httpGet(t, origin+path, nil); gone.status != http.StatusNotFound {
			t.Fatalf("GET %s after removal = %d %s", path, gone.status, gone.body)
		}
	}
	if other := readDeviceObjects(t, origin+"/v1/outputs/"+kept+"/device-objects"); len(other.Objects) != 1 || other.Objects[0].OutputID != kept || len(other.Objects[0].FQDNGroups) != 1 {
		t.Fatalf("another format's objects = %+v", other)
	}

	stopServer()
	store, err := sqlite.OpenExisting(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Both attempts that reached the device are recorded, the failed one first.
	attempts, err := store.DeviceCleanupAttempts(context.Background(), removed)
	if err != nil || len(attempts) != 2 || attempts[0].Status != application.DeploymentAttemptFailed || attempts[0].Error != "verify_failed" ||
		attempts[1].Status != application.DeploymentAttemptSucceeded || attempts[1].ID != reply.Cleanups[0].ID || !attempts[1].Result.Applied {
		t.Fatalf("cleanup journal = %+v err=%v", attempts, err)
	}
	if endpoints, err := store.OutputFQDNEndpoints(context.Background(), removed); err != nil || len(endpoints) != 0 {
		t.Fatalf("the removed format still owns groups at %v err=%v", endpoints, err)
	}
	// The format removed without the opt-in keeps its ledger rows as evidence
	// that the groups on the router are Routevane's.
	endpoints, err := store.OutputFQDNEndpoints(context.Background(), plain)
	if err != nil || !slices.Equal(endpoints, []string{"http://10.255.0.1"}) {
		t.Fatalf("plain endpoints = %v err=%v", endpoints, err)
	}
	if evidence, err := store.ManagedFQDNOwnership(context.Background(), endpoints[0], plain); err != nil || len(evidence.Groups) != 1 {
		t.Fatalf("plain ledger = %+v err=%v", evidence, err)
	}
}

func TestDeletingAProfileCanTakeItsStaticRoutesOffTheRouterButKeepsSharedOnes(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}, "discord.expiry.test": {"198.51.100.20"}})
	device := newFakeKeeneticDevice()
	foreign := "10.9.9.0/255.255.255.0"
	shared := "192.0.2.10/255.255.255.255"
	discord := "198.51.100.20/255.255.255.255"
	device.routes[foreign] = deviceInterfaceName
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()
	deps := runtimeDeps{
		Resolver: resolver, Now: func() time.Time { return now },
		DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()}, Secrets: newCleanupSecrets(),
	}
	origin, stop, done, stderr := startServeServer(t, catalog, data, deps)
	serverStopped := false
	stopServer := func() {
		if serverStopped {
			return
		}
		serverStopped = true
		stop()
		if code := <-done; code != 0 {
			t.Errorf("serve=%d stderr=%s", code, stderr.String())
		}
	}
	defer stopServer()

	profileA, outputA, _ := createProfileOutput(t, origin, "A", "keenetic", "youtube", "discord")
	profileB, outputB, _ := createProfileOutput(t, origin, "B", "keenetic", "youtube")
	connection := registerSavedConnection(t, origin, "keenetic", "Home router")
	artifactB := refreshAndBuild(t, origin, profileB, outputB).Artifact.ID
	// Both profiles reach the router through the saved connection with a
	// password typed for each send; none is saved yet.
	sendThroughSavedConnection(t, origin, refreshAndBuild(t, origin, profileA, outputA).Artifact.ID, connection, strings.Repeat("a", 32), devicePassword)
	sendThroughSavedConnection(t, origin, artifactB, connection, strings.Repeat("b", 32), devicePassword)
	if !fakeDeviceHasRoutes(device, foreign, shared, discord) {
		t.Fatalf("delivered routes = %v", device.routes)
	}
	postJSON(t, origin+"/v1/profiles/"+profileA+"/archive", `{}`)

	// The deletion dialog sees the profile's claims split by what a cleanup
	// would do with them, and that this connection has no saved password.
	objects := readDeviceObjects(t, origin+"/v1/profiles/"+profileA+"/device-objects")
	if len(objects.Objects) != 1 || objects.Cleanable {
		t.Fatalf("profile device objects = %+v", objects)
	}
	owned := objects.Objects[0]
	if owned.OutputID != outputA || owned.TargetID != "keenetic" || owned.Endpoint != "http://10.255.0.1" || owned.Interface != deviceInterfaceName ||
		owned.Routes == nil || *owned.Routes != (application.DeviceRouteCounts{Claimed: 2, Removable: 1, Shared: 1}) || owned.FQDNGroups != nil ||
		owned.Cleanable || owned.BlockedBy != "credential_missing" || owned.Connection == nil || owned.Connection.ID != connection || owned.Connection.CredentialStored {
		t.Fatalf("owned objects = %+v routes=%+v connection=%+v", owned, owned.Routes, owned.Connection)
	}

	// Asked to clean a router it cannot sign in to, the deletion refuses
	// before contacting any device.
	requests := len(device.sentBodies())
	status, reply := retire(t, origin+"/v1/profiles/"+profileA+"/delete", `{"remove_from_devices":true}`)
	if status != http.StatusConflict || reply.Error != "device_cleanup_unavailable" || reply.Retired == nil || *reply.Retired ||
		len(reply.Blocked) != 1 || reply.Blocked[0].BlockedBy != "credential_missing" || reply.Blocked[0].OutputID != outputA || len(reply.Cleanups) != 0 {
		t.Fatalf("blocked deletion status=%d reply=%+v", status, reply)
	}
	if len(device.sentBodies()) != requests {
		t.Fatal("a blocked cleanup contacted the router")
	}
	if live := httpGet(t, origin+"/v1/profiles/"+profileA, nil); live.status != http.StatusOK {
		t.Fatalf("a blocked cleanup deleted the profile: %d", live.status)
	}

	postJSON(t, origin+"/v1/devices/"+connection+"/credential", `{"credential":"`+devicePassword+`"}`)
	status, reply = retire(t, origin+"/v1/profiles/"+profileA+"/delete", `{"remove_from_devices":true}`)
	if status != http.StatusOK || reply.DeletedAt == "" || len(reply.Cleanups) != 1 || reply.Cleanups[0].OutputID != outputA ||
		reply.Cleanups[0].Status != application.DeploymentAttemptSucceeded || !reply.Cleanups[0].Result.Applied {
		t.Fatalf("deletion status=%d reply=%+v", status, reply)
	}
	// Only the route this profile alone claimed is gone. The route the other
	// profile still claims and the operator's own route stay.
	if !fakeDeviceHasRoutes(device, foreign, shared) {
		t.Fatalf("after deletion routes = %v", device.routes)
	}
	if gone := httpGet(t, origin+"/v1/profiles/"+profileA, nil); gone.status != http.StatusNotFound {
		t.Fatalf("deleted profile answered %d", gone.status)
	}
	// The other profile's next send does not bring back a route only the
	// deleted profile claimed: its claim left the ledger with the route.
	sendThroughSavedConnection(t, origin, artifactB, connection, strings.Repeat("c", 32), "")
	if !fakeDeviceHasRoutes(device, foreign, shared) {
		t.Fatalf("a later send restored a removed route: %v", device.routes)
	}

	stopServer()
	state := readManagedOwnership(t, data, application.ManagedRouteScope{Endpoint: "http://10.255.0.1", TargetID: "keenetic", Interface: deviceInterfaceName})
	if len(state.Routes) != 1 || state.Routes[0].Prefix.String() != "192.0.2.10/32" || len(state.Claims) != 1 || state.Claims[0].OutputID != outputB {
		t.Fatalf("ledger after deletion = %+v", state)
	}
}
