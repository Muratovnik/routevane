// Package routevaneplugin is the public contract between Routevane and an
// out-of-process adapter. A plugin author imports this package and nothing else
// from Routevane.
//
// The transport is length-prefixed JSON over the child's standard input and
// output. There is no listener, no port, and no loopback socket, so a plugin is
// reachable only by the process that started it and nothing else on the machine
// can reach a plugin or impersonate the host.
package routevaneplugin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the version this build speaks. It changes only when the
// wire contract changes in a way an older peer cannot satisfy.
const ProtocolVersion = 1

// SupportedProtocolVersions is every version this build can speak, newest first.
// A plugin picks one; a plugin that can pick none is refused at handshake.
func SupportedProtocolVersions() []int { return []int{ProtocolVersion} }

// Wire bounds. A plugin is hostile input: it is a separate program the operator
// installed, not part of this build.
const (
	// MaxFrameBytes bounds one message in either direction.
	MaxFrameBytes = 8 << 20
	// MaxLogLine bounds one diagnostic line a plugin writes to its stderr.
	MaxLogLine = 4 << 10
)

var (
	ErrFrameTooLarge     = errors.New("plugin frame exceeds the byte bound")
	ErrProtocolViolation = errors.New("plugin violated the protocol")
	ErrIncompatible      = errors.New("plugin protocol version is not supported")
)

// Kind is what a plugin provides. The set is closed: a plugin that claims
// anything else is refused before it is asked to do work.
type Kind string

const (
	KindRenderer Kind = "renderer"
	KindSource   Kind = "source"
)

// Permission is a capability a plugin declares in its manifest. The host grants
// only what the declared kind requires, so a plugin cannot widen its own reach
// by asking.
type Permission string

const (
	// PermissionRenderPlan lets a plugin receive a routing plan projection.
	PermissionRenderPlan Permission = "render_plan"
	// PermissionObserveNames lets a plugin receive names to observe.
	PermissionObserveNames Permission = "observe_names"
	// PermissionNetwork declares that a plugin makes its own outbound requests.
	// The host cannot enforce it inside another process, so declaring it is a
	// disclosure to the operator, never a grant of anything the host holds.
	PermissionNetwork Permission = "network"
)

// KnownPermission reports whether a permission is one this build understands.
func KnownPermission(value Permission) bool {
	switch value {
	case PermissionRenderPlan, PermissionObserveNames, PermissionNetwork:
		return true
	default:
		return false
	}
}

// RequiredPermissions is the closed set a kind is allowed to hold.
func RequiredPermissions(kind Kind) []Permission {
	switch kind {
	case KindRenderer:
		return []Permission{PermissionRenderPlan}
	case KindSource:
		return []Permission{PermissionObserveNames, PermissionNetwork}
	default:
		return nil
	}
}

// MessageType identifies one frame's purpose.
type MessageType string

const (
	MessageHandshake          MessageType = "handshake"
	MessageHandshakeAck       MessageType = "handshake_ack"
	MessageRender             MessageType = "render"
	MessageValidate           MessageType = "validate"
	MessageProjectedRuleCount MessageType = "projected_rule_count"
	MessageObserve            MessageType = "observe"
	MessageResult             MessageType = "result"
	MessageShutdown           MessageType = "shutdown"
)

// Envelope is every frame. Exactly one payload field is meaningful, chosen by
// Type, and Error is set only on a result.
type Envelope struct {
	Type MessageType `json:"type"`
	// ID correlates a request with its result. A result whose id does not match
	// the outstanding request is a protocol violation, not a late answer.
	ID uint64 `json:"id"`

	Handshake    *Handshake    `json:"handshake,omitempty"`
	HandshakeAck *HandshakeAck `json:"handshake_ack,omitempty"`
	Render       *RenderCall   `json:"render,omitempty"`
	Validate     *ValidateCall `json:"validate,omitempty"`
	Observe      *ObserveCall  `json:"observe,omitempty"`
	Result       *Result       `json:"result,omitempty"`
}

// Handshake is the host's opening frame. It names the host build and every
// protocol version the host can speak.
type Handshake struct {
	Host             string `json:"host"`
	HostVersion      string `json:"host_version"`
	ProtocolVersions []int  `json:"protocol_versions"`
	// GrantedPermissions is what the host will honor for this plugin. A plugin
	// that needs more must say so in its manifest and be reinstalled, never
	// negotiated up at runtime.
	GrantedPermissions []Permission `json:"granted_permissions"`
}

// HandshakeAck is the plugin's answer: the single version it chose and the
// manifest it implements.
type HandshakeAck struct {
	ProtocolVersion int      `json:"protocol_version"`
	Manifest        Manifest `json:"manifest"`
}

