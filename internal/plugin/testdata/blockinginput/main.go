// Command blockinginput is a test fixture: it completes the handshake and then
// keeps running without reading another byte from standard input. It models a
// plugin whose input side is stuck independently of the host's shutdown path.
package main

import (
	"os"
	"os/signal"

	plugin "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

func main() {
	handshake, err := plugin.ReadFrame(os.Stdin)
	if err != nil || handshake.Type != plugin.MessageHandshake || handshake.Handshake == nil {
		os.Exit(1)
	}
	version, err := plugin.NegotiateVersion(handshake.Handshake.ProtocolVersions)
	if err != nil {
		os.Exit(1)
	}

	// Register the wait before acknowledging the handshake. Once Start returns,
	// this process has no code path that reads another protocol frame.
	terminated := make(chan os.Signal, 1)
	signal.Notify(terminated)
	defer signal.Stop(terminated)
	manifest := plugin.Manifest{
		Name: "blocking-input-plugin", Version: "1.0.0", ProtocolVersion: version,
		Kind: plugin.KindRenderer, Permissions: []plugin.Permission{plugin.PermissionRenderPlan},
		Renderer: &plugin.RendererManifest{
			ID: "blocking-input", FormatVersion: "blocking-input-v1", ContentType: "text/plain", FileExtension: "txt",
			SupportedRuleKinds: []string{"ipv4"},
		},
	}
	if err := plugin.WriteFrame(os.Stdout, plugin.Envelope{
		Type: plugin.MessageHandshakeAck,
		ID:   handshake.ID,
		HandshakeAck: &plugin.HandshakeAck{
			ProtocolVersion: version,
			Manifest:        manifest,
		},
	}); err != nil {
		os.Exit(1)
	}
	<-terminated
}
