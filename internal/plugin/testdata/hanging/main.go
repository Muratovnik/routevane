// Command hanging is a test fixture: it completes the handshake and then never
// answers. A plugin that hangs must be terminated by the host, not waited on.
package main

import (
	"os"
	"time"

	plugin "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

type handler struct{ plugin.Unimplemented }

func (handler) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name: "hanging-plugin", Version: "1.0.0", ProtocolVersion: plugin.ProtocolVersion,
		Kind: plugin.KindRenderer, Permissions: []plugin.Permission{plugin.PermissionRenderPlan},
		Renderer: &plugin.RendererManifest{
			ID: "hanging", FormatVersion: "hanging-v1", ContentType: "text/plain", FileExtension: "txt",
			SupportedRuleKinds: []string{"ipv4"},
		},
	}
}

func (handler) Render([]byte) ([]byte, error) {
	// Never answering is the point of the fixture.
	time.Sleep(10 * time.Minute)
	return nil, nil
}

func main() {
	if err := plugin.Serve(handler{}); err != nil {
		os.Exit(1)
	}
}
