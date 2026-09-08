package application

import (
	"context"
	"fmt"
	"time"
)

// RefreshInterval is the closed set of scheduling rules. It is deliberately
// three coarse values rather than a duration: a profile follows an
// upstream that publishes on the order of days, and a free-form interval would
// invite a minute-by-minute schedule that only hammers other people's servers.
type RefreshInterval string

const (
	// RefreshDefault means the profile follows the service-wide rule. It is what a
	// profile has until someone says otherwise, so changing the default reaches
	// every profile that never disagreed with it.
	RefreshDefault RefreshInterval = ""
	RefreshOff     RefreshInterval = "off"
	RefreshDaily   RefreshInterval = "daily"
	RefreshWeekly  RefreshInterval = "weekly"
)

// SettingRefreshInterval is the stored key of the service-wide rule.
const SettingRefreshInterval = "refresh.interval"

// Every duration is the whole period, not an approximation: a daily profile is
// refreshed no more than once in twenty-four hours.
const (
	refreshDailyPeriod  = 24 * time.Hour
	refreshWeeklyPeriod = 7 * refreshDailyPeriod
)

func (i RefreshInterval) valid() bool {
	switch i {
	case RefreshDefault, RefreshOff, RefreshDaily, RefreshWeekly:
		return true
	default:
		return false
	}
}

// validDefault is the same set without the deferral: the service-wide rule has
// nothing to defer to.
func (i RefreshInterval) validDefault() bool {
	switch i {
	case RefreshOff, RefreshDaily, RefreshWeekly:
		return true
	default:
		return false
	}
}

func (i RefreshInterval) period() (time.Duration, bool) {
	switch i {
	case RefreshDaily:
		return refreshDailyPeriod, true
	case RefreshWeekly:
		return refreshWeeklyPeriod, true
	default:
		return 0, false
	}
}

// Schedule is what a profile is actually refreshed by, stated so a screen can show
// the rule and where it came from without inferring either.
type Schedule struct {
	// Interval is the profile's own rule, empty when it follows the default.
	Interval RefreshInterval `json:"interval"`
	// Effective is the rule that actually applies.
	Effective RefreshInterval `json:"effective"`
	// FollowsDefault says the effective rule is the service-wide one, so a
	// screen can say "as in settings" rather than repeating the value as if the
	// profile had chosen it.
	FollowsDefault  bool      `json:"follows_default"`
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitzero"`
	// LastRefreshFailed marks that the most recent scheduled run did not
	// finish. The previous file keeps serving, and the next attempt is the next
	// tick rather than immediately.
	LastRefreshFailed bool `json:"last_refresh_failed"`
	// NextRefreshAt is when this profile is next due. Zero when nothing is due,
	// which is the honest answer for a profile that does not refresh on a timer.
	NextRefreshAt time.Time `json:"next_refresh_at,omitzero"`
}

// ScheduleOf resolves one profile against the service-wide default.
func ScheduleOf(profile Profile, fallback RefreshInterval) Schedule {
	effective := profile.RefreshInterval
	follows := false
	if effective == RefreshDefault {
		effective = fallback
		follows = true
	}
	if !effective.validDefault() {
		effective = RefreshOff
	}
	schedule := Schedule{
		Interval: profile.RefreshInterval, Effective: effective, FollowsDefault: follows,
		LastRefreshedAt: profile.LastRefreshedAt, LastRefreshFailed: profile.LastRefreshFailed,
	}
	if period, scheduled := effective.period(); scheduled {
		// A profile that has never refreshed is due now rather than one period
		// from an epoch it was never part of.
		schedule.NextRefreshAt = profile.LastRefreshedAt.Add(period)
		if profile.LastRefreshedAt.IsZero() {
			schedule.NextRefreshAt = time.Time{}
		}
	}
	return schedule
}

// due reports whether this profile should refresh at the given moment.
func (s Schedule) due(now time.Time) bool {
	if _, scheduled := s.Effective.period(); !scheduled {
		return false
	}
	if s.LastRefreshedAt.IsZero() {
		return true
	}
	return !now.Before(s.NextRefreshAt)
}

// DefaultRefreshInterval reads the service-wide rule. An unset or unreadable
// value is off: nothing reaches the network on a timer until the operator says
// so.
func (s *PublicationService) DefaultRefreshInterval(ctx context.Context) (RefreshInterval, error) {
	value, err := s.config.Store.Setting(ctx, SettingRefreshInterval)
	if err != nil {
		return RefreshOff, err
	}
	interval := RefreshInterval(value)
	if !interval.validDefault() {
		return RefreshOff, nil
	}
	return interval, nil
}

func (s *PublicationService) SetDefaultRefreshInterval(ctx context.Context, interval RefreshInterval) error {
	if !interval.validDefault() {
		return fmt.Errorf("invalid refresh interval")
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return fmt.Errorf("clock returned zero time")
	}
	return s.config.Store.PutSetting(ctx, SettingRefreshInterval, string(interval), now)
}

