// Command crashing is a test fixture: it completes the handshake and then ends
// the process mid-call. A plugin that dies must fail its own call and nothing
// else.
package main

import (
	"os"

	plugin "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

type handler struct{ plugin.Unimplemented }

func (handler) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name: "crashing-plugin", Version: "1.0.0", ProtocolVersion: plugin.ProtocolVersion,
		Kind: plugin.KindRenderer, Permissions: []plugin.Permission{plugin.PermissionRenderPlan},
		Renderer: &plugin.RendererManifest{
			ID: "crashing", FormatVersion: "crashing-v1", ContentType: "text/plain", FileExtension: "txt",
			SupportedRuleKinds: []string{"ipv4"},
		},
	}
}

func (handler) Render([]byte) ([]byte, error) {
	// Ending the process here is the point of the fixture.
	os.Exit(3)
	return nil, nil
}

func main() {
	if err := plugin.Serve(handler{}); err != nil {
		os.Exit(1)
	}
}
