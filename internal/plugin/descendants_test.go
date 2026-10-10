//go:build linux || darwin

package plugin

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startPluginWithObservedChild(t *testing.T, installed Installed) (*Client, net.Conn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed.Directory, "child-address"), []byte(listener.Addr().String()), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := Start(context.Background(), installed, Options{CallTimeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	child, err := listener.Accept()
	if err != nil {
		t.Fatalf("observe plugin helper: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })
	return client, child
}

func assertPluginChildEcho(t *testing.T, child net.Conn) {
	t.Helper()
	if err := child.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	const request = "still-owned"
	if _, err := io.WriteString(child, request); err != nil {
		t.Fatalf("write to live plugin helper: %v", err)
	}
	answer := make([]byte, len(request))
	if _, err := io.ReadFull(child, answer); err != nil {
		t.Fatalf("read from live plugin helper: %v", err)
	}
	if string(answer) != request {
		t.Fatalf("helper answer = %q", answer)
	}
}

func TestPluginCloseTerminatesDescendantsWithoutTerminatingAnotherPlugin(t *testing.T) {
	installed := buildPlugin(t, "./internal/plugin/testdata/gracefulchild", fixtureManifest("graceful-child-plugin", "graceful-child"))
	for _, mode := range []string{"normal exit", "call timeout"} {
		t.Run(mode, func(t *testing.T) {
			_, unrelated := startPluginWithObservedChild(t, installed)
			client, child := startPluginWithObservedChild(t, installed)
			assertPluginChildEcho(t, unrelated)
			assertPluginChildEcho(t, child)
			if mode == "call timeout" {
				renderer, err := NewRenderer(client)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := renderer.Render(testPlan(t)); !errors.Is(err, ErrTimeout) {
					t.Fatalf("render error = %v, want ErrTimeout", err)
				}
			}
			if err := client.Close(); err != nil {
				t.Fatalf("close plugin: %v", err)
			}
			if err := child.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			var answer [1]byte
			if count, err := child.Read(answer[:]); count != 0 || !errors.Is(err, io.EOF) {
				t.Fatalf("plugin helper survived Close: bytes=%d err=%v", count, err)
			}
			assertPluginChildEcho(t, unrelated)
		})
	}
}