// Manifest describes what a plugin provides. The installed copy on disk is
// authoritative; the copy a plugin reports at handshake must match it, so a
// plugin cannot claim more at runtime than the operator installed.
type Manifest struct {
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	ProtocolVersion int          `json:"protocol_version"`
	Kind            Kind         `json:"kind"`
	Permissions     []Permission `json:"permissions"`
	// Executable is the file to run, relative to the plugin directory.
	Executable string `json:"executable"`
	// SHA256 is the executable's digest. The host refuses to run a file whose
	// digest differs, so a replaced binary is a refusal rather than a surprise.
	SHA256 string `json:"sha256"`

	Renderer *RendererManifest `json:"renderer,omitempty"`
	Source   *SourceManifest   `json:"source,omitempty"`
}

// RendererManifest is the format metadata a renderer plugin provides. It mirrors
// the built-in descriptor so a plugin renderer is indistinguishable downstream.
type RendererManifest struct {
	ID                 string   `json:"id"`
	FormatVersion      string   `json:"format_version"`
	ContentType        string   `json:"content_type"`
	FileExtension      string   `json:"file_extension"`
	SupportedRuleKinds []string `json:"supported_rule_kinds"`
}

// SourceManifest is the observation metadata a source plugin provides.
type SourceManifest struct {
	Type     string `json:"type"`
	Revision string `json:"revision"`
}

// RenderCall carries the canonical plan JSON. It is the same bounded document
// the snapshot boundary already produces, so a plugin sees exactly what a
// built-in renderer sees and nothing more.
type RenderCall struct {
	PlanJSON json.RawMessage `json:"plan_json"`
	// Projection asks for the rule count only, without an artifact.
	Projection bool `json:"projection,omitempty"`
}

// ValidateCall carries artifact bytes for the plugin's own validator.
type ValidateCall struct {
	Payload []byte `json:"payload"`
}

// ObserveCall carries the names a source plugin should observe. It never carries
// a credential, a token, or a device address.
//
// The wire key of ListID is still service_id. This protocol is versioned
// separately and renaming its keys would break every plugin built against it,
// so it keeps the retired word until that version is raised (ADR 0039).
type ObserveCall struct {
	ListID      string   `json:"service_id"`
	ComponentID string   `json:"component_id"`
	SourceID    string   `json:"source_id"`
	Revision    string   `json:"revision"`
	Names       []string `json:"names"`
	ObservedAt  string   `json:"observed_at"`
}

// Result is the answer to one call.
type Result struct {
	// Error is a stable code and message. A plugin that fails says why; the host
	// never guesses.
	Error *Error `json:"error,omitempty"`
	// Payload is a rendered artifact.
	Payload []byte `json:"payload,omitempty"`
	// RuleCount is a projection answer.
	RuleCount int `json:"rule_count,omitempty"`
	// Observations are normalized values a source plugin reports. They are
	// re-validated by the host: a plugin cannot introduce an unchecked address.
	Observations []Observation `json:"observations,omitempty"`
}

// Observation is one value a source plugin reports. Only a domain, an address,
// or a prefix can cross this boundary, and the host parses each one itself.
type Observation struct {
	// Value is an address or a CIDR prefix in canonical text form.
	Value string `json:"value"`
	// TTLSeconds is optional. Zero means the host's source validity applies.
	TTLSeconds int64 `json:"ttl_seconds,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// WriteFrame writes one length-prefixed message.
func WriteFrame(w io.Writer, envelope Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode plugin frame: %w", err)
	}
	// The bound is what makes the header conversion exact: a payload this size
	// always fits the four-byte length prefix both peers agree on.
	size := len(payload)
	if size < 0 || size > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(size))
	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write plugin frame header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write plugin frame: %w", err)
	}
	return nil
}

// ReadFrame reads one length-prefixed message. A frame that claims more than the
// bound is refused before any of it is buffered.
func ReadFrame(r io.Reader) (Envelope, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return Envelope{}, err
	}
	length := binary.BigEndian.Uint32(header)
	if length == 0 {
		return Envelope{}, ErrProtocolViolation
	}
	if length > MaxFrameBytes {
		return Envelope{}, ErrFrameTooLarge
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Envelope{}, err
	}
	var envelope Envelope
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrProtocolViolation, err)
	}
	if decoder.More() {
		return Envelope{}, ErrProtocolViolation
	}
	if envelope.Type == "" {
		return Envelope{}, ErrProtocolViolation
	}
	return envelope, nil
}

// NegotiateVersion picks the newest version both peers can speak.
func NegotiateVersion(offered []int) (int, error) {
	supported := map[int]struct{}{}
	for _, version := range SupportedProtocolVersions() {
		supported[version] = struct{}{}
	}
	best := 0
	for _, version := range offered {
		if _, ok := supported[version]; ok && version > best {
			best = version
		}
	}
	if best == 0 {
		return 0, ErrIncompatible
	}
	return best, nil
}
