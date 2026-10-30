package main

import (
	"encoding/json"
	"io"
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
)

// This is the public send path: published output, confirmed HTTP attempt,
// durable SQLite ownership and the device's own group/route read-back.
func TestFQDNSendKeepsClaudeGroupAcrossEditsArchiveAndServerRestart(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	target, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", "keenetic-dns.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalog, "targets", "keenetic-dns.yaml"), target, 0o600); err != nil {
		t.Fatal(err)
	}
	device := newFQDNE2EDevice()
	device.groups["manual"] = fqdnE2EGroup{Description: "Operator list", Entries: []string{"manual.example"}}
	deviceServer := httptest.NewServer(device.handler())
	defer deviceServer.Close()
	data := filepath.Join(t.TempDir(), "data")
	deps := runtimeDeps{DeviceDialer: redirectDialer{target: deviceServer.Listener.Addr().String()}}
	origin, stop, done, stderr := startServeServer(t, catalog, data, deps)
	defer func() {
		if stop != nil {
			stop()
			if code := <-done; code != 0 {
				t.Errorf("serve=%d stderr=%s", code, stderr.String())
			}
		}
	}()
	created := postJSON(t, origin+"/v1/lists", `{"title":"Claude","domains":["claude.ai"]}`)
	var list struct {
		List struct{ ID string } `json:"list"`
	}
	if err := json.Unmarshal(created, &list); err != nil || list.List.ID == "" {
		t.Fatalf("create Claude=%s err=%v", created, err)
	}
	profileID, outputID, _ := createProfileOutput(t, origin, "Home AI", "keenetic-dns", list.List.ID)
	artifactID := refreshAndBuild(t, origin, profileID, outputID).Artifact.ID
	send := func(attempt string, confirm bool) {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"attempt_id": attempt, "confirm": confirm, "device": "http://10.255.0.1",
			"username": deviceUser, "password": devicePassword, "interface": deviceInterfaceName,
		})
		if err != nil {
			t.Fatal(err)
		}
		response := postGuardedBody(t, origin+"/v1/artifacts/"+artifactID+"/deploy", string(body))
		if response.status != http.StatusOK || confirm && (!strings.Contains(string(response.body), `"status":"succeeded"`) || !strings.Contains(string(response.body), `"applied":true`)) {
			t.Fatalf("send status=%d body=%s", response.status, response.body)
		}
	}
	assertBinding := func() {
		t.Helper()
		response := postGuardedBody(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":"keenetic-dns"}`)
		var bound struct {
			Output struct{ ID string } `json:"output"`
		}
		if err := json.Unmarshal(response.body, &bound); err != nil || response.status != http.StatusConflict || bound.Output.ID != outputID {
			t.Fatalf("repeat format selection changed identity: %d %s err=%v", response.status, response.body, err)
		}
	}
	groupName := ""
	assertClaude := func(want ...string) {
		t.Helper()
		groups, routes, _ := device.snapshot()
		if len(groups) != 2 || len(routes) != 1 || !reflect.DeepEqual(groups["manual"], fqdnE2EGroup{Description: "Operator list", Entries: []string{"manual.example"}}) {
			t.Fatalf("duplicate groups or foreign changes: groups=%#v routes=%v", groups, routes)
		}
		for name, group := range groups {
			if name == "manual" {
				continue
			}
			if groupName == "" {
				groupName = name
			}
			if name != groupName || group.Description != "Claude" || !reflect.DeepEqual(group.Entries, want) || routes[name] != deviceInterfaceName {
				t.Fatalf("Claude changed identity, title or contents: %s %#v route=%s", name, group, routes[name])
			}
		}
	}
	send("", false)
	if _, _, commands := device.snapshot(); commands != 0 {
		t.Fatal("preview wrote to the device")
	}
	send(strings.Repeat("1", 32), true)
	assertClaude("claude.ai")
	assertBinding()
	_, _, firstCommands := device.snapshot()
	send(strings.Repeat("2", 32), true)
	assertClaude("claude.ai")
	if _, _, commands := device.snapshot(); commands != firstCommands {
		t.Fatal("repeat send submitted mutations for an unchanged group")
	}
	postJSON(t, origin+"/v1/lists/"+list.List.ID+"/update", `{"title":"Claude","domains":["anthropic.com","claude.ai"]}`)
	artifactID = refreshAndBuild(t, origin, profileID, outputID).Artifact.ID
	send(strings.Repeat("3", 32), true)
	assertClaude("anthropic.com", "claude.ai")
	_, _, changedCommands := device.snapshot()
	postJSON(t, origin+"/v1/profiles/"+profileID+"/archive", `{}`)
	postJSON(t, origin+"/v1/profiles/"+profileID+"/restore", `{}`)
	assertBinding()
	stop()
	code := <-done
	stop = nil
	if code != 0 {
		t.Fatalf("first serve=%d stderr=%s", code, stderr.String())
	}
	origin, stop, done, stderr = startServeServer(t, catalog, data, deps)
	assertBinding()
	// A new user action after restart is not an attempt replay. It must read
	// the persisted ownership and find the already complete object in place.
	send(strings.Repeat("4", 32), true)
	assertClaude("anthropic.com", "claude.ai")
	if _, _, commands := device.snapshot(); commands != changedCommands {
		t.Fatal("archive/restore or process restart created new device effects")
	}
}

