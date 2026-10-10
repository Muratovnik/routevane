package plugin

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	wire "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

// This exercises the real client protocol using a trusted in-process peer.
// No installed plugin, process runner or operating-system containment is used.
func rendererPipePeer(t *testing.T, blockAt int32) (*Client, <-chan struct{}, chan struct{}, *atomic.Int32) {
	t.Helper()
	peerIn, hostIn := io.Pipe()
	hostOut, peerOut := io.Pipe()
	entered, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	count := &atomic.Int32{}
	manifest := rendererManifest()
	client := &Client{
		manifest: manifest, installed: Installed{Manifest: manifest},
		options: Options{CallTimeout: 3 * time.Second}.withDefaults(), command: &exec.Cmd{},
		stdin: hostIn, stdout: hostOut, callSlot: make(chan struct{}, 1), closedSignal: make(chan struct{}),
	}
	go func() {
		defer close(done)
		for {
			request, err := wire.ReadFrame(peerIn)
			if err != nil {
				return
			}
			if count.Add(1) == blockAt {
				close(entered)
				select {
				case <-release:
				case <-stopped:
					return
				}
			}
			answer := wire.Envelope{ID: request.ID, Type: wire.MessageResult,
				Result: &wire.Result{RuleCount: 1, Payload: []byte("192.0.2.1\n")}}
			if err := wire.WriteFrame(peerOut, answer); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stopped)
		_ = peerIn.Close()
		_ = hostIn.Close()
		_ = hostOut.Close()
		_ = peerOut.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("protocol peer did not finish")
		}
	})
	return client, entered, release, count
}

type rendererContextStore struct{ definition domain.ListDefinition }

func (*rendererContextStore) ApplySuccess(context.Context, application.SuccessCycle) error {
	return nil
}
func (*rendererContextStore) RecordFailure(context.Context, application.FailureCycle) error {
	return nil
}
func (s *rendererContextStore) ReadPlanningSnapshot(ctx context.Context, _ string, _ map[string]string, _ string, _ time.Time) (application.PlanningSnapshot, error) {
	raw := domain.RawJSONTargetDefinition()
	return application.PlanningSnapshot{Format: application.FormatRecord{
		FormatKey: raw.FormatKey, ListID: s.definition.ID, TargetID: raw.ID,
		RendererID: raw.RendererID, CatalogRevision: s.definition.CatalogRevision,
	}}, ctx.Err()
}

type rendererContextOutput struct{ puts atomic.Int32 }

func (o *rendererContextOutput) Put(ctx context.Context, _ domain.RendererDescriptor, _ string, _ []byte) (string, bool, error) {
	o.puts.Add(1)
	return "output", false, ctx.Err()
}

func TestRendererBuildListsCancellationStopsPluginIO(t *testing.T) {
	for _, test := range []struct {
		name    string
		blockAt int32
	}{
		{"preflight", 1},
		{"render", 2},
		{"renderer validation", 3},
		{"publication validation", 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, entered, release, count := rendererPipePeer(t, test.blockAt)
			renderer, err := NewRenderer(client)
			if err != nil {
				t.Fatal(err)
			}
			definition := domain.ListDefinition{ID: "example", CatalogRevision: "catalog-revision",
				Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
				Seeds:      []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:ip", SourceClass: domain.SourceManual}}}
			target := domain.TargetDefinition{ID: "examplecsv", FormatKey: "example-csv-v1", RendererID: "example-csv",
				Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 1024, MaxArtifactSize: 131072}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			output := &rendererContextOutput{}
			done := make(chan error, 1)
			go func() {
				_, err := application.BuildLists(ctx, []domain.ListDefinition{definition}, map[string]map[string]string{"example": {}},
					target, &rendererContextStore{definition}, renderer, output,
					application.ClockFunc(func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }))
				done <- err
			}()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("build ended before blocked renderer call: %v", err)
			case <-time.After(time.Second):
				t.Fatal("renderer call did not start")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("build lost cancellation cause: %v", err)
				}
			case <-time.After(time.Second):
				t.Error("canceled build waited for the plugin answer")
				close(release)
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("build did not finish after finite peer release")
				}
				return
			}
			if got := count.Load(); got != test.blockAt {
				t.Errorf("renderer sent more requests after cancellation: got %d, want %d", got, test.blockAt)
			}
			if got := output.puts.Load(); got != 0 {
				t.Errorf("canceled build reached output storage %d times", got)
			}
			close(release)
		})
	}
}

func TestRendererContextBindingsAreIndependent(t *testing.T) {
	client, entered, release, count := rendererPipePeer(t, 1)
	renderer, err := NewRenderer(client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	canceled := renderer.WithContext(ctx)
	live := renderer.WithContext(context.Background())
	cancel()
	if err := canceled.Validate([]byte("canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled renderer error = %v", err)
	}
	if count.Load() != 0 {
		t.Fatal("pre-canceled renderer sent a request")
	}
	done := make(chan error, 1)
	go func() { done <- live.Validate([]byte("live")) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("one binding canceled another: %v", err)
	case <-time.After(time.Second):
		t.Fatal("live renderer call did not start")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("live renderer call did not finish")
	}
	if err := renderer.Validate([]byte("registry")); err != nil {
		t.Fatalf("request binding mutated the shared renderer: %v", err)
	}
	if count.Load() != 2 {
		t.Fatalf("frame count = %d, want 2 live calls", count.Load())
	}
}

func TestClientQueuedCancellationLeavesActiveCallRunning(t *testing.T) {
	client, entered, release, count := rendererPipePeer(t, 1)
	first := make(chan error, 1)
	go func() {
		_, err := client.Call(context.Background(), wire.Envelope{Type: wire.MessageValidate, Validate: &wire.ValidateCall{Payload: []byte("first")}})
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first exchange did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := client.Call(ctx, wire.Envelope{Type: wire.MessageValidate, Validate: &wire.ValidateCall{Payload: []byte("queued")}})
	if !errors.Is(err, ErrTimeout) || count.Load() != 1 || time.Since(started) > time.Second {
		t.Errorf("queued cancellation: elapsed=%s frames=%d err=%v", time.Since(started), count.Load(), err)
	}
	close(release)
	select {
	case err := <-first:
		if err != nil {
			t.Errorf("queued cancellation broke the active call: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active exchange did not finish")
	}
}
