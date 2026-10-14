package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
)

type publicationCommitBarrier struct {
	calls   atomic.Int32
	reached chan struct{}
	release chan struct{}
}

func newPublicationCommitBarrier() *publicationCommitBarrier {
	return &publicationCommitBarrier{reached: make(chan struct{}), release: make(chan struct{})}
}

func (b *publicationCommitBarrier) beforeCommit(string) error {
	if b.calls.Add(1) == 1 {
		close(b.reached)
		<-b.release
	}
	return nil
}

type publicationTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *publicationTestClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *publicationTestClock) set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type unusedPublicationSource struct{}

func (unusedPublicationSource) Observe(context.Context, application.SourceRequest) (application.SourceResult, error) {
	return application.SourceResult{}, nil
}

type publicationConsistencyFixture struct {
	service *application.PublicationService
	store   *sqlite.Store
	clock   *publicationTestClock
	barrier *publicationCommitBarrier
	profile application.Profile
	output  application.Output
}

func newPublicationConsistencyFixture(t *testing.T) publicationConsistencyFixture {
	t.Helper()
	return newPublicationConsistencyFixtureForTarget(t, domain.RawJSONTargetDefinition(), rawjson.Renderer{})
}

func newPrefixPublicationFixture(t *testing.T) publicationConsistencyFixture {
	t.Helper()
	target := domain.TargetDefinition{
		ID: "keenetic-dns", FormatKey: keeneticdns.Version, RendererID: keeneticdns.ID,
		Constraints: domain.TargetConstraints{
			SupportsDomainSuffix: true, SupportsDynamicDNSSet: true,
			SupportsIPv4: true, SupportsIPv6: true, SupportsPrefixes: true,
			MaxRules: 38400, MaxArtifactSize: keeneticdns.MaxArtifactSize,
			MaxEntriesPerList: 300, MaxLists: 128,
		},
	}
	return newPublicationConsistencyFixtureForTarget(t, target, keeneticdns.Renderer{})
}

