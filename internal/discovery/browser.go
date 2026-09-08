package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

var (
	ErrBrowserUnavailable = errors.New("discovery browser executable is not configured")
	ErrPageLoadFailed     = errors.New("discovery page load failed")
)

// Session bounds. A discovery run is a single managed page load, not a crawl.
const (
	DefaultSessionTimeout = 30 * time.Second
	DefaultMaxRequests    = 512
	DefaultMaxHosts       = 128
	DefaultMaxBytes       = 32 << 20
	settleDelay           = 2 * time.Second
)

// profileCleanupAttempts and profileCleanupBackoff bound how hard a discovery
// run retries removing its temporary profile before it gives up. On Windows
// the browser process has usually just exited when cleanup runs, and can
// still briefly hold a file lock on its own profile; a short, bounded retry
// absorbs that instead of turning a transient lock into a permanent failure.
const (
	profileCleanupAttempts = 5
	profileCleanupBackoff  = 50 * time.Millisecond
)

// removeProfileDir removes a discovery session's temporary profile, retrying a
// failed attempt a bounded number of times with a short backoff. A failure
// that persists across every attempt is returned rather than swallowed, so the
// caller can report it instead of silently leaving browsing state on disk.
func removeProfileDir(dir string) error {
	var lastErr error
	for attempt := 1; attempt <= profileCleanupAttempts; attempt++ {
		lastErr = os.RemoveAll(dir)
		if lastErr == nil {
			return nil
		}
		if attempt < profileCleanupAttempts {
			time.Sleep(profileCleanupBackoff * time.Duration(attempt))
		}
	}
	return fmt.Errorf("remove discovery profile %s: %w", dir, lastErr)
}

// BrowserOptions carries the injectable parts of one managed page load. The
// zero value is invalid: an explicit executable path keeps the browser an owned
// dependency rather than something discovered from the environment.
type BrowserOptions struct {
	// ExecPath is the Chromium-family executable to run.
	ExecPath string
	// UserDataParent is where the isolated temporary profile is created. Empty
	// uses the operating system temporary directory.
	UserDataParent string
	// Timeout bounds the whole session.
	Timeout time.Duration
	// MaxRequests, MaxHosts, and MaxBytes bound the session volume.
	MaxRequests int
	MaxHosts    int
	MaxBytes    int64
	// Resolver and Dialer are the proxy's seams. Nil uses the standard library.
	Resolver Resolver
	Dialer   Dialer
	// HostResolverRules is passed to the browser unchanged. Tests use it to send
	// a public-looking hostname to a local test server without weakening the
	// destination policy, which still sees the hostname and its resolved
	// address.
	HostResolverRules string
	// TrustedSPKI pins additional server public keys by base64 SHA-256 of their
	// SubjectPublicKeyInfo. It trusts exactly those keys and nothing else, so it
	// is a pin rather than a way to disable certificate verification.
	TrustedSPKI []string
}

// PageLoad is everything one managed page load observed. It contains hostnames
// only: addresses stay in the observation store, never in a draft.
type PageLoad struct {
	FinalURL string
	Hosts    []string
	Blocked  map[string]string
	Requests int
	Bytes    int64
	// ProfileDir is the temporary profile that was used and removed. It is
	// reported so a caller can assert that nothing was left behind.
	ProfileDir string
	// CleanupError reports that removing ProfileDir failed even after the
	// bounded retry, so the caller can surface it instead of the failure being
	// silently dropped. It never replaces the operation's own error: a load
	// failure is reported through the returned error exactly as before, and a
	// cleanup failure is only ever visible here.
	CleanupError string
}

// LoadPage performs exactly one managed page load in an isolated temporary
// profile and returns the hosts the page contacted.
//
// Browser HTTP traffic passes through an in-process proxy that applies the
// shared destination policy, without a loopback bypass. The isolated profile
// also disables WebRTC's non-proxied UDP. The temporary profile is removed on
// success, failure, and cancellation.
func LoadPage(ctx context.Context, target Target, options BrowserOptions) (result PageLoad, err error) {
	if ctx == nil || target.URL == "" {
		return PageLoad{}, ErrInvalidTarget
	}
	if options.ExecPath == "" {
		return PageLoad{}, ErrBrowserUnavailable
	}
	if info, err := os.Stat(options.ExecPath); err != nil || !info.Mode().IsRegular() {
		return PageLoad{}, fmt.Errorf("%w: %s", ErrBrowserUnavailable, options.ExecPath)
	}
	options = options.withDefaults()

	profileDir, err := os.MkdirTemp(options.UserDataParent, "routevane-discovery-")
	if err != nil {
		return PageLoad{}, fmt.Errorf("create discovery profile: %w", err)
	}
	// The profile is removed on every path, including a panic in the browser
	// driver, so a cancelled session cannot leave browsing state on disk. A
	// removal failure is reported on the result rather than dropped: it never
	// overwrites the operation's own error.
	defer func() {
		if cleanupErr := removeProfileDir(profileDir); cleanupErr != nil {
			result.CleanupError = cleanupErr.Error()
		}
	}()

	sessionCtx, cancelSession := context.WithTimeout(ctx, options.Timeout)
	defer cancelSession()

	proxy := newGuardedProxy(options.Resolver, options.Dialer, options.MaxRequests, options.MaxHosts, options.MaxBytes)
	proxyAddress, stopProxy, err := proxy.listenLoopback(sessionCtx)
	if err != nil {
		return PageLoad{}, err
	}
	defer stopProxy()

	allocatorCtx, cancelAllocator, err := newBrowserAllocator(sessionCtx, options, profileDir, proxyAddress)
	if err != nil {
		return PageLoad{}, err
	}
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	var finalURL string
	runErr := chromedp.Run(browserCtx,
		chromedp.Navigate(target.URL),
		// One settle delay, not a crawl: subresources a page requests during
		// load are what the draft is derived from.
		chromedp.Sleep(settleDelay),
		chromedp.Location(&finalURL),
	)
	hosts, blocked := proxy.observed()
	result = PageLoad{
		FinalURL:   finalURL,
		Hosts:      hosts,
		Blocked:    blocked,
		Requests:   int(proxy.requests.Load()),
		Bytes:      proxy.bytes.Load(),
		ProfileDir: profileDir,
	}
	if runErr != nil {
		return result, fmt.Errorf("%w: %v", ErrPageLoadFailed, runErr)
	}
	return result, nil
}

