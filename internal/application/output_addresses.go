package application

import (
	"context"
	"errors"

	"github.com/Muratovnik/routevane/internal/planner"
)

// ErrKeepAddresses refuses the setting on a format that cannot hold it: one
// without name rules never drops an address for a name, and one without
// address rules has nowhere to keep it.
var ErrKeepAddresses = errors.New("format does not take names and addresses")

// PlannerOptions is the policy this output's file is planned under.
func (o Output) PlannerOptions() planner.Options {
	return planner.Options{KeepAddressesBesideNames: o.KeepAddresses}
}

type outputKeepAddressesStore interface {
	UpdateOutputKeepAddresses(context.Context, string, bool) error
}

// SetOutputKeepAddresses records whether an output keeps the addresses a name
// already covers (ADR 0057). It takes effect at the output's next build.
func (s *PublicationService) SetOutputKeepAddresses(ctx context.Context, outputID string, keep bool) (Output, error) {
	output, err := s.writableOutput(ctx, outputID)
	if err != nil {
		return Output{}, err
	}
	target, ok := s.config.Targets[output.TargetID]
	if !ok {
		return Output{}, ErrTargetChanged
	}
	if keep && !target.Constraints.TakesNamesAndAddresses() {
		return Output{}, ErrKeepAddresses
	}
	store, ok := s.config.Store.(outputKeepAddressesStore)
	if !ok {
		return Output{}, ErrKeepAddresses
	}
	if err := store.UpdateOutputKeepAddresses(ctx, output.ID, keep); err != nil {
		return Output{}, err
	}
	output.KeepAddresses = keep
	return output, nil
}
