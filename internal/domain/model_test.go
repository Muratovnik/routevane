package domain

import (
	"net/netip"
	"testing"
	"time"
)

func TestNormalizeDomainAndAddress(t *testing.T) {
	if got, err := NormalizeDomain("WWW.Example.COM."); err != nil || got != "www.example.com" {
		t.Fatalf("NormalizeDomain() = %q, %v", got, err)
	}
	for _, input := range []string{"", " example.com", "example..com", "-example.com", "example-.com", "пример.рф", "foo_bar.example"} {
		if _, err := NormalizeDomain(input); err == nil {
			t.Errorf("NormalizeDomain(%q) accepted malformed input", input)
		}
	}
	if got, err := ParseAddr("::ffff:192.0.2.1"); err != nil || got.String() != "192.0.2.1" || !got.Is4() {
		t.Fatalf("mapped address = %v, %v", got, err)
	}
	for _, input := range []string{" 192.0.2.1", "192.0.2.1 ", "192.0.2.1%eth0", "garbage"} {
		if _, err := ParseAddr(input); err == nil {
			t.Errorf("ParseAddr(%q) accepted malformed input", input)
		}
	}
}

func TestSeedNormalizeRejectsBoundaryWhitespace(t *testing.T) {
	for _, value := range []string{" example.com", "example.com ", "192.0.2.1 ", " 192.0.2.1"} {
		seed := Seed{Kind: RuleDomainSuffix, Value: value, ComponentID: "web"}
		if _, err := seed.Normalize(); err == nil {
			t.Errorf("Seed.Normalize(%q) accepted boundary whitespace", value)
		}
	}
}

func TestSightingIsFreshCutoffAndLifecycle(t *testing.T) {
	cutoff := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resource, _ := NewAddrResourceFromString("192.0.2.1")
	tests := []struct {
		name       string
		validity   ObservationValidity
		validUntil time.Time
		want       bool
	}{
		{name: "before expiry", validity: ValidityValid, validUntil: cutoff.Add(time.Nanosecond), want: true},
		{name: "at expiry", validity: ValidityValid, validUntil: cutoff, want: false},
		{name: "after expiry", validity: ValidityValid, validUntil: cutoff.Add(-time.Nanosecond), want: false},
		{name: "stale flag can reappear", validity: ValidityStale, validUntil: cutoff.Add(time.Hour), want: true},
		{name: "invalid lifecycle", validity: ValidityInvalid, validUntil: cutoff.Add(time.Hour), want: false},
		{name: "archived lifecycle", validity: ValidityArchived, validUntil: cutoff.Add(time.Hour), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sighting := Sighting{Resource: resource, Validity: test.validity, ValidUntil: test.validUntil}
			if got := sighting.IsFresh(cutoff); got != test.want {
				t.Fatalf("IsFresh() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLifecycleEqualityAndSlugBoundary(t *testing.T) {
	expiry := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if got := LifecycleAt(expiry, expiry, false); got != ValidityStale {
		t.Fatalf("expiry equality=%q", got)
	}
	if got := LifecycleAt(expiry, expiry.Add(StaleRetention), false); got != ValidityArchived {
		t.Fatalf("retention equality=%q", got)
	}
	for _, valid := range []string{"a", "example", "dns-main", "list-2"} {
		if err := ValidateSlug(valid); err != nil {
			t.Errorf("ValidateSlug(%q)=%v", valid, err)
		}
	}
	for _, invalid := range []string{"", "../x", "Example", "a_b", "-a", "a-", "1a"} {
		if err := ValidateSlug(invalid); err == nil {
			t.Errorf("ValidateSlug(%q) accepted invalid id", invalid)
		}
	}
}

func FuzzNormalizeDomain(f *testing.F) {
	for _, seed := range []string{"example.com", "Example.COM.", "", "a..b", "a b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got, err := NormalizeDomain(input)
		if err == nil {
			if got == "" || len(got) > 253 {
				t.Fatalf("invalid normalized domain %q", got)
			}
			if got != NormalizeDomainValue(got) {
				t.Fatalf("domain not canonical: %q", got)
			}
		}
	})
}

func FuzzParseAddr(f *testing.F) {
	for _, seed := range []string{"127.0.0.1", "2001:db8::1", "::ffff:192.0.2.1", "bad"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got, err := ParseAddr(input)
		if err == nil {
			if !got.IsValid() || got.Is4In6() {
				t.Fatalf("noncanonical address %v", got)
			}
			if reparsed, parseErr := netip.ParseAddr(got.String()); parseErr != nil || reparsed != got {
				t.Fatalf("address does not round trip: %v", got)
			}
		}
	})
}
