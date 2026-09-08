// Package plugin hosts out-of-process adapters. The wire contract itself lives
// in the public SDK, because a plugin author must be able to import it while
// this host stays internal.
package plugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	wire "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

var (
	ErrPluginRoot       = errors.New("plugin directory is not usable")
	ErrManifestInvalid  = errors.New("plugin manifest is not usable")
	ErrChecksumMismatch = errors.New("plugin executable does not match its manifest checksum")
	ErrManifestMismatch = errors.New("plugin reported a manifest that differs from the installed one")
	ErrUnavailable      = errors.New("plugin is unavailable")
	ErrTimeout          = errors.New("plugin did not answer within the deadline")
	ErrPermissionDenied = errors.New("plugin requested a permission its kind may not hold")
)

// Host bounds. A plugin runs as long as one call needs and no longer.
const (
	DefaultHandshakeTimeout = 10 * time.Second
	DefaultCallTimeout      = 30 * time.Second
	DefaultShutdownGrace    = 2 * time.Second
	// MaxPluginDirectories bounds discovery.
	MaxPluginDirectories = 64
	// MaxManifestBytes bounds one installed manifest document.
	MaxManifestBytes = 64 << 10
	// MaxExecutableBytes bounds the file the host will hash and run.
	MaxExecutableBytes = 256 << 20
)

// Options carries the injectable parts of the host.
type Options struct {
	// HostVersion is reported to the plugin at handshake.
	HostVersion string
	// HandshakeTimeout and CallTimeout bound the plugin's answers.
	HandshakeTimeout time.Duration
	CallTimeout      time.Duration
	// Logger receives the plugin's diagnostics as structured records.
	Logger *slog.Logger
}

func (o Options) withDefaults() Options {
	if o.HandshakeTimeout <= 0 {
		o.HandshakeTimeout = DefaultHandshakeTimeout
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = DefaultCallTimeout
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.HostVersion == "" {
		o.HostVersion = "dev"
	}
	return o
}

// Client is one live plugin process. Every call is bounded, and a call that ends
// the process fails that call only: the host keeps running.
type Client struct {
	installed Installed
	options   Options

	mu                sync.Mutex
	command           *exec.Cmd
	stdin             io.WriteCloser
	stdout            io.ReadCloser
	nextID            uint64
	manifest          wire.Manifest
	version           int
	closed            bool
	logsDone          chan struct{}
	snapshotDirectory string
	containment       processContainment

	// callSlot admits one exchange at a time. The protocol is one frame in, one
	// answer out on a single pair of pipes, and that pairing is only true if
	// callers take turns: two concurrent exchanges would interleave their frames
	// on the same stream and answer each other's requests. One serving process
	// is shared by the refresh scheduler and every HTTP handler, so taking turns
	// cannot be left to how the composition happens to be wired.
	//
	// It is a channel rather than a mutex so a caller queued behind a slow plugin
	// still honors its own deadline instead of blocking on an unbounded lock.
	callSlot chan struct{}
	// closedSignal releases calls queued for the protocol turn when shutdown
	// starts. A second closed check after admission closes the select race.
	closedSignal chan struct{}
}

// Start launches the plugin and completes the handshake.
//
// The child is started with an empty environment except the few variables a
// process needs to run at all, in the plugin's own directory, with no inherited
// handles beyond its three standard streams. Nothing the host holds — no
// subscription token, no device credential, no database path — is passed to it.
func Start(ctx context.Context, installed Installed, options Options) (*Client, error) {
	options = options.withDefaults()
	if ctx == nil {
		return nil, ErrUnavailable
	}
	if installed.Executable == "" || installed.Manifest.Name == "" {
		return nil, ErrUnavailable
	}
	executable, snapshotDirectory, err := stageVerifiedExecutable(installed)
	if err != nil {
		return nil, err
	}
	keepSnapshot := false
	defer func() {
		if !keepSnapshot {
			_ = os.RemoveAll(snapshotDirectory)
		}
	}()
	runner, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("%w: locate plugin runner: %v", ErrUnavailable, err)
	}
	containment, err := newProcessContainment()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	keepContainment := false
	defer func() {
		if !keepContainment {
			_ = containment.close()
		}
	}()
	// #nosec G204 -- runner is this Routevane executable. The private runner
	// receives the verified snapshot as data and does not invoke a shell.
	command := exec.Command(runner, processRunnerCommand, executable, installed.Directory)
	command.Dir = installed.Directory
	// A minimal environment is the point: a plugin cannot read a secret that was
	// never put in its environment.
	command.Env = minimalEnvironment()
	if err := containment.prepare(command); err != nil {
		return nil, fmt.Errorf("%w: prepare process containment: %v", ErrUnavailable, err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: stdin", ErrUnavailable)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: stdout", ErrUnavailable)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: stderr", ErrUnavailable)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	abort := func() {
		_ = containment.terminate(command)
		_ = command.Process.Kill()
		_ = stdin.Close()
		_ = command.Wait()
		_ = stdout.Close()
		_ = stderr.Close()
	}
	if err := containment.attach(command); err != nil {
		abort()
		return nil, fmt.Errorf("%w: install process containment: %v", ErrUnavailable, err)
	}
	if _, err := stdin.Write([]byte{processRunnerGate}); err != nil {
		abort()
		return nil, fmt.Errorf("%w: open plugin runner gate: %v", ErrUnavailable, err)
	}
	client := &Client{installed: installed, options: options, command: command, stdin: stdin, stdout: stdout, logsDone: make(chan struct{}), snapshotDirectory: snapshotDirectory, containment: containment, callSlot: make(chan struct{}, 1), closedSignal: make(chan struct{})}
	keepSnapshot = true
	keepContainment = true
	go client.drainDiagnostics(stderr)

	if err := client.handshake(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// drainDiagnostics turns the plugin's stderr into structured host records. Each
// line is bounded, so a plugin cannot flood the host's log with one write.
func (c *Client) drainDiagnostics(stderr io.ReadCloser) {
	defer close(c.logsDone)
	defer func() { _ = stderr.Close() }()
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 4096), wire.MaxLogLine)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		c.options.Logger.Info("plugin diagnostic", "plugin", c.installed.Manifest.Name, "version", c.installed.Manifest.Version, "message", line)
	}
}

