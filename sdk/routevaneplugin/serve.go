package routevaneplugin

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Handler is what a plugin author implements. A plugin implements exactly one
// kind; the methods for the other kind are never called.
//
// Every method may return an error. An error becomes a Result.Error the host can
// report, so a plugin never has to decide how to fail.
type Handler interface {
	// Manifest returns the manifest this plugin implements. It must match the
	// installed manifest.json byte-for-byte in every field the host compares, or
	// the host refuses the plugin at handshake.
	Manifest() Manifest
	// Render produces an artifact from the canonical plan document. A renderer
	// plugin implements it; a source plugin returns ErrUnsupported.
	Render(plan []byte) ([]byte, error)
	// ProjectedRuleCount reports how many entries the artifact would carry.
	ProjectedRuleCount(plan []byte) (int, error)
	// Validate decides whether bytes are a valid artifact of this format.
	Validate(payload []byte) error
	// Observe reports values for the requested names. A source plugin implements
	// it; a renderer plugin returns ErrUnsupported.
	Observe(call ObserveCall) ([]Observation, error)
}

// ErrUnsupported is what a plugin returns for a call its kind does not serve.
var ErrUnsupported = errors.New("unsupported call for this plugin kind")

// Unimplemented can be embedded so a plugin only writes the methods its kind
// needs. Every embedded method refuses rather than pretending to succeed.
type Unimplemented struct{}

func (Unimplemented) Render([]byte) ([]byte, error)          { return nil, ErrUnsupported }
func (Unimplemented) ProjectedRuleCount([]byte) (int, error) { return 0, ErrUnsupported }
func (Unimplemented) Validate([]byte) error                  { return ErrUnsupported }
func (Unimplemented) Observe(ObserveCall) ([]Observation, error) {
	return nil, ErrUnsupported
}

// Serve runs the plugin protocol on the process's standard input and output
// until the host asks it to stop or the stream ends.
//
// Diagnostics belong on standard error: the host reads them line by line and
// re-emits them as its own structured records. Anything a plugin writes to
// standard output that is not a protocol frame breaks the conversation.
func Serve(handler Handler) error {
	return ServeStreams(os.Stdin, os.Stdout, handler)
}

// ServeStreams is Serve with explicit streams, which is what makes a plugin
// testable in its own repository without spawning a process.
func ServeStreams(in io.Reader, out io.Writer, handler Handler) error {
	if handler == nil {
		return errors.New("plugin handler is nil")
	}
	negotiated := 0
	for {
		envelope, err := ReadFrame(in)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch envelope.Type {
		case MessageShutdown:
			return nil
		case MessageHandshake:
			if envelope.Handshake == nil {
				return ErrProtocolViolation
			}
			version, err := NegotiateVersion(envelope.Handshake.ProtocolVersions)
			if err != nil {
				// A plugin that cannot speak any offered version says so and
				// stops, so the host refuses it before asking for work.
				_ = WriteFrame(out, Envelope{Type: MessageResult, ID: envelope.ID, Result: &Result{
					Error: &Error{Code: "incompatible_protocol", Message: fmt.Sprintf("this plugin speaks %v", SupportedProtocolVersions())},
				}})
				return err
			}
			negotiated = version
			manifest := handler.Manifest()
			manifest.ProtocolVersion = version
			if err := WriteFrame(out, Envelope{Type: MessageHandshakeAck, ID: envelope.ID, HandshakeAck: &HandshakeAck{
				ProtocolVersion: version, Manifest: manifest,
			}}); err != nil {
				return err
			}
		default:
			if negotiated == 0 {
				// Nothing is served before a handshake: an unnegotiated call has
				// no agreed meaning.
				_ = WriteFrame(out, Envelope{Type: MessageResult, ID: envelope.ID, Result: &Result{
					Error: &Error{Code: "handshake_required", Message: "the host must handshake first"},
				}})
				return ErrProtocolViolation
			}
			result := dispatch(handler, envelope)
			if err := WriteFrame(out, Envelope{Type: MessageResult, ID: envelope.ID, Result: &result}); err != nil {
				return err
			}
		}
	}
}

func dispatch(handler Handler, envelope Envelope) Result {
	switch envelope.Type {
	case MessageRender:
		if envelope.Render == nil {
			return Result{Error: &Error{Code: "invalid_request", Message: "render call is missing its plan"}}
		}
		payload, err := handler.Render(envelope.Render.PlanJSON)
		if err != nil {
			return Result{Error: &Error{Code: "render_failed", Message: err.Error()}}
		}
		return Result{Payload: payload}
	case MessageProjectedRuleCount:
		if envelope.Render == nil {
			return Result{Error: &Error{Code: "invalid_request", Message: "projection call is missing its plan"}}
		}
		count, err := handler.ProjectedRuleCount(envelope.Render.PlanJSON)
		if err != nil {
			return Result{Error: &Error{Code: "projection_failed", Message: err.Error()}}
		}
		return Result{RuleCount: count}
	case MessageValidate:
		if envelope.Validate == nil {
			return Result{Error: &Error{Code: "invalid_request", Message: "validate call is missing its payload"}}
		}
		if err := handler.Validate(envelope.Validate.Payload); err != nil {
			return Result{Error: &Error{Code: "validation_failed", Message: err.Error()}}
		}
		return Result{}
	case MessageObserve:
		if envelope.Observe == nil {
			return Result{Error: &Error{Code: "invalid_request", Message: "observe call is missing its names"}}
		}
		observations, err := handler.Observe(*envelope.Observe)
		if err != nil {
			return Result{Error: &Error{Code: "observation_failed", Message: err.Error()}}
		}
		return Result{Observations: observations}
	default:
		return Result{Error: &Error{Code: "unknown_message", Message: string(envelope.Type)}}
	}
}
