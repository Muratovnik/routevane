package application

import (
	"testing"
	"time"
)

var scheduleNow = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

func profileAt(interval RefreshInterval, last time.Time, failed bool) Profile {
	return Profile{ID: "list", Name: "list", RefreshInterval: interval, LastRefreshedAt: last, LastRefreshFailed: failed}
}

// A list that never said anything follows the service-wide rule, and keeps
// following it as that rule changes. Copying the default into the list at the
// moment it was created would freeze it instead.
func TestAProfileWithoutARuleFollowsTheDefault(t *testing.T) {
	schedule := ScheduleOf(profileAt(RefreshDefault, time.Time{}, false), RefreshWeekly)
	if schedule.Effective != RefreshWeekly || !schedule.FollowsDefault {
		t.Fatalf("schedule = %#v", schedule)
	}
	schedule = ScheduleOf(profileAt(RefreshDaily, time.Time{}, false), RefreshWeekly)
	if schedule.Effective != RefreshDaily || schedule.FollowsDefault {
		t.Fatalf("schedule = %#v", schedule)
	}
}

// Off is a rule, not the absence of one: a list may refuse the timer while the
// service-wide default says daily.
func TestAProfileMayTurnTheTimerOffAgainstTheDefault(t *testing.T) {
	schedule := ScheduleOf(profileAt(RefreshOff, time.Time{}, false), RefreshDaily)
	if schedule.Effective != RefreshOff || schedule.due(scheduleNow) {
		t.Fatalf("schedule = %#v", schedule)
	}
}

func TestWhatIsDue(t *testing.T) {
	tests := []struct {
		name     string
		profile  Profile
		fallback RefreshInterval
		due      bool
	}{
		{"never refreshed and scheduled", profileAt(RefreshDaily, time.Time{}, false), RefreshOff, true},
		{"never refreshed and off", profileAt(RefreshOff, time.Time{}, false), RefreshOff, false},
		{"inside the period", profileAt(RefreshDaily, scheduleNow.Add(-23*time.Hour), false), RefreshOff, false},
		{"exactly a period later", profileAt(RefreshDaily, scheduleNow.Add(-24*time.Hour), false), RefreshOff, true},
		{"weekly is not daily", profileAt(RefreshWeekly, scheduleNow.Add(-48*time.Hour), false), RefreshOff, false},
		{"weekly is due after a week", profileAt(RefreshWeekly, scheduleNow.Add(-8*24*time.Hour), false), RefreshOff, true},
		// A failed run advanced the clock, so the next attempt is the next
		// period rather than every tick against a source already failing.
		{"a failed run waits its period", profileAt(RefreshDaily, scheduleNow.Add(-time.Minute), true), RefreshOff, false},
		{"a failed run is due again later", profileAt(RefreshDaily, scheduleNow.Add(-25*time.Hour), true), RefreshOff, true},
		{"the default carries the list", profileAt(RefreshDefault, time.Time{}, false), RefreshDaily, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ScheduleOf(test.profile, test.fallback).due(scheduleNow); got != test.due {
				t.Fatalf("due = %v, want %v", got, test.due)
			}
		})
	}
}

// A stored value the product no longer implements must not silently become a
// timer: the safe reading of an unknown rule is off.
func TestAnUnknownStoredRuleReadsAsOff(t *testing.T) {
	schedule := ScheduleOf(profileAt(RefreshInterval("hourly"), time.Time{}, false), RefreshDaily)
	if schedule.Effective != RefreshOff || schedule.due(scheduleNow) {
		t.Fatalf("schedule = %#v", schedule)
	}
	schedule = ScheduleOf(profileAt(RefreshDefault, time.Time{}, false), RefreshInterval("hourly"))
	if schedule.Effective != RefreshOff {
		t.Fatalf("schedule = %#v", schedule)
	}
}

// A list that has never refreshed has no next time to state. Naming one would
// be inventing a moment nothing recorded.
func TestANeverRefreshedProfileStatesNoNextTime(t *testing.T) {
	schedule := ScheduleOf(profileAt(RefreshDaily, time.Time{}, false), RefreshOff)
	if !schedule.NextRefreshAt.IsZero() || !schedule.due(scheduleNow) {
		t.Fatalf("schedule = %#v", schedule)
	}
}

func TestOnlyTheThreeRulesAreAccepted(t *testing.T) {
	for _, valid := range []RefreshInterval{RefreshDefault, RefreshOff, RefreshDaily, RefreshWeekly} {
		if !valid.valid() {
			t.Fatalf("%q must be a valid list rule", valid)
		}
	}
	if RefreshDefault.validDefault() {
		t.Fatal("the service-wide rule has nothing to defer to")
	}
	if RefreshInterval("hourly").valid() {
		t.Fatal("an unimplemented rule must not be accepted")
	}
}