func newPublicationConsistencyFixtureForTarget(t *testing.T, target domain.TargetDefinition, renderer application.Renderer) publicationConsistencyFixture {
	t.Helper()
	dataRoot, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(context.Background(), dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	barrier := newPublicationCommitBarrier()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	clock := &publicationTestClock{now: now}
	revision := strings.Repeat("c", 64)
	component := []domain.ComponentDefinition{{ID: "web", Required: true}}
	definitions := map[string]domain.ListDefinition{
		"alpha": {
			ID: "alpha", CatalogRevision: revision, Components: component,
			Seeds: []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:alpha", SourceClass: domain.SourceManual}},
		},
		"beta": {
			ID: "beta", CatalogRevision: revision, Components: component,
			Seeds: []domain.Seed{{Kind: domain.RuleIPv4, Value: "198.51.100.2", ComponentID: "web", SourceID: "manual:beta", SourceClass: domain.SourceManual}},
		},
	}
	service, err := application.NewPublicationService(application.PublicationConfig{
		Definitions: definitions, Targets: map[string]domain.TargetDefinition{target.ID: target},
		Categories: map[string]domain.CategoryDefinition{"group": {ID: "group", Title: "Group", Lists: []string{"alpha"}}},
		Store:      store,
		Files:      filesystem.PublishedStore{DataRoot: dataRoot, Writer: filesystem.PublishedWriter{BeforeCommit: barrier.beforeCommit}},
		Renderers:  application.RendererRegistry{renderer.ID(): renderer},
		Sources:    application.SourceRegistry{domain.SourceDNS: unusedPublicationSource{}},
		Clock:      application.ClockFunc(clock.read),
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.CreateProfile(context.Background(), "concurrent", application.ProfileComposition{Lists: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.AddOutput(context.Background(), profile.ID, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}
	return publicationConsistencyFixture{service: service, store: store, clock: clock, barrier: barrier, profile: profile, output: created.Output}
}

func (f publicationConsistencyFixture) startBlockedBuild() <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := f.service.Build(context.Background(), f.output.ID)
		done <- err
	}()
	return done
}

func waitPublicationBarrier(t *testing.T, barrier <-chan struct{}) {
	t.Helper()
	select {
	case <-barrier:
	case <-time.After(5 * time.Second):
		t.Fatal("build did not reach the publication barrier")
	}
}

func waitPublicationBuild(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("build did not finish after releasing the publication barrier")
		return nil
	}
}

func TestOutputPrefixChangeInvalidatesOnlyItsPendingPublication(t *testing.T) {
	for _, sameOutput := range []bool{true, false} {
		name := "other_output"
		if sameOutput {
			name = "same_output"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newPrefixPublicationFixture(t)
			if _, err := fixture.service.SetOutputFQDNPrefix(ctx, fixture.output.ID, "before"); err != nil {
				t.Fatal(err)
			}
			changedOutputID := fixture.output.ID
			if !sameOutput {
				profile, err := fixture.service.CreateProfile(ctx, "other", application.ProfileComposition{Lists: []string{"alpha"}})
				if err != nil {
					t.Fatal(err)
				}
				created, err := fixture.service.AddOutput(ctx, profile.ID, "keenetic-dns")
				if err != nil {
					t.Fatal(err)
				}
				changedOutputID = created.Output.ID
			}
			pending := fixture.startBlockedBuild()
			released := false
			defer func() {
				if !released {
					close(fixture.barrier.release)
				}
			}()
			waitPublicationBarrier(t, fixture.barrier.reached)
			if _, err := fixture.service.SetOutputFQDNPrefix(ctx, changedOutputID, "after"); err != nil {
				t.Fatal(err)
			}
			close(fixture.barrier.release)
			released = true
			err := waitPublicationBuild(t, pending)
			wantPrefix, wantRows := "before-", 1
			if sameOutput {
				if !errors.Is(err, application.ErrProfileChanged) {
					t.Fatalf("old-prefix publication error=%v, want ErrProfileChanged", err)
				}
				wantPrefix, wantRows = "after-", 0
			} else if err != nil {
				t.Fatalf("a different output's prefix invalidated this publication: %v", err)
			}
			snapshots, artifacts, err := fixture.store.PublicationCounts(ctx)
			if err != nil || snapshots != wantRows || artifacts != wantRows {
				t.Fatalf("candidate rows: snapshots=%d artifacts=%d want=%d err=%v", snapshots, artifacts, wantRows, err)
			}
			output, err := fixture.store.Output(ctx, fixture.output.ID)
			if err != nil {
				t.Fatal(err)
			}
			if sameOutput {
				if output.LatestArtifactID != "" || output.PreviousArtifactID != "" {
					t.Fatalf("stale prefix advanced publication pointers: %#v", output)
				}
				built, err := fixture.service.Build(ctx, fixture.output.ID)
				if err != nil {
					t.Fatal(err)
				}
				output = built.Output
			}
			artifact, err := fixture.service.Artifact(ctx, output.LatestArtifactID)
			if err != nil {
				t.Fatal(err)
			}
			groups, err := keeneticdns.Parse(artifact.Payload)
			if err != nil || len(groups) != 1 || !strings.HasPrefix(groups[0].Name, wantPrefix) {
				t.Fatalf("published prefix groups=%#v want=%q err=%v", groups, wantPrefix, err)
			}
		})
	}
}

func TestArchivedOutputRefusesOperatorEditsAndSubscriptionIssuance(t *testing.T) {
	for _, existingSubscription := range []bool{false, true} {
		name := "new subscription"
		if existingSubscription {
			name = "existing subscription remains readable"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newPrefixPublicationFixture(t)
			fixture.barrier.calls.Store(1)
			if _, err := fixture.service.SetOutputFQDNPrefix(ctx, fixture.output.ID, "before"); err != nil {
				t.Fatal(err)
			}
			device := application.Device{
				ID: strings.Repeat("d", 32), TargetID: "keenetic-dns", Name: "Fixture",
				Address: "http://10.255.0.1", Account: "admin", Interface: "Wireguard0",
				CreatedAt: fixture.clock.read(), UpdatedAt: fixture.clock.read(),
			}
			if err := fixture.store.CreateDevice(ctx, device); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.service.SetOutputDevice(ctx, fixture.output.ID, &device); err != nil {
				t.Fatal(err)
			}
			built, err := fixture.service.Build(ctx, fixture.output.ID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := fixture.service.Artifact(ctx, built.Artifact.ID)
			if err != nil {
				t.Fatal(err)
			}
			var token string
			if existingSubscription {
				token, err = fixture.service.IssueSubscription(ctx, fixture.output.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := fixture.service.ArchiveProfile(ctx, fixture.profile.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.service.SetOutputFQDNPrefix(ctx, fixture.output.ID, "after"); !errors.Is(err, application.ErrProfileArchived) {
				t.Fatalf("archived prefix error=%v", err)
			}
			if _, err := fixture.service.SetOutputDevice(ctx, fixture.output.ID, nil); !errors.Is(err, application.ErrProfileArchived) {
				t.Fatalf("archived binding error=%v", err)
			}
			if _, err := fixture.service.IssueSubscription(ctx, fixture.output.ID); !errors.Is(err, application.ErrProfileArchived) {
				t.Fatalf("archived subscription error=%v", err)
			}
			stored, err := fixture.store.Output(ctx, fixture.output.ID)
			if err != nil || stored.FQDNGroupPrefix != "before" || stored.DeviceID != device.ID || stored.LatestArtifactID != built.Artifact.ID {
				t.Fatalf("archived operator state changed: %#v err=%v", stored, err)
			}
			after, err := fixture.service.Artifact(ctx, built.Artifact.ID)
			if err != nil || !bytes.Equal(after.Payload, before.Payload) {
				t.Fatalf("archival changed readable artifact bytes: %v", err)
			}
			if existingSubscription {
				payload, err := fixture.service.Subscription(ctx, token)
				if err != nil || !bytes.Equal(payload.Payload, before.Payload) {
					t.Fatalf("archival changed an existing subscription: %v", err)
				}
			}
			if _, err := fixture.service.RestoreProfile(ctx, fixture.profile.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.service.SetOutputFQDNPrefix(ctx, fixture.output.ID, "after"); err != nil {
				t.Fatalf("restored prefix remained frozen: %v", err)
			}
			if _, err := fixture.service.SetOutputDevice(ctx, fixture.output.ID, nil); err != nil {
				t.Fatalf("restored binding remained frozen: %v", err)
			}
			if !existingSubscription {
				token, err = fixture.service.IssueSubscription(ctx, fixture.output.ID)
				if err != nil {
					t.Fatalf("restored subscription issuance failed: %v", err)
				}
			}
			payload, err := fixture.service.Subscription(ctx, token)
			if err != nil || !bytes.Equal(payload.Payload, before.Payload) {
				t.Fatalf("restoring changed immutable published bytes: %v", err)
			}
		})
	}
}

func TestPublicationCommitUsesCurrentProfileState(t *testing.T) {
	t.Run("archive completes before pending build", func(t *testing.T) {
		fixture := newPublicationConsistencyFixture(t)
		pending := fixture.startBlockedBuild()
		waitPublicationBarrier(t, fixture.barrier.reached)
		released := false
		defer func() {
			if !released {
				close(fixture.barrier.release)
			}
		}()

		fixture.clock.set(fixture.clock.read().Add(time.Minute))
		if _, err := fixture.service.ArchiveProfile(context.Background(), fixture.profile.ID); err != nil {
			t.Fatal(err)
		}
		close(fixture.barrier.release)
		released = true
		if err := waitPublicationBuild(t, pending); !errors.Is(err, application.ErrProfileArchived) {
			t.Fatalf("pending build error = %v, want ErrProfileArchived", err)
		}
		output, err := fixture.store.Output(context.Background(), fixture.output.ID)
		if err != nil {
			t.Fatal(err)
		}
		if output.LatestArtifactID != "" || output.PreviousArtifactID != "" {
			t.Fatalf("archived profile advanced publication pointers: %#v", output)
		}
		snapshots, artifacts, err := fixture.store.PublicationCounts(context.Background())
		if err != nil || snapshots != 0 || artifacts != 0 {
			t.Fatalf("archived candidate persisted rows: snapshots=%d artifacts=%d err=%v", snapshots, artifacts, err)
		}
	})

	t.Run("new composition stays current when older build finishes last", func(t *testing.T) {
		fixture := newPublicationConsistencyFixture(t)
		older := fixture.startBlockedBuild()
		waitPublicationBarrier(t, fixture.barrier.reached)
		released := false
		defer func() {
			if !released {
				close(fixture.barrier.release)
			}
		}()

		fixture.clock.set(fixture.clock.read().Add(time.Minute))
		updated, err := fixture.service.UpdateProfile(context.Background(), fixture.profile.ID, "concurrent", application.ProfileComposition{Lists: []string{"beta"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.Refresh(context.Background(), updated.ID); err != nil {
			t.Fatal(err)
		}
		fixture.clock.set(fixture.clock.read().Add(time.Minute))
		newer, err := fixture.service.Build(context.Background(), fixture.output.ID)
		if err != nil {
			t.Fatal(err)
		}
		close(fixture.barrier.release)
		released = true
		if err := waitPublicationBuild(t, older); !errors.Is(err, application.ErrProfileChanged) {
			t.Fatalf("older build error = %v, want ErrProfileChanged", err)
		}
		attempt, err := fixture.store.LatestOutputAttempt(context.Background(), fixture.output.ID)
		if err != nil || attempt.Code != application.BuildFailureProfileChanged {
			t.Fatalf("stale build attempt = %#v, err=%v", attempt, err)
		}
		output, err := fixture.store.Output(context.Background(), fixture.output.ID)
		if err != nil {
			t.Fatal(err)
		}
		if output.LatestArtifactID != newer.Artifact.ID || output.PreviousArtifactID != "" {
			t.Fatalf("older build displaced newer publication: output=%#v newer=%s", output, newer.Artifact.ID)
		}
		snapshots, artifacts, err := fixture.store.PublicationCounts(context.Background())
		if err != nil || snapshots != 1 || artifacts != 1 {
			t.Fatalf("stale candidate persisted rows: snapshots=%d artifacts=%d err=%v", snapshots, artifacts, err)
		}
	})

	t.Run("category membership change invalidates pending build", func(t *testing.T) {
		fixture := newPublicationConsistencyFixture(t)
		fixture.clock.set(fixture.clock.read().Add(time.Minute))
		profile, err := fixture.service.UpdateProfile(context.Background(), fixture.profile.ID, "concurrent", application.ProfileComposition{Categories: []string{"group"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.Refresh(context.Background(), profile.ID); err != nil {
			t.Fatal(err)
		}
		pending := fixture.startBlockedBuild()
		waitPublicationBarrier(t, fixture.barrier.reached)
		released := false
		defer func() {
			if !released {
				close(fixture.barrier.release)
			}
		}()

		lists := []string{"beta"}
		if _, err := fixture.service.UpdateCategory(context.Background(), "group", application.CategoryUpdate{Lists: &lists}); err != nil {
			t.Fatal(err)
		}
		close(fixture.barrier.release)
		released = true
		if err := waitPublicationBuild(t, pending); !errors.Is(err, application.ErrProfileChanged) {
			t.Fatalf("pending category build error = %v, want ErrProfileChanged", err)
		}
		output, err := fixture.store.Output(context.Background(), fixture.output.ID)
		if err != nil || output.LatestArtifactID != "" {
			t.Fatalf("changed category became current: output=%#v err=%v", output, err)
		}
	})

	t.Run("custom list domain change invalidates pending build", func(t *testing.T) {
		fixture := newPublicationConsistencyFixture(t)
		fixture.clock.set(fixture.clock.read().Add(time.Minute))
		custom, err := fixture.service.CreateCustomList(context.Background(), "custom", []string{"one.example"})
		if err != nil {
			t.Fatal(err)
		}
		profile, err := fixture.service.UpdateProfile(context.Background(), fixture.profile.ID, "concurrent", application.ProfileComposition{Lists: []string{custom.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.Refresh(context.Background(), profile.ID); err != nil {
			t.Fatal(err)
		}
		pending := fixture.startBlockedBuild()
		waitPublicationBarrier(t, fixture.barrier.reached)
		released := false
		defer func() {
			if !released {
				close(fixture.barrier.release)
			}
		}()

		if _, err := fixture.service.UpdateCustomList(context.Background(), custom.ID, "custom", []string{"two.example"}); err != nil {
			t.Fatal(err)
		}
		close(fixture.barrier.release)
		released = true
		if err := waitPublicationBuild(t, pending); !errors.Is(err, application.ErrProfileChanged) {
			t.Fatalf("pending custom-list build error = %v, want ErrProfileChanged", err)
		}
		output, err := fixture.store.Output(context.Background(), fixture.output.ID)
		if err != nil || output.LatestArtifactID != "" {
			t.Fatalf("changed custom list became current: output=%#v err=%v", output, err)
		}
	})

	t.Run("observation refresh does not invalidate pending build", func(t *testing.T) {
		fixture := newPublicationConsistencyFixture(t)
		pending := fixture.startBlockedBuild()
		waitPublicationBarrier(t, fixture.barrier.reached)
		released := false
		defer func() {
			if !released {
				close(fixture.barrier.release)
			}
		}()

		fixture.clock.set(fixture.clock.read().Add(time.Minute))
		if _, err := fixture.service.Refresh(context.Background(), fixture.profile.ID); err != nil {
			t.Fatal(err)
		}
		close(fixture.barrier.release)
		released = true
		if err := waitPublicationBuild(t, pending); err != nil {
			t.Fatalf("refresh invalidated cutoff-consistent build: %v", err)
		}
		output, err := fixture.store.Output(context.Background(), fixture.output.ID)
		if err != nil || output.LatestArtifactID == "" {
			t.Fatalf("valid pending build did not become current: output=%#v err=%v", output, err)
		}
	})

	t.Run("older cutoff cannot replace newer completed build", func(t *testing.T) {
		fixture := newPublicationConsistencyFixture(t)
		older := fixture.startBlockedBuild()
		waitPublicationBarrier(t, fixture.barrier.reached)
		released := false
		defer func() {
			if !released {
				close(fixture.barrier.release)
			}
		}()

		fixture.clock.set(fixture.clock.read().Add(time.Minute))
		if _, err := fixture.service.Refresh(context.Background(), fixture.profile.ID); err != nil {
			t.Fatal(err)
		}
		fixture.clock.set(fixture.clock.read().Add(time.Minute))
		newer, err := fixture.service.Build(context.Background(), fixture.output.ID)
		if err != nil {
			t.Fatal(err)
		}
		close(fixture.barrier.release)
		released = true
		if err := waitPublicationBuild(t, older); !errors.Is(err, application.ErrBuildSuperseded) {
			t.Fatalf("older cutoff build error = %v, want ErrBuildSuperseded", err)
		}
		attempt, err := fixture.store.LatestOutputAttempt(context.Background(), fixture.output.ID)
		if err != nil || attempt.Code != application.BuildFailureSuperseded {
			t.Fatalf("superseded build attempt = %#v, err=%v", attempt, err)
		}
		output, err := fixture.store.Output(context.Background(), fixture.output.ID)
		if err != nil || output.LatestArtifactID != newer.Artifact.ID {
			t.Fatalf("older cutoff displaced newer build: output=%#v newer=%s err=%v", output, newer.Artifact.ID, err)
		}
	})
}
