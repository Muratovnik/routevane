package application

import (
	"context"
	"errors"

	"github.com/Muratovnik/routevane/internal/domain"
)

var ErrFQDNPrefix = errors.New("invalid FQDN group prefix")

func ValidateFQDNPrefix(targetID, prefix string) error {
	if prefix == "" {
		return nil
	}
	if targetID != "keenetic-dns" || len(prefix) > 24 || domain.ValidateSlug(prefix) != nil {
		return ErrFQDNPrefix
	}
	return nil
}

type outputPrefixStore interface {
	UpdateOutputFQDNPrefix(context.Context, string, string) error
}

func (s *PublicationService) SetOutputFQDNPrefix(ctx context.Context, outputID, prefix string) (Output, error) {
	output, err := s.Output(ctx, outputID)
	if err != nil {
		return Output{}, err
	}
	if output.TargetID != "keenetic-dns" {
		return Output{}, ErrFQDNPrefix
	}
	if err := ValidateFQDNPrefix(output.TargetID, prefix); err != nil {
		return Output{}, err
	}
	store, ok := s.config.Store.(outputPrefixStore)
	if !ok {
		return Output{}, ErrFQDNPrefix
	}
	if err := store.UpdateOutputFQDNPrefix(ctx, output.ID, prefix); err != nil {
		return Output{}, err
	}
	output.FQDNGroupPrefix = prefix
	return output, nil
}
