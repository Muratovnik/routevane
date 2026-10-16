package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
		if previousOrigin == "" {
			desktopBandPort(t, data, origin)
		}
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
		// A backend that stops before announcing an origin says why only in
		// its log, so the attempt keeps it for the failure message.
		logs := &syncBuffer{}
		go func() {
			exited <- runWithDeps(write, logs, args, runtimeDeps{Context: ctx, DesktopInput: input})
			_ = write.Close()
		}()
		if _, err := io.WriteString(control, token+"\n"); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(read).ReadString('\n')
		if err != nil {
			code := -1
			select {
			case code = <-exited:
			case <-ctx.Done():
			}
			t.Fatalf("attempt %d: no ready origin: %v exit=%d logs=%s", attempt, err, code, logs.String())
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
				t.Fatalf("attempt %d: desktop exit=%d logs=%s", attempt, code, logs.String())
			}
		case <-ctx.Done():
			t.Fatalf("attempt %d: recovery backend outlived parent logs=%s", attempt, logs.String())
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

func TestDesktopFirstPortRetriesDistinctBandPorts(t *testing.T) {
	data := t.TempDir()
	binds := &recordingListen{refuse: func(_ string, attempt int) bool { return attempt <= 5 }}
	// A constant source would ask for one port forever without the permutation.
	deps := runtimeDeps{Listen: binds.listen, RandomIndex: func(int) int { return 0 }}
	origin, _, stop := startDesktopBackend(t, []string{"--data-dir", data}, deps)
	port := desktopBandPort(t, data, origin)
	if code := stop(); code != 0 {
		t.Fatalf("desktop exit=%d", code)
	}
	addresses := binds.recorded()
	if len(addresses) < 6 || addresses[len(addresses)-1] != fmt.Sprintf("127.0.0.1:%d", port) {
		t.Fatalf("announced port %d is not the bound attempt: %v", port, addresses)
	}
	requireDistinctBandAttempts(t, addresses, 0)
}

func TestDesktopExhaustedBandFallsBackToAssignedPort(t *testing.T) {
	data := t.TempDir()
	binds := &recordingListen{refuse: func(address string, _ int) bool { return address != "127.0.0.1:0" }}
	origin, logs, stop := startDesktopBackend(t, []string{"--data-dir", data}, runtimeDeps{Listen: binds.listen})
	if code := stop(); code != 0 {
		t.Fatalf("desktop exit=%d", code)
	}
	addresses := binds.recorded()
	if len(addresses) != desktopPortAttempts+1 || addresses[desktopPortAttempts] != "127.0.0.1:0" {
		t.Fatalf("bounded band search did not end in the assigned port: %v", addresses)
	}
	requireDistinctBandAttempts(t, addresses[:desktopPortAttempts], 0)
	if !strings.Contains(logs.String(), "desktop_port_band_unavailable") {
		t.Fatalf("fallback has no reason code: %s", logs.String())
	}
	if saved, err := filesystem.DesktopPort(data); err != nil || origin != fmt.Sprintf("http://127.0.0.1:%d", saved) {
		t.Fatalf("announced %s, persisted %d %v", origin, saved, err)
	}
}

func TestDesktopPortRecoveryNeverRetriesTheFailedSavedPort(t *testing.T) {
	data := t.TempDir()
	// Index zero would draw the first band port first if the failed port were
	// still a candidate.
	const failed = desktopPortFirst
	if err := filesystem.SaveDesktopPort(data, failed); err != nil {
		t.Fatal(err)
	}
	failedAddress := fmt.Sprintf("127.0.0.1:%d", failed)
	binds := &recordingListen{refuse: func(address string, attempt int) bool { return address == failedAddress || attempt <= 3 }}
	deps := runtimeDeps{Listen: binds.listen, RandomIndex: func(int) int { return 0 }}
	origin, _, stop := startDesktopBackend(t, []string{"--data-dir", data, "--recover-desktop-port"}, deps)
	port := desktopBandPort(t, data, origin)
	if code := stop(); code != 0 {
		t.Fatalf("desktop exit=%d", code)
	}
	addresses := binds.recorded()
	if port == failed || len(addresses) < 4 || addresses[0] != failedAddress {
		t.Fatalf("recovery did not start from the saved port: port=%d attempts=%v", port, addresses)
	}
	requireDistinctBandAttempts(t, addresses[1:], failed)
}

