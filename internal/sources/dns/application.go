package dns

import (
	"context"
	"errors"

	"github.com/Muratovnik/routevane/internal/application"
)

// Source adapts the bounded Observer to the consumer-owned application seam.
type Source struct {
	Observer *Observer
}

func (s Source) Observe(ctx context.Context, request application.SourceRequest) (application.SourceResult, error) {
	observer := s.Observer
	if observer == nil {
		return application.SourceResult{}, errors.New("DNS observer is nil")
	}
	copy := *observer
	copy.SourceRevision = request.SourceRevision
	result, err := copy.Observe(ctx, Query{ListID: request.ListID, ComponentID: request.ComponentID, SourceID: request.SourceID, Names: request.Names}, request.ObservedAt)
	if err != nil {
		return application.SourceResult{}, err
	}
	return application.SourceResult{Sightings: result.Sightings, Relations: result.Relations}, nil
}