func (c *Client) handshake(ctx context.Context) error {
	granted := wire.RequiredPermissions(c.installed.Manifest.Kind)
	envelope := wire.Envelope{Type: wire.MessageHandshake, Handshake: &wire.Handshake{
		Host: "routevane", HostVersion: c.options.HostVersion,
		ProtocolVersions: wire.SupportedProtocolVersions(), GrantedPermissions: granted,
	}}
	envelope.ID = c.allocate()
	answer, err := c.exchange(ctx, envelope, c.options.HandshakeTimeout)
	if err != nil {
		return err
	}
	if answer.Type != wire.MessageHandshakeAck || answer.HandshakeAck == nil {
		return fmt.Errorf("%w: expected a handshake acknowledgement", wire.ErrProtocolViolation)
	}
	chosen := answer.HandshakeAck.ProtocolVersion
	if _, err := wire.NegotiateVersion([]int{chosen}); err != nil {
		return fmt.Errorf("%w: plugin chose version %d", wire.ErrIncompatible, chosen)
	}
	reported := answer.HandshakeAck.Manifest
	if err := validateReported(reported); err != nil {
		return err
	}
	// The installed manifest is authoritative. A plugin that reports different
	// metadata at runtime is refused rather than trusted, because the operator
	// reviewed the installed copy and not this one.
	if !sameManifest(c.installed.Manifest, reported) {
		return fmt.Errorf("%w: %s", ErrManifestMismatch, c.installed.Manifest.Name)
	}
	c.mu.Lock()
	c.manifest, c.version = reported, chosen
	c.mu.Unlock()
	return nil
}

func sameManifest(installed, reported wire.Manifest) bool {
	if installed.Name != reported.Name || installed.Version != reported.Version || installed.Kind != reported.Kind {
		return false
	}
	if installed.ProtocolVersion != reported.ProtocolVersion {
		return false
	}
	if (installed.Renderer == nil) != (reported.Renderer == nil) || (installed.Source == nil) != (reported.Source == nil) {
		return false
	}
	if installed.Renderer != nil {
		left, right := *installed.Renderer, *reported.Renderer
		if left.ID != right.ID || left.FormatVersion != right.FormatVersion || left.ContentType != right.ContentType || left.FileExtension != right.FileExtension {
			return false
		}
		if strings.Join(left.SupportedRuleKinds, ",") != strings.Join(right.SupportedRuleKinds, ",") {
			return false
		}
	}
	if installed.Source != nil && (*installed.Source != *reported.Source) {
		return false
	}
	return permissionSet(installed.Permissions) == permissionSet(reported.Permissions)
}

func permissionSet(permissions []wire.Permission) string {
	values := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		values = append(values, string(permission))
	}
	slices.Sort(values)
	return strings.Join(values, ",")
}

// Manifest returns the negotiated manifest.
func (c *Client) Manifest() wire.Manifest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.manifest
}

// ProtocolVersion returns the negotiated version.
func (c *Client) ProtocolVersion() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

// Call issues one bounded request. A plugin that crashes, hangs, or answers out
// of order fails this call and nothing else.
func (c *Client) Call(ctx context.Context, envelope wire.Envelope) (wire.Result, error) {
	envelope.ID = c.allocate()
	answer, err := c.exchange(ctx, envelope, c.options.CallTimeout)
	if err != nil {
		return wire.Result{}, err
	}
	if answer.Type != wire.MessageResult || answer.Result == nil {
		return wire.Result{}, fmt.Errorf("%w: expected a result", wire.ErrProtocolViolation)
	}
	if answer.Result.Error != nil {
		return *answer.Result, answer.Result.Error
	}
	return *answer.Result, nil
}

