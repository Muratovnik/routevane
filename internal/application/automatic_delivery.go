package application

import (
	"context"
	"errors"
	"time"
)

var ErrAutomaticDeliveryComposition = errors.New("invalid automatic delivery composition")

type scheduledDeviceSource interface {
	Device(context.Context, string) (Device, error)
	DeviceCredential(context.Context, string) (string, error)
}

type scheduledDeploymentTarget interface {
	Deploy(context.Context, DeployCommand) (DeployResult, error)
}

type scheduledOutputSource interface {
	Output(context.Context, string) (Output, error)
}

type AutomaticDeliveryConfig struct {
	Devices     scheduledDeviceSource
	Deployments scheduledDeploymentTarget
	Outputs     scheduledOutputSource
	Gate        *DeliveryGate
	// AttemptBudget is configurable only for deterministic boundary tests. A
	// zero value selects the production budget.
	AttemptBudget time.Duration
}

// AutomaticDeliveryService is the explicit bridge between publication,
// registered-device consent and the deployment lifecycle. Publication stays
// unaware of credentials and transports; deployment stays unaware of timers.
type AutomaticDeliveryService struct{ config AutomaticDeliveryConfig }

func NewAutomaticDeliveryService(config AutomaticDeliveryConfig) (*AutomaticDeliveryService, error) {
	if config.Devices == nil || config.Deployments == nil || config.Outputs == nil || config.Gate == nil || config.AttemptBudget < 0 {
		return nil, ErrAutomaticDeliveryComposition
	}
	if config.AttemptBudget == 0 {
		config.AttemptBudget = automaticDeliveryBudget
	}
	return &AutomaticDeliveryService{config: config}, nil
}

const automaticDeliveryBudget = 3 * time.Minute

// Deliver applies only artifacts built by these runs, to only the device named
// by each output, and only after that device opted in. Each attempt has its own
// budget and a failure cannot prevent a sibling output from being attempted.
func (s *AutomaticDeliveryService) Deliver(ctx context.Context, runs []ScheduledRun) []ScheduledRun {
	for runIndex := range runs {
		for _, publication := range runs[runIndex].Publications {
			if publication.DeviceID == "" {
				continue
			}
			delivered, failure := s.deliverOne(ctx, publication)
			if failure != "" {
				runs[runIndex].deliveryFailed(publication, failure)
				continue
			}
			if delivered {
				runs[runIndex].Delivered++
			}
		}
	}
	return runs
}

func (s *AutomaticDeliveryService) deliverOne(ctx context.Context, publication ScheduledPublication) (bool, string) {
	attemptCtx, cancel := context.WithTimeout(ctx, s.config.AttemptBudget)
	defer cancel()
	release, err := s.config.Gate.Acquire(attemptCtx)
	if err != nil {
		return false, classifyAutomaticDeliveryFailure(err)
	}
	defer release()

	// Re-read every authorization input while holding the same gate as all
	// binding, device, and consent mutations. The gate remains held through the
	// deployment lifecycle and any rollback, so the final check and the first
	// side effect cannot be separated by a stale detach or disable.
	output, err := s.config.Outputs.Output(attemptCtx, publication.OutputID)
	if err != nil {
		return false, "output_unavailable"
	}
	if output.DeviceID != publication.DeviceID {
		return false, ""
	}
	device, err := s.config.Devices.Device(attemptCtx, publication.DeviceID)
	if err != nil {
		return false, "device_unavailable"
	}
	if !device.AutoDeliver {
		return false, ""
	}
	credential, err := s.config.Devices.DeviceCredential(attemptCtx, device.ID)
	if err != nil {
		return false, "credential_unavailable"
	}
	_, err = s.config.Deployments.Deploy(attemptCtx, DeployCommand{
		ArtifactID: publication.ArtifactID,
		Connection: Connection{
			URL: device.Address, Username: device.Account,
			Password: credential, Interface: device.Interface,
		},
		Confirm: true,
	})
	if err != nil {
		return false, classifyAutomaticDeliveryFailure(err)
	}
	return true, ""
}

func (r *ScheduledRun) deliveryFailed(publication ScheduledPublication, code string) {
	r.DeliveryFailures = append(r.DeliveryFailures, ScheduledDeliveryFailure{
		OutputID: publication.OutputID, DeviceID: publication.DeviceID,
		ArtifactID: publication.ArtifactID, Code: code,
	})
}

func classifyAutomaticDeliveryFailure(err error) string {
	switch {
	case errors.Is(err, ErrRollbackFailed):
		return "rollback_failed"
	case errors.Is(err, ErrVerifyFailed):
		return "verification_failed"
	case errors.Is(err, ErrBackupRequired):
		return "backup_failed"
	case errors.Is(err, ErrDeviceIncompatible):
		return "device_incompatible"
	case errors.Is(err, ErrConnectionInvalid):
		return "connection_invalid"
	case errors.Is(err, context.DeadlineExceeded):
		return "delivery_timeout"
	default:
		return "delivery_failed"
	}
}