type fqdnE2EGroup struct {
	Description string
	Entries     []string
}

// The FQDN device double shares the authenticated router identity with the
// static-route E2E fixture, and implements the native group command surface.
type fqdnE2EDevice struct {
	base     *fakeKeeneticDevice
	mu       sync.Mutex
	groups   map[string]fqdnE2EGroup
	routes   map[string]string
	commands int
	// keepDeletedGroups acknowledges a group deletion without performing it,
	// so a read-back finds the group still there.
	keepDeletedGroups bool
}

func newFQDNE2EDevice() *fqdnE2EDevice {
	return &fqdnE2EDevice{base: newFakeKeeneticDevice(), groups: map[string]fqdnE2EGroup{}, routes: map[string]string{}}
}

func (d *fqdnE2EDevice) snapshot() (map[string]fqdnE2EGroup, map[string]string, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	groups := maps.Clone(d.groups)
	for name, group := range groups {
		group.Entries = slices.Clone(group.Entries)
		slices.Sort(group.Entries)
		groups[name] = group
	}
	return groups, maps.Clone(d.routes), d.commands
}

func (d *fqdnE2EDevice) handler() http.Handler {
	base := d.base.handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.base.mu.Lock()
		authenticated := d.base.authenticated
		d.base.mu.Unlock()
		if !authenticated || r.URL.Path != "/rci/object-group/fqdn" && r.URL.Path != "/rci/dns-proxy/route" && r.URL.Path != "/rci/" && r.URL.Path != "/ci/startup-config" {
			base.ServeHTTP(w, r)
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		switch r.URL.Path {
		case "/rci/object-group/fqdn":
			groups := map[string]any{}
			for name, group := range d.groups {
				entries := make([]map[string]string, 0, len(group.Entries))
				for _, value := range group.Entries {
					entries = append(entries, map[string]string{"address": value})
				}
				groups[name] = map[string]any{"include": entries, "description": group.Description}
			}
			writeDeviceJSON(w, groups)
		case "/rci/dns-proxy/route":
			routes := make([]map[string]any, 0, len(d.routes))
			for name, attached := range d.routes {
				routes = append(routes, map[string]any{"group": name, "interface": attached, "auto": true})
			}
			writeDeviceJSON(w, routes)
		case "/ci/startup-config":
			var config struct {
				Groups map[string]fqdnE2EGroup
				Routes map[string]string
			}
			if r.Method == http.MethodGet {
				config.Groups, config.Routes = d.groups, d.routes
				writeDeviceJSON(w, config)
			} else if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&config); err != nil {
				w.WriteHeader(http.StatusBadRequest)
			} else {
				d.groups, d.routes = config.Groups, config.Routes
				w.WriteHeader(http.StatusOK)
			}
		case "/rci/":
			var commands []struct{ Parse string }
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&commands); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			answers := make([]map[string]any, 0, len(commands))
			for _, command := range commands {
				d.commands++
				status := "message"
				if !d.applyCommand(command.Parse) {
					status = "error"
				}
				answers = append(answers, map[string]any{"parse": map[string]any{"status": []map[string]string{{"status": status, "code": "parsed"}}}})
			}
			writeDeviceJSON(w, answers)
		}
	})
}

func (d *fqdnE2EDevice) applyCommand(line string) bool {
	if line == "system configuration save" {
		return true
	}
	remove := strings.HasPrefix(line, "no ")
	line = strings.TrimPrefix(line, "no ")
	parts := strings.SplitN(line, " ", 5)
	if len(parts) >= 3 && parts[0] == "object-group" && parts[1] == "fqdn" {
		name := parts[2]
		group, exists := d.groups[name]
		if len(parts) == 3 {
			if remove {
				if d.routes[name] != "" {
					return false
				}
				if !d.keepDeletedGroups {
					delete(d.groups, name)
				}
			} else if !exists {
				d.groups[name] = fqdnE2EGroup{}
			}
			return true
		}
		if len(parts) != 5 || !exists {
			return false
		}
		switch parts[3] {
		case "include":
			if remove {
				group.Entries = slices.DeleteFunc(group.Entries, func(value string) bool { return value == parts[4] })
			} else if !slices.Contains(group.Entries, parts[4]) {
				group.Entries = append(group.Entries, parts[4])
			}
		case "description":
			value := parts[4]
			if remove || len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' || strings.ContainsAny(value[1:len(value)-1], "\"\\\r\n") {
				return false
			}
			group.Description = value[1 : len(value)-1]
		default:
			return false
		}
		d.groups[name] = group
		return true
	}
	fields := strings.Fields(line)
	if len(fields) >= 5 && fields[0] == "dns-proxy" && fields[1] == "route" && fields[2] == "object-group" {
		if remove && len(fields) == 5 {
			delete(d.routes, fields[3])
			return true
		}
		if _, exists := d.groups[fields[3]]; exists && len(fields) == 6 && fields[5] == "auto" {
			d.routes[fields[3]] = fields[4]
			return true
		}
	}
	return false
}