// SetProfileRefreshInterval records a profile's own rule. Setting it to the default
// is how a profile goes back to following settings, which is not the same as
// setting it to whatever the default currently is.
func (s *PublicationService) SetProfileRefreshInterval(ctx context.Context, profileID string, interval RefreshInterval) (Profile, error) {
	if !interval.valid() {
		return Profile{}, fmt.Errorf("invalid refresh interval")
	}
	profile, err := s.Profile(ctx, profileID)
	if err != nil {
		return Profile{}, err
	}
	if err := profile.writable(); err != nil {
		return Profile{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Profile{}, fmt.Errorf("clock returned zero time")
	}
	profile.RefreshInterval = interval
	profile.UpdatedAt = now
	if err := s.config.Store.UpdateProfileRefreshInterval(ctx, profile.ID, interval, now); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// ProfileSchedule states how one profile is refreshed right now.
func (s *PublicationService) ProfileSchedule(ctx context.Context, profile Profile) (Schedule, error) {
	fallback, err := s.DefaultRefreshInterval(ctx)
	if err != nil {
		return Schedule{}, err
	}
	return ScheduleOf(profile, fallback), nil
}

// RunDueRefreshes refreshes and rebuilds every profile whose rule says it is due.
// It is the scheduler's whole body: the decision of what is due is here, and
// the caller owns only when to ask.
//
// A failed profile is marked and its clock is advanced, so the next attempt is the
// next tick rather than a retry loop against a source that is already failing.
// The previous file keeps serving throughout: a refresh that does not finish
// publishes nothing, and nothing published is ever removed.
func (s *PublicationService) RunDueRefreshes(ctx context.Context) ([]ScheduledRun, error) {
	fallback, err := s.DefaultRefreshInterval(ctx)
	if err != nil {
		return nil, err
	}
	if _, scheduled := fallback.period(); !scheduled {
		// The default is off, but a profile may still have said daily itself.
		fallback = RefreshOff
	}
	profiles, err := s.config.Store.ProfilesForScheduling(ctx)
	if err != nil {
		return nil, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return nil, fmt.Errorf("clock returned zero time")
	}
	runs := make([]ScheduledRun, 0)
	for _, profile := range profiles {
		// An archived profile is skipped before it is judged due, so the timer
		// never marks it failed for a refresh it was never going to attempt.
		if profile.Archived() {
			continue
		}
		if !ScheduleOf(profile, fallback).due(now) {
			continue
		}
		run := ScheduledRun{ProfileID: profile.ID, Name: profile.Name, At: now}
		if _, refreshErr := s.Refresh(ctx, profile.ID); refreshErr != nil {
			run.Error = refreshErr.Error()
		} else {
			outputs, outputErr := s.config.Store.OutputsByProfile(ctx, profile.ID)
			if outputErr != nil {
				run.Error = outputErr.Error()
			} else {
				for _, output := range outputs {
					built, buildErr := s.Build(ctx, output.ID)
					if buildErr != nil {
						details := ClassifyBuildFailure(buildErr)
						run.Failures = append(run.Failures, ScheduledOutputFailure{
							OutputID: output.ID, TargetID: output.TargetID, Code: details.Code,
						})
						continue
					}
					run.Built++
					run.Publications = append(run.Publications, ScheduledPublication{
						OutputID: output.ID, TargetID: output.TargetID, DeviceID: output.DeviceID,
						ArtifactID: built.Artifact.ID,
					})
				}
			}
		}
		failed := run.Failed()
		if err := s.config.Store.RecordProfileRefreshResult(ctx, profile.ID, now, failed); err != nil {
			return runs, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// ScheduledRun is one profile the scheduler touched, reported so the operator can
// read what the timer did without reconstructing it from the artifact history.
type ScheduledRun struct {
	ProfileID        string                     `json:"list_id"`
	Name             string                     `json:"name"`
	At               time.Time                  `json:"at"`
	Built            int                        `json:"built"`
	Publications     []ScheduledPublication     `json:"publications,omitempty"`
	Failures         []ScheduledOutputFailure   `json:"failures,omitempty"`
	Delivered        int                        `json:"delivered"`
	DeliveryFailures []ScheduledDeliveryFailure `json:"delivery_failures,omitempty"`
	// Error is reserved for a profile-wide refresh or storage failure. Individual
	// output failures stay structured above so one failed format cannot hide a
	// successful sibling or stop it from being attempted.
	Error string `json:"error,omitempty"`
}

// ScheduledPublication is a file created by this exact scheduled run. Only
// these artifacts are eligible for automatic delivery; a failed rebuild never
// falls back to sending an older file as if it were fresh.
type ScheduledPublication struct {
	OutputID   string `json:"output_id"`
	TargetID   string `json:"target_id"`
	DeviceID   string `json:"device_id,omitempty"`
	ArtifactID string `json:"artifact_id"`
}

// ScheduledDeliveryFailure is the bounded, non-secret report of one failed
// unattended attempt. Raw transport errors stay in the deployment boundary.
type ScheduledDeliveryFailure struct {
	OutputID   string `json:"output_id"`
	DeviceID   string `json:"device_id"`
	ArtifactID string `json:"artifact_id"`
	Code       string `json:"code"`
}

// ScheduledOutputFailure names one output the scheduler could not rebuild.
// Code is the same bounded classification persisted on the output attempt;
// raw internal errors remain in the boundary that owns local logs.
type ScheduledOutputFailure struct {
	OutputID string `json:"output_id"`
	TargetID string `json:"target_id"`
	Code     string `json:"code"`
}

func (r ScheduledRun) Failed() bool {
	return r.Error != "" || len(r.Failures) != 0
}