// exchange writes one frame and reads exactly one answer under a deadline. The
// deadline is enforced by the host, so a plugin that never answers cannot hold
// the caller. Exchanges take turns: see Client.callSlot.
func (c *Client) exchange(ctx context.Context, envelope wire.Envelope, timeout time.Duration) (wire.Envelope, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return wire.Envelope{}, ErrUnavailable
	}
	// Waiting for a turn happens before the call deadline starts: the deadline
	// bounds how long the plugin may take to answer, not how long other callers
	// were ahead in the queue.
	select {
	case c.callSlot <- struct{}{}:
	case <-ctx.Done():
		return wire.Envelope{}, fmt.Errorf("%w: %s: %v", ErrTimeout, c.installed.Manifest.Name, ctx.Err())
	case <-c.closedSignal:
		return wire.Envelope{}, ErrUnavailable
	}
	c.mu.Lock()
	closed = c.closed
	c.mu.Unlock()
	if closed {
		<-c.callSlot
		return wire.Envelope{}, ErrUnavailable
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type answer struct {
		envelope wire.Envelope
		err      error
	}
	done := make(chan answer, 1)
	go func() {
		// The turn is given up when the pipe work actually ends, not when this
		// call stops waiting for it. A timed-out exchange terminates the plugin,
		// which closes the pipes and unblocks this goroutine, so the next caller
		// waits for a broken pipe rather than for a frame that will never come.
		defer func() { <-c.callSlot }()
		if err := wire.WriteFrame(c.stdin, envelope); err != nil {
			done <- answer{err: fmt.Errorf("%w: %v", ErrUnavailable, err)}
			return
		}
		received, err := wire.ReadFrame(c.stdout)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				// The plugin ended mid-call. That is this call's failure, not the
				// host's: the host reports it and stays running.
				done <- answer{err: fmt.Errorf("%w: the plugin ended during a call", ErrUnavailable)}
				return
			}
			done <- answer{err: err}
			return
		}
		if received.ID != envelope.ID {
			done <- answer{err: fmt.Errorf("%w: answer id %d does not match request %d", wire.ErrProtocolViolation, received.ID, envelope.ID)}
			return
		}
		done <- answer{envelope: received}
	}()

	select {
	case result := <-done:
		return result.envelope, result.err
	case <-callCtx.Done():
		// A hung plugin is terminated, not waited on.
		_ = c.terminate()
		return wire.Envelope{}, fmt.Errorf("%w: %s", ErrTimeout, c.installed.Manifest.Name)
	}
}

func (c *Client) allocate() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return c.nextID
}

// Close asks the plugin to stop, then makes sure it did.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.closedSignal)
	c.mu.Unlock()

	exited := make(chan struct{})
	go func() {
		_ = c.command.Wait()
		close(exited)
	}()
	shutdownCanceled := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case c.callSlot <- struct{}{}:
			defer func() { <-c.callSlot }()
		case <-shutdownCanceled:
			return
		}
		select {
		case <-shutdownCanceled:
			return
		default:
		}
		shutdown := wire.Envelope{Type: wire.MessageShutdown}
		shutdown.ID = c.allocate()
		_ = wire.WriteFrame(c.stdin, shutdown)
		_ = c.stdin.Close()
	}()
	timer := time.NewTimer(DefaultShutdownGrace)
	select {
	case <-exited:
		timer.Stop()
	case <-timer.C:
		close(shutdownCanceled)
		_ = c.stdin.Close()
		_ = c.terminate()
		<-exited
	}
	select {
	case <-shutdownCanceled:
	default:
		close(shutdownCanceled)
	}
	_ = c.stdin.Close()
	<-shutdownDone
	<-c.logsDone
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), DefaultShutdownGrace)
	defer cleanupCancel()
	if err := c.containment.waitEmpty(cleanupCtx); err != nil {
		return errors.Join(err, c.containment.close())
	}
	return errors.Join(c.containment.close(), removeSnapshot(cleanupCtx, c.snapshotDirectory))
}

func (c *Client) terminate() error {
	if c.command.Process == nil {
		return nil
	}
	if err := c.containment.terminate(c.command); err == nil {
		return nil
	}
	return c.command.Process.Kill()
}

// minimalEnvironment is what a plugin process is given. It is deliberately the
// smallest set that lets a program start on each platform; nothing Routevane
// holds is added.
func minimalEnvironment() []string {
	keep := []string{"SYSTEMROOT", "WINDIR", "PATHEXT", "TMP", "TEMP", "TMPDIR", "HOME", "LANG"}
	environment := make([]string, 0, len(keep))
	for _, name := range keep {
		if value := os.Getenv(name); value != "" {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}
