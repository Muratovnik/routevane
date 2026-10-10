// Command gracefulchild is a fixture whose helper outlives a normal protocol
// shutdown unless the host owns the complete process group's lifetime.
package main

import (
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	plugin "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

type handler struct{ plugin.Unimplemented }

func (handler) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name: "graceful-child-plugin", Version: "1.0.0", ProtocolVersion: plugin.ProtocolVersion,
		Kind: plugin.KindRenderer, Permissions: []plugin.Permission{plugin.PermissionRenderPlan},
		Renderer: &plugin.RendererManifest{
			ID: "graceful-child", FormatVersion: "graceful-child-v1", ContentType: "text/plain", FileExtension: "txt",
			SupportedRuleKinds: []string{"ipv4"},
		},
	}
}

func (handler) Render([]byte) ([]byte, error) {
	// Exceed the host's test deadline so it must terminate this process and its
	// helper even when a protocol shutdown cannot be answered.
	time.Sleep(30 * time.Second)
	return nil, nil
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--child" {
		runChild(os.Args[2])
		return
	}
	address, err := os.ReadFile("child-address")
	if err != nil {
		os.Exit(1)
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	// The helper intentionally inherits no protocol pipes. A leaked helper
	// must remain observable after the plugin's stdout and stderr are closed.
	child := exec.Command(executable, "--child", strings.TrimSpace(string(address)))
	if err := child.Start(); err != nil {
		os.Exit(1)
	}
	if err := plugin.Serve(handler{}); err != nil {
		os.Exit(1)
	}
}

func runChild(address string) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		os.Exit(1)
	}
	connection, err := net.DialTimeout("tcp4", address, 5*time.Second)
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = connection.Close() }()
	// A failed negative control is still bounded, and closing the test-owned
	// connection also releases this helper without a numeric-PID cleanup.
	_ = connection.SetDeadline(time.Now().Add(30 * time.Second))
	_, _ = io.Copy(connection, connection)
}
