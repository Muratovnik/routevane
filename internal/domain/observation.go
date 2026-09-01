package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type ObservationValidity string

const (
	ValidityValid    ObservationValidity = "valid"
	ValidityStale    ObservationValidity = "stale"
	ValidityArchived ObservationValidity = "archived"
	ValidityInvalid  ObservationValidity = "invalid"
)

// SharedNetworkEvidence records an explicit, trusted catalog/metadata signal.
// It is never inferred from a source identifier, source class, RDAP, ASN, or
// the fact that an address was observed.
type SharedNetworkEvidence string

const (
	SharedNetworkEvidenceNone    SharedNetworkEvidence = ""
	SharedNetworkEvidenceTrusted SharedNetworkEvidence = "trusted"
)

type Sighting struct {
	ID                    string
	ServiceID             string
	ComponentID           string
	Resource              Resource
	SourceID              string
	SourceClass           SourceClass
	SourceRevision        string
	FirstSeen             time.Time
	LastSeen              time.Time
	ValidUntil            time.Time
	TTLSeconds            int64
	TTLKnown              bool
	ObservationCount      int
	Metadata              string
	Validity              ObservationValidity
	SharedNetworkEvidence SharedNetworkEvidence
}

func (s Sighting) IsFresh(cutoff time.Time) bool {
	return s.Resource.IsValid() && s.Validity != ValidityInvalid && s.Validity != ValidityArchived && s.ValidUntil.After(cutoff)
}

func (s Sighting) Fingerprint() string {
	parts := []string{s.ServiceID, s.ComponentID, s.Resource.Kind.String(), s.Resource.CanonicalValue(), string(s.SourceClass), s.SourceID, s.SourceRevision, string(s.SharedNetworkEvidence)}
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}

type RelationType string

const (
	RelationCNAMETo RelationType = "cname_to"
	// RelationLoadedBy records that a host was requested while another host's
	// document was loading. It is provenance about how a dependency appeared,
	// never proof that either host owns the other.
	RelationLoadedBy RelationType = "loaded_by"
	// RelationRedirectsTo records an observed redirect hop.
	RelationRedirectsTo RelationType = "redirects_to"
	// RelationObservedInSession records that a host appeared while a named
	// exploration step was running. It is what lets a component decision be
	// attributed to an action the user actually performed.
	RelationObservedInSession RelationType = "observed_in_session"
)

// KnownRelationType reports whether a relation type is one this build stores.
func KnownRelationType(value RelationType) bool {
	switch value {
	case RelationCNAMETo, RelationLoadedBy, RelationRedirectsTo, RelationObservedInSession:
		return true
	default:
		return false
	}
}

type Relation struct {
	SourceResource Resource
	RelationType   RelationType
	TargetResource Resource
	ServiceID      string
	ComponentID    string
	FirstSeen      time.Time
	LastSeen       time.Time
	ValidUntil     time.Time
	SourceID       string
	SourceRevision string
	Validity       ObservationValidity
}

func (r Relation) IsFresh(cutoff time.Time) bool {
	return r.SourceResource.IsValid() && r.TargetResource.IsValid() && r.Validity != ValidityInvalid && r.Validity != ValidityArchived && r.ValidUntil.After(cutoff)
}

func (r Relation) Fingerprint() string {
	parts := []string{r.SourceResource.Kind.String(), r.SourceResource.CanonicalValue(), string(r.RelationType), r.TargetResource.Kind.String(), r.TargetResource.CanonicalValue(), r.ServiceID, r.ComponentID, r.SourceID, r.SourceRevision}
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}

const StaleRetention = 90 * 24 * time.Hour

// LifecycleAt derives observation state from an explicit cutoff. Expiry is
// intentionally strict: equality is stale, and retention equality archives.
func LifecycleAt(validUntil, cutoff time.Time, invalid bool) ObservationValidity {
	if invalid || validUntil.IsZero() {
		return ValidityInvalid
	}
	validUntil = validUntil.UTC()
	cutoff = cutoff.UTC()
	if cutoff.Before(validUntil) {
		return ValidityValid
	}
	if !cutoff.Before(validUntil.Add(StaleRetention)) {
		return ValidityArchived
	}
	return ValidityStale
}
