package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

func TestDesktopLeaseAndPrivateAPI(t *testing.T) {
	data := t.TempDir()
	previousOrigin := ""
	for range 2 {
		input, control := io.Pipe()
		output, ready := io.Pipe()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)
		t.Cleanup(func() { _ = control.Close(); _ = output.Close() })
		done := make(chan int, 1)
		go func() {
			done <- runWithDeps(ready, io.Discard, []string{"desktop", "--catalog-dir", filepath.Join("..", "..", "catalog"), "--data-dir", data}, runtimeDeps{Context: ctx, DesktopInput: input})
			_ = ready.Close()
			_ = input.Close()
		}()
		token := strings.Repeat("a", 64)
		if _, err := io.WriteString(control, token+"\n"); err != nil {
			t.Fatal(err)
		}
		origin, err := bufio.NewReader(output).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		origin = strings.TrimSpace(origin)
		if previousOrigin != "" && origin != previousOrigin {
			t.Fatalf("subscription origin changed: %s -> %s", previousOrigin, origin)
		}
		previousOrigin = origin
		client := &http.Client{Timeout: 3 * time.Second}
		for _, credential := range []string{"", "wrong", token} {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/v1/profiles", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Routevane-Desktop", credential)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			want := http.StatusForbidden
			if credential == token {
				want = http.StatusOK
			}
			if response.StatusCode != want {
				t.Fatalf("status=%d want=%d", response.StatusCode, want)
			}
		}
		_ = control.Close()
		select {
		case code := <-done:
			if code != 0 {
				t.Fatalf("desktop exit=%d", code)
			}
		case <-ctx.Done():
			t.Fatal("desktop outlived its parent pipe")
		}
		client.CloseIdleConnections()
		cancel()
	}
}

func TestDesktopRejectsInvalidHandshake(t *testing.T) {
	for _, input := range []string{"", "secret\n", strings.Repeat("x", 64) + "\n", strings.Repeat("a", 130) + "\n"} {
		code := runWithDeps(io.Discard, io.Discard, []string{"desktop"}, runtimeDeps{DesktopInput: strings.NewReader(input)})
		if code != 2 {
			t.Fatalf("invalid handshake returned %d", code)
		}
	}
}

func TestDesktopOccupiedSavedPortDoesNotChangeSubscriptionsOrStealListener(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	data := t.TempDir()
	if err := filesystem.SaveDesktopPort(data, port); err != nil {
		t.Fatal(err)
	}
	input, control := io.Pipe()
	defer input.Close()
	defer control.Close()
	go func() { _, _ = io.WriteString(control, strings.Repeat("a", 64)+"\n") }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	code := runWithDeps(&stdout, io.Discard, []string{"desktop", "--catalog-dir", filepath.Join("..", "..", "catalog"), "--data-dir", data}, runtimeDeps{Context: ctx, DesktopInput: input})
	if code != 1 {
		t.Fatalf("occupied port returned %d", code)
	}
	if stdout.String() != "routevane:desktop-port-unavailable\n" {
		t.Fatalf("unclassified saved-port failure: %q", stdout.String())
	}
	if saved, err := filesystem.DesktopPort(data); saved != port || err != nil {
		t.Fatalf("saved endpoint changed: %d %v", saved, err)
	}
	connection, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal("independent listener no longer reachable:", err)
	}
	_ = connection.Close()
}

