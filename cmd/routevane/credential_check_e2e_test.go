package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/deployers/keenetic/keenetictest"
)

// hostRoutingDialer sends each device address to its own double, so one server
// can reach several routers that each answer differently.
type hostRoutingDialer map[string]string

func (d hostRoutingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	target, ok := d[host]
	if !ok {
		return nil, errors.New("no device double answers this address")
	}
	return (&net.Dialer{}).DialContext(ctx, network, target)
}

// keyedTestSecrets stands in for the operating system's store, so this test
// never writes to the workstation's credential manager.
type keyedTestSecrets struct {
	mu      sync.Mutex
	secrets map[string]string
}

func (*keyedTestSecrets) Available() bool { return true }

func (s *keyedTestSecrets) PutSecret(_ context.Context, key, _, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[key] = secret
	return nil
}

func (s *keyedTestSecrets) Secret(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.secrets[key]
	if !ok {
		return "", application.ErrNotFound
	}
	return secret, nil
}

func (s *keyedTestSecrets) DeleteSecret(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, key)
	return nil
}

func (s *keyedTestSecrets) held(deviceID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets["Routevane device "+deviceID]
}

// Saving a router password signs in to the router first, read-only. A password
// the router refuses is not kept; one it accepts is kept and said to be
// verified; a missing interface or an unreachable router keeps the password and
// says what could not be checked. The routers are never changed, and the log
// names the route, the result and the code only.
func TestSavingARouterPasswordChecksItWithAReadOnlyLoginFirst(t *testing.T) {
	newer := newFakeKeeneticDevice()
	// The KeeneticOS 5.2 login (ADR 0058).
	newer.ndw4 = &keenetictest.NDW4{Login: deviceUser, Password: devicePassword, Salt: []byte("0123456789abcdef"), Iterations: 2, MemoryKiB: 8}
	newerServer := httptest.NewServer(newer.handler())
	defer newerServer.Close()
	older := newFakeKeeneticDevice()
	olderServer := httptest.NewServer(older.handler())
	defer olderServer.Close()
	closed, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddress := closed.Addr().String()

	secrets := &keyedTestSecrets{secrets: map[string]string{}}
	deps := runtimeDeps{
		DeviceDialer: hostRoutingDialer{
			"10.255.0.1": newerServer.Listener.Addr().String(),
			"10.255.0.2": olderServer.Listener.Addr().String(),
			"10.255.0.3": closedAddress,
		},
		Secrets: secrets,
	}
	origin, stop, done, stderr := startServeServer(t, writeAlphaBetaCatalog(t), filepath.Join(t.TempDir(), "data"), deps)
	defer func() {
		stop()
		if code := <-done; code != 0 {
			t.Errorf("serve=%d stderr=%s", code, stderr.String())
		}
	}()
	// The port is released only now, so the server cannot have taken it: a
	// dial to the switched-off router finds nothing listening.
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}

	register := func(name, address, interfaceName string) string {
		t.Helper()
		body, err := json.Marshal(map[string]string{"target_id": "keenetic", "name": name, "address": address, "account": deviceUser, "interface": interfaceName})
		if err != nil {
			t.Fatal(err)
		}
		var created struct {
			Device application.Device `json:"device"`
		}
		if err := json.Unmarshal(postJSON(t, origin+"/v1/devices", string(body)), &created); err != nil || created.Device.ID == "" {
			t.Fatalf("register %s: %+v err=%v", name, created, err)
		}
		return created.Device.ID
	}
	save := func(id, password string) (int, map[string]any) {
		t.Helper()
		body, err := json.Marshal(map[string]string{"credential": password})
		if err != nil {
			t.Fatal(err)
		}
		response := postGuardedBody(t, origin+"/v1/devices/"+id+"/credential", string(body))
		var decoded map[string]any
		if err := json.Unmarshal(response.body, &decoded); err != nil {
			t.Fatalf("status=%d body=%s err=%v", response.status, response.body, err)
		}
		return response.status, decoded
	}
	stored := func(reply map[string]any) bool {
		device, _ := reply["device"].(map[string]any)
		flag, _ := device["credential_stored"].(bool)
		return flag
	}
	verification := func(reply map[string]any) (string, string) {
		check, _ := reply["verification"].(map[string]any)
		result, _ := check["result"].(string)
		code, _ := check["code"].(string)
		return result, code
	}
	listed := func(id string) bool {
		t.Helper()
		response := httpGet(t, origin+"/v1/devices", nil)
		var registry struct {
			Devices []application.DeviceCard `json:"devices"`
		}
		if err := json.Unmarshal(response.body, &registry); err != nil {
			t.Fatalf("list status=%d body=%s err=%v", response.status, response.body, err)
		}
		for _, device := range registry.Devices {
			if device.ID == id {
				return device.CredentialStored
			}
		}
		t.Fatalf("device %s is not listed", id)
		return false
	}

	router := register("Home router", "http://10.255.0.1", deviceInterfaceName)
	const refusedSecret = "not-the-device-secret"

	// The router refuses the password: nothing is stored, and the refusal names
	// the field to fix.
	status, reply := save(router, refusedSecret)
	if status != http.StatusUnprocessableEntity || reply["error"] != "device_auth_rejected" || reply["field"] != "credential" {
		t.Fatalf("a refused password: status=%d reply=%v", status, reply)
	}
	if secrets.held(router) != "" || listed(router) {
		t.Fatal("a password the router refused was stored")
	}

	// The router accepts it: stored and verified.
	status, reply = save(router, devicePassword)
	if result, code := verification(reply); status != http.StatusOK || !stored(reply) || result != "verified" || code != "" {
		t.Fatalf("an accepted password: status=%d reply=%v", status, reply)
	}
	if secrets.held(router) != devicePassword || !listed(router) {
		t.Fatal("an accepted password was not stored")
	}

	// The login succeeds but the interface is not on the router: the password is
	// right, so it is kept, and the answer says what else to fix.
	wrongInterface := register("Second router", "http://10.255.0.2", "Wireguard9")
	status, reply = save(wrongInterface, devicePassword)
	if result, code := verification(reply); status != http.StatusOK || !stored(reply) || result != "interface_missing" || code != "device_interface_missing" {
		t.Fatalf("a missing interface: status=%d reply=%v", status, reply)
	}
	if secrets.held(wrongInterface) != devicePassword {
		t.Fatal("a password for a router without that interface was not stored")
	}

	// Nothing answers: the password is kept and said to be unchecked.
	unreachable := register("Switched-off router", "http://10.255.0.3", deviceInterfaceName)
	status, reply = save(unreachable, refusedSecret)
	if result, code := verification(reply); status != http.StatusOK || !stored(reply) || result != "unverified" || code != "device_unreachable" {
		t.Fatalf("an unreachable router: status=%d reply=%v", status, reply)
	}
	if secrets.held(unreachable) != refusedSecret {
		t.Fatal("a password for an unreachable router was not stored")
	}

	// Each check was a login and reads only.
	for name, device := range map[string]*fakeKeeneticDevice{"newer": newer, "older": older} {
		if routes, restores := device.snapshot(); device.applyCount() != 0 || restores != 0 || routes != 0 {
			t.Fatalf("checking a password changed the %s router: applies=%d restores=%d routes=%d", name, device.applyCount(), restores, routes)
		}
	}

	logs := stderr.String()
	for _, want := range []string{`"route":"devices.credential"`, `"code":"device_auth_rejected"`, `"result":"interface_missing"`, `"code":"device_unreachable"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("the log lacks %s: %s", want, logs)
		}
	}
	for _, secret := range []string{refusedSecret, devicePassword, "10.255.0.", "127.0.0.1:" + strings.Split(closedAddress, ":")[1], "authentication status", "ndm_session"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("the log carries %q: %s", secret, logs)
		}
	}
}
