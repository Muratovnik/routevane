package application

import (
	"context"
	"fmt"
	"time"
)

// RefreshInterval is the closed set of scheduling rules. It is deliberately
// three coarse values rather than a duration: a routing list follows an
// upstream that publishes on the order of days, and a free-form interval would
// invite a minute-by-minute schedule that only hammers other people's servers.
type RefreshInterval string

const (
	// RefreshDefault means the list follows the service-wide rule. It is what a
	// list has until someone says otherwise, so changing the default reaches
	// every list that never disagreed with it.
	RefreshDefault RefreshInterval = ""
	RefreshOff     RefreshInterval = "off"
	RefreshDaily   RefreshInterval = "daily"
	RefreshWeekly  RefreshInterval = "weekly"
)

// SettingRefreshInterval is the stored key of the service-wide rule.
const SettingRefreshInterval = "refresh.interval"

// Every duration is the whole period, not an approximation: a daily list is
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

// Schedule is what a list is actually refreshed by, stated so a screen can show
// the rule and where it came from without inferring either.
type Schedule struct {
	// Interval is the list's own rule, empty when it follows the default.
	Interval RefreshInterval `json:"interval"`
	// Effective is the rule that actually applies.
	Effective RefreshInterval `json:"effective"`
	// FollowsDefault says the effective rule is the service-wide one, so a
	// screen can say "as in settings" rather than repeating the value as if the
	// list had chosen it.
	FollowsDefault  bool      `json:"follows_default"`
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitzero"`
	// LastRefreshFailed marks that the most recent scheduled run did not
	// finish. The previous file keeps serving, and the next attempt is the next
	// tick rather than immediately.
	LastRefreshFailed bool `json:"last_refresh_failed"`
	// NextRefreshAt is when this list is next due. Zero when nothing is due,
	// which is the honest answer for a list that does not refresh on a timer.
	NextRefreshAt time.Time `json:"next_refresh_at,omitzero"`
}

// ScheduleOf resolves one list against the service-wide default.
func ScheduleOf(list List, fallback RefreshInterval) Schedule {
	effective := list.RefreshInterval
	follows := false
	if effective == RefreshDefault {
		effective = fallback
		follows = true
	}
	if !effective.validDefault() {
		effective = RefreshOff
	}
	schedule := Schedule{
		Interval: list.RefreshInterval, Effective: effective, FollowsDefault: follows,
		LastRefreshedAt: list.LastRefreshedAt, LastRefreshFailed: list.LastRefreshFailed,
	}
	if period, scheduled := effective.period(); scheduled {
		// A list that has never refreshed is due now rather than one period
		// from an epoch it was never part of.
		schedule.NextRefreshAt = list.LastRefreshedAt.Add(period)
		if list.LastRefreshedAt.IsZero() {
			schedule.NextRefreshAt = time.Time{}
		}
	}
	return schedule
}

// due reports whether this list should refresh at the given moment.
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

// SetListRefreshInterval records a list's own rule. Setting it to the default
// is how a list goes back to following settings, which is not the same as
// setting it to whatever the default currently is.
func (s *PublicationService) SetListRefreshInterval(ctx context.Context, listID string, interval RefreshInterval) (List, error) {
	if !interval.valid() {
		return List{}, fmt.Errorf("invalid refresh interval")
	}
	list, err := s.List(ctx, listID)
	if err != nil {
		return List{}, err
	}
	if err := list.writable(); err != nil {
		return List{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return List{}, fmt.Errorf("clock returned zero time")
	}
	list.RefreshInterval = interval
	list.UpdatedAt = now
	if err := s.config.Store.UpdateListSchedule(ctx, list.ID, interval, list.LastRefreshedAt, list.LastRefreshFailed, now); err != nil {
		return List{}, err
	}
	return list, nil
}

// ListSchedule states how one list is refreshed right now.
func (s *PublicationService) ListSchedule(ctx context.Context, list List) (Schedule, error) {
	fallback, err := s.DefaultRefreshInterval(ctx)
	if err != nil {
		return Schedule{}, err
	}
	return ScheduleOf(list, fallback), nil
}

// RunDueRefreshes refreshes and rebuilds every list whose rule says it is due.
// It is the scheduler's whole body: the decision of what is due is here, and
// the caller owns only when to ask.
//
// A failed list is marked and its clock is advanced, so the next attempt is the
// next tick rather than a retry loop against a source that is already failing.
// The previous file keeps serving throughout: a refresh that does not finish
// publishes nothing, and nothing published is ever removed.
func (s *PublicationService) RunDueRefreshes(ctx context.Context) ([]ScheduledRun, error) {
	fallback, err := s.DefaultRefreshInterval(ctx)
	if err != nil {
		return nil, err
	}
	if _, scheduled := fallback.period(); !scheduled {
		// The default is off, but a list may still have said daily itself.
		fallback = RefreshOff
	}
	lists, err := s.config.Store.Lists(ctx)
	if err != nil {
		return nil, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return nil, fmt.Errorf("clock returned zero time")
	}
	runs := make([]ScheduledRun, 0)
	for _, list := range lists {
		// An archived list is skipped before it is judged due, so the timer
		// never marks it failed for a refresh it was never going to attempt.
		if list.Archived() {
			continue
		}
		if !ScheduleOf(list, fallback).due(now) {
			continue
		}
		run := ScheduledRun{ListID: list.ID, Name: list.Name, At: now}
		if _, refreshErr := s.Refresh(ctx, list.ID); refreshErr != nil {
			run.Error = refreshErr.Error()
		} else {
			outputs, outputErr := s.config.Store.OutputsByList(ctx, list.ID)
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
		if err := s.config.Store.UpdateListSchedule(ctx, list.ID, list.RefreshInterval, now, failed, list.UpdatedAt); err != nil {
			return runs, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// ScheduledRun is one list the scheduler touched, reported so the operator can
// read what the timer did without reconstructing it from the artifact history.
type ScheduledRun struct {
	ListID           string                     `json:"list_id"`
	Name             string                     `json:"name"`
	At               time.Time                  `json:"at"`
	Built            int                        `json:"built"`
	Publications     []ScheduledPublication     `json:"publications,omitempty"`
	Failures         []ScheduledOutputFailure   `json:"failures,omitempty"`
	Delivered        int                        `json:"delivered"`
	DeliveryFailures []ScheduledDeliveryFailure `json:"delivery_failures,omitempty"`
	// Error is reserved for a list-wide refresh or storage failure. Individual
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