func TestDesktopExplicitPortRecoveryPreservesSubscriptionAndNextOrigin(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	data := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	origin, cancel, done, _ := startAlphaBetaServeServer(t, catalog, data, now)
	created := postJSON(t, origin+"/v1/profiles", `{"name":"Recovery","lists":["alpha"]}`)
	var profile struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(created, &profile); err != nil {
		t.Fatal(err)
	}
	created = postJSON(t, origin+"/v1/profiles/"+profile.Profile.ID+"/outputs", `{"target_id":"keenetic"}`)
	var output struct {
		Output struct {
			ID string `json:"id"`
		} `json:"output"`
	}
	if err := json.Unmarshal(created, &output); err != nil {
		t.Fatal(err)
	}
	postJSON(t, origin+"/v1/profiles/"+profile.Profile.ID+"/refresh", `{}`)
	built := postJSON(t, origin+"/v1/outputs/"+output.Output.ID+"/build", `{}`)
	var subscription struct {
		URL string `json:"subscription_url"`
	}
	if err := json.Unmarshal(built, &subscription); err != nil {
		t.Fatal(err)
	}
	before := httpGet(t, subscription.URL, nil)
	if before.status != 200 {
		t.Fatalf("fixture subscription status=%d", before.status)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("fixture exit=%d", code)
	}
	foreign, err := net.Listen("tcp4", strings.TrimPrefix(origin, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	oldPort := foreign.Addr().(*net.TCPAddr).Port
	if err := filesystem.SaveDesktopPort(data, oldPort); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	previous := ""
	for attempt := range 3 {
		input, control := io.Pipe()
		read, write := io.Pipe()
		ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(stop)
		t.Cleanup(func() { _ = control.Close(); _ = read.Close(); _ = input.Close() })
		exited := make(chan int, 1)
		args := []string{"desktop", "--catalog-dir", catalog, "--data-dir", data}
		if attempt < 2 {
			args = append(args, "--recover-desktop-port")
		}
		go func() {
			exited <- runWithDeps(write, io.Discard, args, runtimeDeps{Context: ctx, DesktopInput: input})
			_ = write.Close()
		}()
		if _, err := io.WriteString(control, token+"\n"); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(read).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		current := strings.TrimSpace(line)
		if current == origin || (previous != "" && current != previous) {
			t.Fatalf("recovered endpoint=%s old=%s previous=%s", current, origin, previous)
		}
		previous = current
		savedPort, err := filesystem.DesktopPort(data)
		if err != nil || current != fmt.Sprintf("http://127.0.0.1:%d", savedPort) {
			t.Fatalf("ready origin precedes persisted port: %s %d %v", current, savedPort, err)
		}
		after := httpGet(t, current+strings.TrimPrefix(subscription.URL, origin), nil)
		if after.status != 200 || after.etag != before.etag || !bytes.Equal(after.body, before.body) {
			t.Fatalf("subscription changed during recovery: %d %s", after.status, after.body)
		}
		response := httpGet(t, current+"/health", nil)
		if response.status != http.StatusForbidden {
			t.Fatalf("private API exposed: %d", response.status)
		}
		connection, err := net.DialTimeout("tcp4", foreign.Addr().String(), time.Second)
		if err != nil {
			t.Fatal("foreign listener stopped:", err)
		}
		_ = connection.Close()
		_ = control.Close()
		select {
		case code := <-exited:
			if code != 0 {
				t.Fatalf("desktop exit=%d", code)
			}
		case <-ctx.Done():
			t.Fatal("recovery backend outlived parent")
		}
		stop()
	}
}

func TestDesktopFailedPortRecoveryKeepsOldStateAndClosesListener(t *testing.T) {
	data := t.TempDir()
	const oldPort = 43210
	if err := filesystem.SaveDesktopPort(data, oldPort); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "desktop.port")
	t.Cleanup(func() { _ = os.Chmod(data, 0o700); _ = os.Chmod(path, 0o600) })
	var candidate net.Listener
	input, control := io.Pipe()
	defer input.Close()
	defer control.Close()
	go func() { _, _ = io.WriteString(control, strings.Repeat("a", 64)+"\n") }()
	deps := runtimeDeps{DesktopInput: input, Listen: func(network, address string) (net.Listener, error) {
		if address != "127.0.0.1:0" {
			return nil, os.ErrPermission
		}
		var err error
		candidate, err = net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		if runtime.GOOS == "windows" {
			err = os.Chmod(path, 0o400)
		} else {
			err = os.Chmod(data, 0o500)
		}
		if err != nil {
			_ = candidate.Close()
			return nil, err
		}
		return candidate, nil
	}}
	var stdout bytes.Buffer
	code := runWithDeps(&stdout, io.Discard, []string{"desktop", "--recover-desktop-port", "--catalog-dir", filepath.Join("..", "..", "catalog"), "--data-dir", data}, deps)
	if code != 1 || stdout.Len() != 0 || candidate == nil {
		t.Fatalf("failed commit announced readiness: code=%d stdout=%q", code, stdout.String())
	}
	if saved, err := filesystem.DesktopPort(data); err != nil || saved != oldPort {
		t.Fatalf("failed commit replaced old state: %d %v", saved, err)
	}
	if connection, err := net.DialTimeout("tcp4", candidate.Addr().String(), time.Second); err == nil {
		_ = connection.Close()
		t.Fatal("candidate listener leaked")
	}
}

func TestDesktopPortRecoveryKeepsOldStateWhenCompositionFails(t *testing.T) {
	data := t.TempDir()
	const oldPort = 43210
	if err := filesystem.SaveDesktopPort(data, oldPort); err != nil {
		t.Fatal(err)
	}
	var candidate net.Listener
	input, control := io.Pipe()
	defer input.Close()
	defer control.Close()
	go func() { _, _ = io.WriteString(control, strings.Repeat("a", 64)+"\n") }()
	deps := runtimeDeps{DesktopInput: input, Listen: func(network, address string) (net.Listener, error) {
		if address != "127.0.0.1:0" {
			return nil, os.ErrPermission
		}
		var err error
		candidate, err = net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		return zonedListener{candidate}, nil
	}}
	var stdout, logs bytes.Buffer
	code := runWithDeps(&stdout, &logs, []string{"desktop", "--recover-desktop-port", "--catalog-dir", filepath.Join("..", "..", "catalog"), "--data-dir", data}, deps)
	if code != 1 || stdout.Len() != 0 || candidate == nil || !strings.Contains(logs.String(), "composition_invalid") {
		t.Fatalf("failed composition announced readiness: code=%d stdout=%q logs=%s", code, stdout.String(), logs.String())
	}
	if saved, err := filesystem.DesktopPort(data); err != nil || saved != oldPort {
		t.Fatalf("failed composition replaced old state: %d %v", saved, err)
	}
	if connection, err := net.DialTimeout("tcp4", candidate.Addr().String(), time.Second); err == nil {
		_ = connection.Close()
		t.Fatal("candidate listener leaked")
	}
}

// zonedListener passes the loopback check but cannot form an HTTP origin, so
// server composition fails only after the candidate port is bound.
type zonedListener struct{ net.Listener }

func (l zonedListener) Addr() net.Addr {
	address := *l.Listener.Addr().(*net.TCPAddr)
	address.Zone = "zone"
	return &address
}
