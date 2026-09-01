package application

import (
	"context"

	"github.com/Muratovnik/routevane/internal/domain"
)

type ArtifactPayload struct {
	Artifact ArtifactBuildRecord
	Payload  []byte
	Fallback bool
	// Descriptor is the format metadata of the renderer that produced these
	// bytes. It lets a transport name and type the download without holding a
	// per-format constant of its own.
	Descriptor domain.RendererDescriptor
}

func (s *PublicationService) Snapshot(ctx context.Context, id string) (PlanSnapshotRecord, error) {
	if !isHexID(id) {
		return PlanSnapshotRecord{}, ErrNotFound
	}
	return s.config.Store.PlanSnapshot(ctx, id)
}
func (s *PublicationService) Artifact(ctx context.Context, id string) (ArtifactPayload, error) {
	if !isHexID(id) {
		return ArtifactPayload{}, ErrNotFound
	}
	return s.verifiedArtifact(ctx, id)
}
func (s *PublicationService) verifiedArtifact(ctx context.Context, id string) (ArtifactPayload, error) {
	artifact, err := s.config.Store.ArtifactBuild(ctx, id)
	if err != nil {
		return ArtifactPayload{}, err
	}
	renderer, registered := s.config.Renderers[artifact.RendererID]
	if !registered || renderer == nil || renderer.Version() != artifact.RendererVersion {
		// The bytes exist but this build no longer implements the format that
		// produced them. Serving them untyped would be worse than refusing.
		return ArtifactPayload{}, ErrArtifactUnavailable
	}
	payload, err := s.config.Files.ReadPublished(ctx, artifact, renderer.Descriptor())
	if err != nil {
		return ArtifactPayload{}, ErrArtifactUnavailable
	}
	return ArtifactPayload{Artifact: artifact, Payload: payload, Descriptor: renderer.Descriptor()}, nil
}
