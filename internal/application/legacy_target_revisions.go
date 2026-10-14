package application

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/Muratovnik/routevane/internal/domain"
)

// TargetFormat is what an output created before ADR 0054 must still match to
// be moved to its target's own revision: the format and renderer it was
// created with, and the revision of the target's artifact fields today.
type TargetFormat struct {
	TargetID        string
	FormatKey       string
	RendererID      string
	RendererVersion string
	Revision        string
}

// LegacyTargetRevisionStore holds the outputs that still carry the retired
// catalog-wide target revision. RebaseLegacyTargetRevisions moves every such
// live output whose target, format key, renderer and renderer version equal
// one of the given formats to that format's revision, in one transaction, and
// reports how many it moved. An output that matches none keeps its old
// revision, so a build of it still reports ErrTargetChanged.
type LegacyTargetRevisionStore interface {
	RebaseLegacyTargetRevisions(context.Context, []TargetFormat) (int, error)
}

// RebaseLegacyTargetRevisions brings the outputs created under the catalog-wide
// target revision to the per-target revision of ADR 0054. It runs once the
// process knows its catalog and renderers, which SQL cannot, and before any
// build: the schema allows the change only for outputs that existed when the
// schema was upgraded, so it is safe to run on every start.
func RebaseLegacyTargetRevisions(ctx context.Context, store LegacyTargetRevisionStore, targets map[string]domain.TargetDefinition, renderers RendererRegistry) (int, error) {
	formats := make([]TargetFormat, 0, len(targets))
	for _, id := range slices.Sorted(maps.Keys(targets)) {
		target := targets[id]
		renderer, err := renderers.For(target)
		if err != nil {
			return 0, fmt.Errorf("rebase target revisions: %w", err)
		}
		formats = append(formats, TargetFormat{
			TargetID: target.ID, FormatKey: target.FormatKey, RendererID: target.RendererID,
			RendererVersion: renderer.Version(), Revision: domain.TargetRevision(target),
		})
	}
	return store.RebaseLegacyTargetRevisions(ctx, formats)
}
