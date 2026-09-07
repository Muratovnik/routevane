package discovery

import (
	"errors"
	"testing"
)

func TestNormalizeTargetDerivesTheRegistrableDomainFromThePublicSuffixList(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantURL    string
		wantHost   string
		wantDomain string
		wantSuffix string
		wantICANN  bool
	}{
		// A multi-label suffix must be handled by the same code path as a
		// single-label one. Getting example.co.uk right without listing co.uk
		// anywhere is the whole point of using the list.
		{"multi label suffix", "https://www.example.co.uk/watch", "https://www.example.co.uk/watch", "www.example.co.uk", "example.co.uk", "co.uk", true},
		{"multi label suffix apex", "example.co.uk", "https://example.co.uk/", "example.co.uk", "example.co.uk", "co.uk", true},
		{"single label suffix", "https://www.example.com", "https://www.example.com/", "www.example.com", "example.com", "com", true},
		{"deep subdomain", "a.b.c.example.com/path?q=1", "https://a.b.c.example.com/path?q=1", "a.b.c.example.com", "example.com", "com", true},
		{"bare host", "example.org", "https://example.org/", "example.org", "example.org", "org", true},
		{"plaintext is upgraded", "http://example.com/x", "https://example.com/x", "example.com", "example.com", "com", true},
		{"trailing dot", "example.com.", "https://example.com/", "example.com", "example.com", "com", true},
		{"uppercase host", "HTTPS://WWW.Example.COM", "https://www.example.com/", "www.example.com", "example.com", "com", true},
		// A private-section suffix still yields a registrable domain, and the
		// section is reported rather than hidden.
		{"private section suffix", "https://project.github.io", "https://project.github.io/", "project.github.io", "project.github.io", "github.io", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			target, err := NormalizeTarget(testCase.raw)
			if err != nil {
				t.Fatal(err)
			}
			if target.URL != testCase.wantURL || target.Host != testCase.wantHost {
				t.Fatalf("target = %#v", target)
			}
			if target.RegistrableDomain != testCase.wantDomain || target.PublicSuffix != testCase.wantSuffix {
				t.Fatalf("target = %#v", target)
			}
			if target.ICANNSuffix != testCase.wantICANN {
				t.Fatalf("icann = %v, want %v", target.ICANNSuffix, testCase.wantICANN)
			}
		})
	}
}

func TestNormalizeTargetRefusesUnusableAndLocalTargets(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want error
	}{
		{"empty", "", ErrInvalidTarget},
		{"whitespace", "   ", ErrInvalidTarget},
		{"control byte", "https://example.com/\x00", ErrInvalidTarget},
		{"credentials", "https://user:secret@example.com", ErrInvalidTarget},
		{"file scheme", "file:///etc/passwd", ErrInvalidTarget},
		{"javascript scheme", "javascript:alert(1)", ErrInvalidTarget},
		{"localhost", "http://localhost:8080", ErrInvalidTarget},
		{"loopback name", "router.localhost", ErrInvalidTarget},
		{"mdns name", "printer.local", ErrInvalidTarget},
		{"internal name", "service.internal", ErrInvalidTarget},
		{"loopback literal", "https://127.0.0.1", ErrUnsafeDestination},
		{"private literal", "https://10.1.2.3", ErrUnsafeDestination},
		{"metadata literal", "https://169.254.169.254/latest", ErrUnsafeDestination},
		{"public literal has no domain", "https://203.0.113.10", ErrNoRegistrableDomain},
		{"bare suffix", "https://co.uk", ErrNoRegistrableDomain},
		{"bare tld", "https://com", ErrNoRegistrableDomain},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NormalizeTarget(testCase.raw); !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestSameSiteUsesTheRegistrableDomainNotAStringSuffix(t *testing.T) {
	cases := []struct {
		registrable string
		host        string
		want        bool
	}{
		{"example.co.uk", "example.co.uk", true},
		{"example.co.uk", "www.example.co.uk", true},
		{"example.co.uk", "cdn.a.example.co.uk", true},
		// A different registrant under the same suffix is not same-site, which a
		// plain string-suffix check would get wrong.
		{"example.co.uk", "other.co.uk", false},
		{"example.co.uk", "notexample.co.uk", false},
		{"example.com", "example.com.evil.test", false},
		{"example.com", "evilexample.com", false},
		{"example.com", "", false},
		{"example.com", "203.0.113.10", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.registrable+"/"+testCase.host, func(t *testing.T) {
			if got := SameSite(testCase.registrable, testCase.host); got != testCase.want {
				t.Fatalf("SameSite(%q, %q) = %v, want %v", testCase.registrable, testCase.host, got, testCase.want)
			}
		})
	}
}

func TestListIDForDerivesASlugFromTheRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"example.com":       "example",
		"example.co.uk":     "example",
		"my-service.org":    "my-service",
		"project.github.io": "project",
	}
	for registrable, want := range cases {
		got, err := ListIDFor(registrable)
		if err != nil || got != want {
			t.Fatalf("ServiceIDFor(%q) = %q, %v; want %q", registrable, got, err, want)
		}
	}
	for _, invalid := range []string{"", "1example.com", "-example.com", "UPPER.com"} {
		if _, err := ListIDFor(invalid); err == nil {
			t.Fatalf("accepted %q as a service identity", invalid)
		}
	}
}

func TestRegistrableDomainOfRefusesAddressesAndInvalidHosts(t *testing.T) {
	if got, err := RegistrableDomainOf("cdn.example.co.uk"); err != nil || got != "example.co.uk" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, invalid := range []string{"203.0.113.10", "2001:db8::1", "", "not a host"} {
		if _, err := RegistrableDomainOf(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}
