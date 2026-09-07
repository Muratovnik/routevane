// Package httpfeed observes an official network feed over HTTPS. Every value
// leaves the package as a normalized domain observation; the transport, the
// destination policy, and the decoders are all bounded here because this
// package is the only consumer that sees the hostile response.
package httpfeed

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/netpolicy"
)

// SourceRevision changes when the observation semantics of this package
// change. It is part of the catalog source revision so stored observations from
// an older decoder are never mixed with a newer one.
const SourceRevision = "http-feed-v1"

const (
	// MaxBytes bounds the response body. A feed larger than this is refused
	// rather than truncated, because a truncated feed is silently incomplete.
	MaxBytes = 1 << 20
	// MaxEntries bounds how many addresses one feed may contribute.
	MaxEntries = 4096
	// MaxRedirects bounds redirect hops. Every hop is validated again.
	MaxRedirects = 3
	// DefaultValidity is how long one successful feed read stays fresh. Feeds
	// carry no per-entry TTL, so validity is a source-level decision.
	DefaultValidity = 24 * time.Hour

	// maxEntryBytes bounds one entry. A domain name is longer than an address,
	// and the bound is the DNS name limit rather than a guess.
	maxEntryBytes = 253

	dialTimeout      = 5 * time.Second
	handshakeTimeout = 5 * time.Second
	responseTimeout  = 10 * time.Second
)

var (
	ErrInvalidQuery        = errors.New("invalid feed query")
	ErrInvalidURL          = errors.New("feed URL is not an absolute HTTPS URL")
	ErrUnsafeDestination   = netpolicy.ErrUnsafeDestination
	ErrTooManyRedirects    = errors.New("feed exceeded the redirect limit")
	ErrFeedTooLarge        = errors.New("feed response exceeds the byte limit")
	ErrTooManyEntries      = errors.New("feed exceeds the entry limit")
	ErrUnsupportedFormat   = errors.New("unsupported feed format")
	ErrUnexpectedStatus    = errors.New("feed returned an unexpected status")
	ErrEmptyFeed           = errors.New("feed contained no usable entry")
	ErrMalformedFeed       = errors.New("feed body is malformed")
	ErrUnsupportedResponse = errors.New("feed returned an unsupported media type")
)

// Resolver resolves a feed host to its candidate addresses. The interface is
// declared here because this observer is the consumer that must inspect every
// candidate before a connection is attempted.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Dialer opens the connection to an address this package has already accepted.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Options carries the injectable parts of the transport. The zero value is the
// production configuration.
type Options struct {
	// Validity overrides DefaultValidity.
	Validity time.Duration
	// RootCAs overrides the system certificate pool. Tests use it to trust a
	// local test certificate without weakening verification.
	RootCAs *x509.CertPool
}

type Observer struct {
	client   *http.Client
	validity time.Duration
}

type Query struct {
	ListID         string
	ComponentID    string
	SourceID       string
	SourceRevision string
	URL            string
	Format         domain.FeedFormat
	// SourceClass is what the catalog says this feed is. A third-party list is
	// community curation, and filing it as official would tell the planner and
	// the diagnostics that a vendor published it about itself.
	SourceClass domain.SourceClass
}

type Result struct {
	Sightings []domain.Sighting
	// Skipped counts entries the decoder read but refused to record. A refused
	// entry never becomes an observation and never fails the whole cycle.
	Skipped int
}

// NewObserver builds an observer whose transport resolves and validates every
// destination itself. A nil resolver or dialer uses the standard library.
func NewObserver(resolver Resolver, dialer Dialer, options Options) *Observer {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: dialTimeout}
	}
	validity := options.Validity
	if validity <= 0 {
		validity = DefaultValidity
	}
	guard := &guardedDialer{resolver: resolver, dialer: dialer}
	transport := &http.Transport{
		// Proxy is deliberately nil: an environment proxy would move the
		// destination decision outside the validated address policy.
		Proxy:                 nil,
		DialContext:           guard.DialContext,
		DisableKeepAlives:     true,
		DisableCompression:    false,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          0,
		TLSHandshakeTimeout:   handshakeTimeout,
		ResponseHeaderTimeout: responseTimeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: options.RootCAs},
	}
	return &Observer{
		client: &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
			Jar:           nil,
		},
		validity: validity,
	}
}