func (options BrowserOptions) withDefaults() BrowserOptions {
	if options.Timeout <= 0 {
		options.Timeout = DefaultSessionTimeout
	}
	if options.MaxRequests <= 0 {
		options.MaxRequests = DefaultMaxRequests
	}
	if options.MaxHosts <= 0 {
		options.MaxHosts = DefaultMaxHosts
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = DefaultMaxBytes
	}
	if options.Resolver == nil {
		options.Resolver = net.DefaultResolver
	}
	if options.Dialer == nil {
		options.Dialer = &net.Dialer{Timeout: 5 * time.Second}
	}
	return options
}

// DefaultBrowserPath reports the Chromium-family executable a local setup
// already owns, or an empty string when none is configured. It reads only the
// explicit environment variable: a discovery run must never pick up an
// arbitrary browser from the search path.
func DefaultBrowserPath() string {
	path := os.Getenv("ROUTEVANE_BROWSER")
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if info, err := os.Stat(cleaned); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return cleaned
}

// newBrowserAllocator prepares the isolated profile before Chromium can start.
// Chromium reads its WebRTC routing policy from this preference. The similarly
// named command-line override alone is ineffective in Chromium 153.
func newBrowserAllocator(ctx context.Context, options BrowserOptions, profileDir, proxyAddress string) (context.Context, context.CancelFunc, error) {
	defaultProfile := filepath.Join(profileDir, "Default")
	if err := os.Mkdir(defaultProfile, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create discovery browser preferences directory: %w", err)
	}
	preferences := []byte(`{"webrtc":{"ip_handling_policy":"disable_non_proxied_udp"}}`)
	if err := os.WriteFile(filepath.Join(defaultProfile, "Preferences"), preferences, 0o600); err != nil {
		return nil, nil, fmt.Errorf("write discovery browser preferences: %w", err)
	}
	allocatorCtx, cancel := chromedp.NewExecAllocator(ctx, browserAllocatorOptions(options, profileDir, proxyAddress)...)
	return allocatorCtx, cancel, nil
}

// browserAllocatorOptions is the single browser configuration both the one-page
// load and the guided session use, so the two cannot drift apart in isolation,
// proxying, or background-traffic suppression.
func browserAllocatorOptions(options BrowserOptions, profileDir, proxyAddress string) []chromedp.ExecAllocatorOption {
	// The maintained default set is the baseline; the additions below are the
	// ones this package depends on rather than a rewrite of it.
	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.ExecPath(options.ExecPath),
		chromedp.UserDataDir(profileDir),
		chromedp.Headless,
		chromedp.DisableGPU,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("proxy-server", "http://"+proxyAddress),
		// An empty bypass list with the loopback exception removed forces even
		// localhost through the guarded proxy.
		chromedp.Flag("proxy-bypass-list", "<-loopback>"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("no-service-autorun", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
		chromedp.Flag("disable-client-side-phishing-detection", true),
		chromedp.Flag("metrics-recording-only", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("disable-domain-reliability", true),
		chromedp.Flag("disable-breakpad", true),
		chromedp.Flag("no-pings", true),
		chromedp.Flag("disable-component-extensions-with-background-pages", true),
		chromedp.Flag("safebrowsing-disable-auto-update", true),
		chromedp.Flag("disable-features", "Translate,OptimizationHints,MediaRouter,DialMediaRouteProvider,InterestFeedContentSuggestions,AutofillServerCommunication,CalculateNativeWinOcclusion"),
	)
	if options.HostResolverRules != "" {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("host-resolver-rules", options.HostResolverRules))
	}
	if len(options.TrustedSPKI) > 0 {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("ignore-certificate-errors-spki-list", strings.Join(options.TrustedSPKI, ",")))
	}
	return allocatorOptions
}
