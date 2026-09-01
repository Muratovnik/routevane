package httpfeed

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
	if s.Observer == nil {
		return application.SourceResult{}, errors.New("HTTP feed observer is nil")
	}
	result, err := s.Observer.Observe(ctx, Query{
		ServiceID:      request.ServiceID,
		ComponentID:    request.ComponentID,
		SourceID:       request.SourceID,
		SourceRevision: request.SourceRevision,
		URL:            request.URL,
		Format:         request.Format,
		SourceClass:    request.SourceClass,
	}, request.ObservedAt)
	if err != nil {
		return application.SourceResult{}, err
	}
	return application.SourceResult{Sightings: result.Sightings, Skipped: result.Skipped}, nil
}
