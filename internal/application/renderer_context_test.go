package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

type rendererContextProbe struct {
	wantCtx    context.Context
	cancel     context.CancelFunc
	stopMethod string
	stopCall   int
	calls      map[string]int
	outputID   string
	prefix     string
}

// This test renderer models request-scoped I/O while retaining OutputRenderer.
// Calls without the consumer's context fail even when a store double ignores it.
type contextualTestRenderer struct {
	mutatingRenderer
	ctx   context.Context
	probe *rendererContextProbe
}

func (r contextualTestRenderer) WithContext(ctx context.Context) Renderer {
	r.ctx = ctx
	return r
}

func (r contextualTestRenderer) check(method string) error {
	if r.ctx != r.probe.wantCtx {
		return errors.New("renderer was not bound to its caller")
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	r.probe.calls[method]++
	if method == r.probe.stopMethod && r.probe.calls[method] == r.probe.stopCall {
		r.probe.cancel()
	}
	return r.ctx.Err()
}

func (r contextualTestRenderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	return len(plan.Rules), r.check("projection")
}

func (r contextualTestRenderer) Render(domain.RoutingPlan) ([]byte, error) {
	return []byte("payload"), r.check("render")
}

func (r contextualTestRenderer) RenderOutput(_ domain.RoutingPlan, outputID, prefix string) ([]byte, error) {
	r.probe.outputID, r.probe.prefix = outputID, prefix
	return []byte("payload"), r.check("output")
}

func (r contextualTestRenderer) Validate([]byte) error { return r.check("validate") }

func TestPublicationRendererCancellationReachesEveryIOPhase(t *testing.T) {
	for _, test := range []struct {
		operation string
		method    string
		call      int
	}{
		{"build", "projection", 1},
		{"build", "projection", 2},
		{"build", "output", 1},
		{"build", "validate", 1},
		{"build", "projection", 3},
		{"export", "projection", 2},
		{"export", "render", 1},
		{"export", "validate", 1},
		{"forecast", "projection", 2},
		{"forecast", "projection", 3},
	} {
		t.Run(fmt.Sprintf("%s/%s/%d", test.operation, test.method, test.call), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probe := &rendererContextProbe{wantCtx: ctx, cancel: cancel, stopMethod: test.method, stopCall: test.call, calls: map[string]int{}}
			renderer := contextualTestRenderer{probe: probe}
			profile, output := testProfileAndOutput()
			store := &publicationFakeStore{profile: profile, output: output}
			files := &publicationFakeFiles{}
			publication := newPublicationTestService(t, store, files, renderer, bytes.NewReader(bytes.Repeat([]byte{0x61}, 256)))
			var err error
			switch test.operation {
			case "build":
				_, err = publication.Build(ctx, output.ID)
			case "export":
				_, err = publication.Export(ctx, profile.ID, output.TargetID)
			case "forecast":
				_, err = publication.ForecastComposition(ctx, ProfileComposition{Lists: profile.Lists}, []string{output.TargetID})
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("operation lost cancellation: %v (calls=%v)", err, probe.calls)
			}
			if len(store.published) != 0 {
				t.Fatal("canceled operation committed a publication")
			}
			// The final projection follows file staging, but must never commit it.
			if !(test.operation == "build" && test.method == "projection" && test.call == 3) && files.puts != 0 {
				t.Fatal("canceled renderer reached artifact storage")
			}
			if renderer.ctx != nil {
				t.Fatal("operation mutated the registry renderer")
			}
		})
	}
}

func TestRendererContextBindingPreservesOutputIdentity(t *testing.T) {
	ctx := context.Background()
	probe := &rendererContextProbe{wantCtx: ctx, calls: map[string]int{}}
	renderer := contextualTestRenderer{probe: probe}
	profile, output := testProfileAndOutput()
	output.FQDNGroupPrefix = "retained-prefix"
	store := &publicationFakeStore{profile: profile, output: output}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, renderer, bytes.NewReader(bytes.Repeat([]byte{0x61}, 256)))
	if _, err := publication.Build(ctx, output.ID); err != nil {
		t.Fatal(err)
	}
	if probe.outputID != output.ID || probe.prefix != output.FQDNGroupPrefix || probe.calls["render"] != 0 || probe.calls["output"] != 1 {
		t.Fatalf("context binding lost OutputRenderer: %#v", probe)
	}
	if len(store.published) != 1 {
		t.Fatal("successful request did not publish")
	}
}
