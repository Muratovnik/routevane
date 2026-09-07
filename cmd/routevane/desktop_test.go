package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
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
	code := runWithDeps(io.Discard, io.Discard, []string{"desktop", "--catalog-dir", filepath.Join("..", "..", "catalog"), "--data-dir", data}, runtimeDeps{Context: ctx, DesktopInput: input})
	if code != 1 {
		t.Fatalf("occupied port returned %d", code)
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