// Observe reads one feed and returns normalized official observations. The
// caller owns the deadline; this method adds no unbounded wait of its own.
func (o *Observer) Observe(ctx context.Context, query Query, observedAt time.Time) (Result, error) {
	if o == nil || o.client == nil {
		return Result{}, ErrInvalidQuery
	}
	if ctx == nil || observedAt.IsZero() {
		return Result{}, ErrInvalidQuery
	}
	if domain.ValidateSlug(query.ListID) != nil || domain.ValidateSlug(query.ComponentID) != nil || domain.ValidateSlug(query.SourceID) != nil || query.SourceRevision == "" {
		return Result{}, ErrInvalidQuery
	}
	if err := ValidateURL(query.URL); err != nil {
		return Result{}, err
	}
	switch query.Format {
	case domain.FeedFormatText, domain.FeedFormatJSON, domain.FeedFormatDomainList:
	default:
		return Result{}, ErrUnsupportedFormat
	}
	sourceClass := query.SourceClass
	switch sourceClass {
	case domain.SourceOfficial, domain.SourceCommunity:
	case "":
		// An undeclared class is the vendor's own publication, which is what
		// every feed in the catalog was before the class existed.
		sourceClass = domain.SourceOfficial
	default:
		return Result{}, ErrInvalidQuery
	}
	body, err := o.fetch(ctx, query.URL)
	if err != nil {
		return Result{}, err
	}
	entries, skipped, err := decode(query.Format, body)
	if err != nil {
		return Result{}, err
	}
	if len(entries) == 0 {
		return Result{}, ErrEmptyFeed
	}
	observedAt = observedAt.UTC()
	validUntil := observedAt.Add(o.validity)
	sightings := make([]domain.Sighting, 0, len(entries))
	for _, resource := range entries {
		sightings = append(sightings, domain.Sighting{
			ListID:           query.ListID,
			ComponentID:      query.ComponentID,
			Resource:         resource,
			SourceID:         query.SourceID,
			SourceClass:      sourceClass,
			SourceRevision:   query.SourceRevision,
			FirstSeen:        observedAt,
			LastSeen:         observedAt,
			ValidUntil:       validUntil,
			TTLKnown:         false,
			ObservationCount: 1,
			Validity:         domain.ValidityValid,
		})
	}
	return Result{Sightings: sightings, Skipped: skipped}, nil
}

// ValidateURL accepts only an absolute HTTPS URL with no credentials. It is
// exported so the catalog boundary can reject a bad feed entry at load time
// instead of at the first refresh.
func ValidateURL(raw string) error {
	if raw == "" || len(raw) > 2048 || strings.TrimSpace(raw) != raw {
		return ErrInvalidURL
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return ErrInvalidURL
	}
	host := parsed.Hostname()
	if host == "" {
		return ErrInvalidURL
	}
	if address, addrErr := netip.ParseAddr(host); addrErr == nil {
		if !netpolicy.PublicUnicast(address) {
			return ErrUnsafeDestination
		}
		return nil
	}
	if _, err := domain.NormalizeDomain(host); err != nil {
		return ErrInvalidURL
	}
	return nil
}

func (o *Observer) fetch(ctx context.Context, target string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, ErrInvalidURL
	}
	request.Header.Set("Accept", "text/plain, application/json")
	request.Header.Set("User-Agent", "Routevane/1 (+local)")
	response, err := o.client.Do(request)
	if err != nil {
		return nil, unwrapTransportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, response.StatusCode)
	}
	// One extra byte distinguishes "exactly at the limit" from "truncated".
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read feed body: %w", err)
	}
	if len(body) > MaxBytes {
		return nil, ErrFeedTooLarge
	}
	return body, nil
}

func checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= MaxRedirects {
		return ErrTooManyRedirects
	}
	if err := ValidateURL(request.URL.String()); err != nil {
		return err
	}
	// A redirect must not carry the previous hop's headers to a new host.
	request.Header.Del("Authorization")
	request.Header.Del("Cookie")
	return nil
}

// guardedDialer resolves the host itself and refuses the whole connection when
// any candidate address is outside the public unicast policy. Rejecting the
// entire answer, rather than picking a safe address from a mixed answer, keeps a
// split DNS answer from being usable for rebinding.
type guardedDialer struct {
	resolver Resolver
	dialer   Dialer
}

func (g *guardedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, ErrUnsafeDestination
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrUnsafeDestination
	}
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		if !netpolicy.PublicUnicast(literal) {
			return nil, ErrUnsafeDestination
		}
		return g.dialer.DialContext(ctx, network, address)
	}
	candidates, err := g.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve feed host: %w", err)
	}
	if !netpolicy.AllPublicUnicast(candidates) {
		return nil, ErrUnsafeDestination
	}
	return g.dialer.DialContext(ctx, network, net.JoinHostPort(candidates[0].Unmap().String(), port))
}

func unwrapTransportError(err error) error {
	for _, sentinel := range []error{ErrUnsafeDestination, ErrTooManyRedirects, ErrInvalidURL} {
		if errors.Is(err, sentinel) {
			return sentinel
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("fetch feed: %w", context.DeadlineExceeded)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("fetch feed: %w", context.Canceled)
	}
	return fmt.Errorf("fetch feed: %w", errors.New(http.StatusText(http.StatusBadGateway)))
}
