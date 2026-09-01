package dns

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type fakeResolver struct {
	hosts    map[string][]string
	cnames   map[string]string
	err      error
	cnameErr error
}

func (f fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.hosts[host]...), nil
}

func (f fakeResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.cnameErr != nil {
		return "", f.cnameErr
	}
	return f.cnames[host], nil
}

func TestObserverNormalizesDeduplicatesAndRecordsTerminalCNAME(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resolver := fakeResolver{hosts: map[string][]string{"www.example.com": {"2001:db8::2", "192.0.2.2", "::ffff:192.0.2.1", "192.0.2.2"}}, cnames: map[string]string{"www.example.com": "EDGE.Example.COM."}}
	result, err := NewObserver(resolver).Observe(context.Background(), Query{ServiceID: "example", ComponentID: "web", SourceID: "dns", Names: []string{"WWW.Example.COM.", "www.example.com"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sightings) != 3 {
		t.Fatalf("sightings = %#v", result.Sightings)
	}
	for _, sighting := range result.Sightings {
		if sighting.ObservationCount != 1 {
			t.Fatalf("duplicate RR rows changed count: %#v", result.Sightings)
		}
		if sighting.TTLKnown || sighting.TTLSeconds != 0 {
			t.Fatalf("system resolver validity was mislabeled as observed TTL: %#v", sighting)
		}
	}
	if result.Sightings[0].Resource.CanonicalValue() != "192.0.2.1" || result.Sightings[1].Resource.CanonicalValue() != "192.0.2.2" || result.Sightings[2].Resource.CanonicalValue() != "2001:db8::2" {
		t.Fatalf("sightings not sorted/canonical: %#v", result.Sightings)
	}
	if len(result.Relations) != 1 || result.Relations[0].SourceResource.CanonicalValue() != "www.example.com" || result.Relations[0].TargetResource.CanonicalValue() != "edge.example.com" || result.Relations[0].SourceRevision != SourceRevision {
		t.Fatalf("relations = %#v", result.Relations)
	}
	if got := result.Sightings[0].ValidUntil; !got.Equal(now.Add(ConservativeValidity)) {
		t.Fatalf("valid until = %s", got)
	}
}

func TestObserverErrorsAtomically(t *testing.T) {
	result, err := NewObserver(fakeResolver{err: errors.New("resolver down")}).Observe(context.Background(), Query{ServiceID: "example", Names: []string{"example.com"}}, time.Now().UTC())
	if err == nil {
		t.Fatal("expected resolver error")
	}
	if len(result.Sightings) != 0 || len(result.Relations) != 0 {
		t.Fatalf("partial result = %#v", result)
	}
}

func TestObserverKeepsValidAddressesWhenCNAMEEnrichmentFails(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resolver := fakeResolver{
		hosts:    map[string][]string{"localhost": {"127.0.0.1"}},
		cnameErr: errors.New("CNAME lookup unsupported"),
	}
	result, err := NewObserver(resolver).Observe(context.Background(), Query{ServiceID: "example", Names: []string{"localhost"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sightings) != 1 || result.Sightings[0].Resource.CanonicalValue() != "127.0.0.1" || len(result.Relations) != 0 {
		t.Fatalf("supplemental CNAME failure changed address evidence: %#v", result)
	}
}

func TestObserverBoundsUniqueAddresses(t *testing.T) {
	addresses := make([]string, 0, MaxUniqueAddresses+1)
	for i := 0; i < MaxUniqueAddresses+1; i++ {
		addresses = append(addresses, fmt.Sprintf("2001:db8::%x", i))
	}
	result, err := NewObserver(fakeResolver{hosts: map[string][]string{"example.com": addresses}}).Observe(context.Background(), Query{ServiceID: "example", Names: []string{"example.com"}}, time.Now().UTC())
	if err == nil || len(result.Sightings) != 0 {
		t.Fatalf("expected bounded atomic failure, result=%#v err=%v", result, err)
	}
}

func TestObserverDuplicatePermutationIsOneObservation(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	first := fakeResolver{hosts: map[string][]string{"a.example.com": {"192.0.2.2", "192.0.2.1", "192.0.2.2"}, "b.example.com": {"192.0.2.1"}}}
	second := fakeResolver{hosts: map[string][]string{"a.example.com": {"192.0.2.1", "192.0.2.2"}, "b.example.com": {"192.0.2.1", "192.0.2.1"}}}
	query := func(names []string, resolver Resolver) Result {
		result, err := NewObserver(resolver).Observe(context.Background(), Query{ServiceID: "example", ComponentID: "web", SourceID: "dns", Names: names}, now)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	a := query([]string{"b.example.com", "a.example.com"}, first)
	b := query([]string{"a.example.com", "b.example.com"}, second)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("duplicate/order changed observation: %#v != %#v", a, b)
	}
	for _, sighting := range a.Sightings {
		if sighting.ObservationCount != 1 {
			t.Fatalf("count = %d, want one", sighting.ObservationCount)
		}
	}
}

type cancellationResolver struct {
	cancel context.CancelFunc
	calls  int
}

func (r *cancellationResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	r.calls++
	if r.calls == 1 {
		r.cancel()
		return []string{"192.0.2.1"}, nil
	}
	return []string{"192.0.2.2"}, nil
}

func TestObserverCancellationIsAtomic(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := NewObserver(&cancellationResolver{cancel: func() {}}).Observe(preCanceled, Query{ServiceID: "example", Names: []string{"a.example.com"}}, now)
	if err == nil || len(result.Sightings) != 0 || len(result.Relations) != 0 {
		t.Fatalf("pre-cancel result=%#v err=%v", result, err)
	}

	ctx, cancelAfterFirst := context.WithCancel(context.Background())
	resolver := &cancellationResolver{cancel: cancelAfterFirst}
	result, err = NewObserver(resolver).Observe(ctx, Query{ServiceID: "example", Names: []string{"a.example.com", "b.example.com"}}, now)
	if err == nil || len(result.Sightings) != 0 || len(result.Relations) != 0 {
		t.Fatalf("partial-cancel result=%#v err=%v", result, err)
	}
}

type cnameCancellationResolver struct{ cancel context.CancelFunc }

func (*cnameCancellationResolver) LookupHost(context.Context, string) ([]string, error) {
	return []string{"192.0.2.1"}, nil
}

func (r *cnameCancellationResolver) LookupCNAME(ctx context.Context, _ string) (string, error) {
	r.cancel()
	return "", ctx.Err()
}

func TestObserverCNAMECancellationRemainsAtomic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result, err := NewObserver(&cnameCancellationResolver{cancel: cancel}).Observe(
		ctx,
		Query{ServiceID: "example", Names: []string{"a.example.com"}},
		time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
	)
	if err == nil || len(result.Sightings) != 0 || len(result.Relations) != 0 {
		t.Fatalf("CNAME cancellation result=%#v err=%v", result, err)
	}
}
