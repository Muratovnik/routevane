package routevaneplugin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// This package is the published contract, so its framing is tested here rather
// than only through the host: a plugin author who breaks a bound must see it
// fail in the package they import.

func TestOneFrameSurvivesARoundTrip(t *testing.T) {
	sent := Envelope{Type: MessageHandshake, ID: 7, Handshake: &Handshake{
		HostVersion: "test", ProtocolVersions: SupportedProtocolVersions(),
	}}
	var pipe bytes.Buffer
	if err := WriteFrame(&pipe, sent); err != nil {
		t.Fatal(err)
	}
	received, err := ReadFrame(&pipe)
	if err != nil {
		t.Fatal(err)
	}
	if received.Type != sent.Type || received.ID != sent.ID {
		t.Fatalf("received = %#v", received)
	}
	if received.Handshake == nil || received.Handshake.HostVersion != "test" {
		t.Fatalf("handshake = %#v", received.Handshake)
	}
	if pipe.Len() != 0 {
		t.Fatalf("%d bytes left after one frame", pipe.Len())
	}
}

func TestTwoFramesAreReadIndependently(t *testing.T) {
	var pipe bytes.Buffer
	for id := uint64(1); id <= 2; id++ {
		if err := WriteFrame(&pipe, Envelope{Type: MessageResult, ID: id, Result: &Result{}}); err != nil {
			t.Fatal(err)
		}
	}
	for id := uint64(1); id <= 2; id++ {
		frame, err := ReadFrame(&pipe)
		if err != nil {
			t.Fatal(err)
		}
		if frame.ID != id {
			t.Fatalf("frame id = %d, want %d", frame.ID, id)
		}
	}
	if _, err := ReadFrame(&pipe); !errors.Is(err, io.EOF) {
		t.Fatalf("a drained stream must report EOF, got %v", err)
	}
}

func TestAFrameLargerThanTheBoundIsRefusedBeforeItIsBuffered(t *testing.T) {
	// The header alone is enough to refuse: the body is never allocated, so a
	// hostile plugin cannot make the host reserve memory on its behalf.
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, MaxFrameBytes+1)
	if _, err := ReadFrame(bytes.NewReader(header)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v", err)
	}
	oversized := Envelope{Type: MessageResult, Result: &Result{Payload: bytes.Repeat([]byte("a"), MaxFrameBytes)}}
	if err := WriteFrame(io.Discard, oversized); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("writing past the bound must be refused, got %v", err)
	}
}

func TestMalformedFramesAreProtocolViolations(t *testing.T) {
	frame := func(payload string) []byte {
		header := make([]byte, 4)
		binary.BigEndian.PutUint32(header, uint32(len(payload)))
		return append(header, payload...)
	}
	cases := map[string][]byte{
		"empty length":     frame(""),
		"not json":         frame("{"),
		"unknown field":    frame(`{"type":"result","surprise":1}`),
		"missing type":     frame(`{"id":1}`),
		"trailing content": frame(`{"type":"result"}{"type":"result"}`),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadFrame(bytes.NewReader(payload)); !errors.Is(err, ErrProtocolViolation) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	// A truncated body is a transport failure, not a contract violation, and
	// must be reported as such so a caller can tell a dead plugin from a lying
	// one.
	truncated := frame(`{"type":"result"}`)
	if _, err := ReadFrame(bytes.NewReader(truncated[:len(truncated)-3])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
}

func TestVersionNegotiationPicksTheNewestSharedVersion(t *testing.T) {
	if chosen, err := NegotiateVersion([]int{ProtocolVersion, ProtocolVersion + 1}); err != nil || chosen != ProtocolVersion {
		t.Fatalf("chosen = %d, err = %v", chosen, err)
	}
	for _, offered := range [][]int{nil, {}, {0}, {-1}, {ProtocolVersion + 1}} {
		if _, err := NegotiateVersion(offered); !errors.Is(err, ErrIncompatible) {
			t.Fatalf("offered %v must be incompatible, got %v", offered, err)
		}
	}
}

func TestAPermissionBelongsToAKind(t *testing.T) {
	if got := RequiredPermissions(KindRenderer); len(got) != 1 || got[0] != PermissionRenderPlan {
		t.Fatalf("renderer permissions = %v", got)
	}
	// A source may reach the network; a renderer may not, so a renderer that
	// declares it is refused rather than silently trusted.
	source := RequiredPermissions(KindSource)
	if len(source) != 2 || source[0] != PermissionObserveNames || source[1] != PermissionNetwork {
		t.Fatalf("source permissions = %v", source)
	}
	if got := RequiredPermissions(Kind("deployer")); got != nil {
		t.Fatalf("an unknown kind must hold nothing, got %v", got)
	}
	if KnownPermission(Permission("read_secrets")) {
		t.Fatal("an unknown permission must not be accepted")
	}
}