func TestDesktopReusesSavedPortOutsideTheBand(t *testing.T) {
	data := t.TempDir()
	// A port saved before the band existed stays, because copied subscription
	// addresses carry it.
	const saved = 59943
	if err := filesystem.SaveDesktopPort(data, saved); err != nil {
		t.Fatal(err)
	}
	binds := &recordingListen{refuse: func(string, int) bool { return false }, assign: true}
	_, _, stop := startDesktopBackend(t, []string{"--data-dir", data}, runtimeDeps{Listen: binds.listen})
	if code := stop(); code != 0 {
		t.Fatalf("desktop exit=%d", code)
	}
	if addresses := binds.recorded(); len(addresses) != 1 || addresses[0] != fmt.Sprintf("127.0.0.1:%d", saved) {
		t.Fatalf("saved port was not reused as-is: %v", addresses)
	}
	if port, err := filesystem.DesktopPort(data); err != nil || port != saved {
		t.Fatalf("saved port changed: %d %v", port, err)
	}
}

func TestDesktopBandSearchIsBoundedDistinctAndSkipsTheFailedPort(t *testing.T) {
	sources := map[string]func(int) int{
		"first":  func(int) int { return 0 },
		"last":   func(n int) int { return n - 1 },
		"crypto": cryptoRandomIndex,
	}
	for name, source := range sources {
		for _, failed := range []int{0, desktopPortFirst, 24321, desktopPortFirst + desktopPortCount - 1, 59943} {
			binds := &recordingListen{refuse: func(string, int) bool { return true }}
			listener, err := listenDesktopBand(binds.listen, source, failed)
			if listener != nil || !errors.Is(err, errDesktopPortBandUnavailable) {
				t.Fatalf("%s/%d: refused band returned %v %v", name, failed, listener, err)
			}
			addresses := binds.recorded()
			if len(addresses) != desktopPortAttempts {
				t.Fatalf("%s/%d: %d attempts", name, failed, len(addresses))
			}
			requireDistinctBandAttempts(t, addresses, failed)
		}
	}
}

// recordingListen records every address the backend asks to bind. Refused
// attempts fail like an excluded port; the rest bind for real, so a band port
// that is genuinely busy on this machine only adds an attempt. assign binds an
// OS-assigned port instead of the requested one.
type recordingListen struct {
	mu        sync.Mutex
	addresses []string
	refuse    func(address string, attempt int) bool
	assign    bool
}

func (r *recordingListen) listen(network, address string) (net.Listener, error) {
	r.mu.Lock()
	r.addresses = append(r.addresses, address)
	refused := r.refuse(address, len(r.addresses))
	r.mu.Unlock()
	if refused {
		return nil, os.ErrPermission
	}
	if r.assign {
		return net.Listen(network, "127.0.0.1:0")
	}
	return net.Listen(network, address)
}

func (r *recordingListen) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.addresses...)
}

func requireDistinctBandAttempts(t *testing.T, addresses []string, failed int) {
	t.Helper()
	seen := make(map[int]bool, len(addresses))
	for _, address := range addresses {
		port, err := strconv.Atoi(strings.TrimPrefix(address, "127.0.0.1:"))
		if err != nil || port < desktopPortFirst || port >= desktopPortFirst+desktopPortCount || port == failed || seen[port] {
			t.Fatalf("attempt %s outside the band, repeated or the failed port %d: %v", address, failed, addresses)
		}
		seen[port] = true
	}
}

// desktopBandPort checks that the announced origin is a band port and the
// persisted one, and returns it.
func desktopBandPort(t *testing.T, data, origin string) int {
	t.Helper()
	parsed, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < desktopPortFirst || port >= desktopPortFirst+desktopPortCount {
		t.Fatalf("desktop origin %s is outside the band", origin)
	}
	if saved, err := filesystem.DesktopPort(data); err != nil || saved != port {
		t.Fatalf("announced port %d, persisted %d %v", port, saved, err)
	}
	return port
}

// startDesktopBackend runs one desktop backend on the shipped catalog and
// returns its announced origin, its logs and a function that ends the lease.
func startDesktopBackend(t *testing.T, args []string, deps runtimeDeps) (string, *syncBuffer, func() int) {
	t.Helper()
	input, control := io.Pipe()
	read, write := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = control.Close(); _ = read.Close(); _ = input.Close() })
	deps.Context, deps.DesktopInput = ctx, input
	logs := &syncBuffer{}
	exited := make(chan int, 1)
	go func() {
		exited <- runWithDeps(write, logs, append([]string{"desktop", "--catalog-dir", filepath.Join("..", "..", "catalog")}, args...), deps)
		_ = write.Close()
	}()
	if _, err := io.WriteString(control, strings.Repeat("a", 64)+"\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(read).ReadString('\n')
	if err != nil {
		t.Fatalf("no ready origin: %v logs=%s", err, logs.String())
	}
	return strings.TrimSpace(line), logs, func() int {
		_ = control.Close()
		select {
		case code := <-exited:
			return code
		case <-ctx.Done():
			t.Fatal("desktop backend outlived its parent pipe")
			return -1
		}
	}
}
