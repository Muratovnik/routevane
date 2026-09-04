package keenetic

import (
	"bytes"
	"context"
	// #nosec G501 -- the device challenge scheme this deployer speaks is defined
	// with MD5 by the peer. It is a wire-format transformation, never a password
	// store or a security decision of ours; see docs/adr/0008.
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/netpolicy"
)

// session is one authenticated RCI conversation.
type session struct {
	client  *http.Client
	baseURL string
}

func (d *Deployer) connect(ctx context.Context, connection application.Connection) (*session, error) {
	if err := d.ValidateConnection(connection); err != nil {
		return nil, err
	}
	parsed, err := netpolicy.ValidateDeviceURL(connection.URL)
	if err != nil {
		return nil, err
	}
	dialer := d.options.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: dialTimeout}
	}
	guard := &deviceDialer{dialer: dialer}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           guard.DialContext,
		DisableKeepAlives:     false,
		TLSHandshakeTimeout:   handshakeTimeout,
		ResponseHeaderTimeout: requestTimeout,
		ForceAttemptHTTP2:     false,
	}
	if d.options.InsecureDeviceTLS {
		transport.TLSClientConfig = insecureDeviceTLSConfig()
	}
	jar := &cookieJar{}
	client := &http.Client{Transport: transport, Jar: jar, CheckRedirect: refuseRedirect, Timeout: requestTimeout}
	current := &session{client: client, baseURL: strings.TrimSuffix(parsed.String(), "/")}
	if err := current.authenticate(ctx, connection.Username, connection.Password); err != nil {
		return nil, err
	}
	return current, nil
}

// authenticate performs the challenge exchange the device expects. The password
// never leaves this function in cleartext and never appears in an error.
func (s *session) authenticate(ctx context.Context, username, password string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/auth", nil)
	if err != nil {
		return fmt.Errorf("%w: build challenge request", ErrDeviceUnreachable)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeviceUnreachable, err)
	}
	challenge := response.Header.Get("X-NDM-Challenge")
	realm := response.Header.Get("X-NDM-Realm")
	status := response.StatusCode
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, MaxResponseBytes))
	_ = response.Body.Close()
	if status == http.StatusOK {
		// An already-authenticated session is acceptable, which is what makes a
		// repeated deployment cheap.
		return nil
	}
	if status != http.StatusUnauthorized || challenge == "" || realm == "" {
		return fmt.Errorf("%w: challenge status %d", ErrDeviceRefused, status)
	}
	digest := md5.Sum([]byte(username + ":" + realm + ":" + password)) // #nosec G401 -- the device's documented challenge scheme fixes this construction; it is not a password store.
	inner := hex.EncodeToString(digest[:])
	outer := sha256.Sum256([]byte(challenge + inner))
	body, err := json.Marshal(map[string]string{"login": username, "password": hex.EncodeToString(outer[:])})
	if err != nil {
		return fmt.Errorf("%w: encode credentials", ErrDeviceRefused)
	}
	authRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/auth", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: build authentication request", ErrDeviceUnreachable)
	}
	authRequest.Header.Set("Content-Type", "application/json")
	authResponse, err := s.client.Do(authRequest)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeviceUnreachable, err)
	}
	authStatus := authResponse.StatusCode
	_, _ = io.Copy(io.Discard, io.LimitReader(authResponse.Body, MaxResponseBytes))
	_ = authResponse.Body.Close()
	if authStatus != http.StatusOK {
		return fmt.Errorf("%w: authentication status %d", ErrDeviceRefused, authStatus)
	}
	return nil
}

// command issues one RCI call. A nil body is a GET.
func (s *session) command(ctx context.Context, path string, body any, out any) error {
	method := http.MethodGet
	var encoded []byte
	if body != nil {
		method = http.MethodPost
		marshalled, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%w: encode command", ErrDeviceRefused)
		}
		encoded = marshalled
	}
	payload, err := s.raw(ctx, method, path, encoded)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%w: %s did not return usable JSON", ErrDeviceAnswer, path)
	}
	return nil
}

// batch issues several RCI commands in one call and refuses the whole batch if
// the device reports an error for any of them.
func (s *session) batch(ctx context.Context, commands []any) error {
	encoded, err := json.Marshal(commands)
	if err != nil {
		return fmt.Errorf("%w: encode batch", ErrDeviceRefused)
	}
	payload, err := s.raw(ctx, http.MethodPost, "/rci/", encoded)
	if err != nil {
		return err
	}
	// The device answers 200 even for a rejected command, so the answer body is
	// what decides success.
	var answers []json.RawMessage
	if err := json.Unmarshal(payload, &answers); err != nil {
		var single json.RawMessage
		if err := json.Unmarshal(payload, &single); err != nil {
			return fmt.Errorf("%w: batch answer is not JSON", ErrDeviceAnswer)
		}
		answers = []json.RawMessage{single}
	}
	for _, answer := range answers {
		if err := deviceError(answer); err != nil {
			return err
		}
	}
	return nil
}

// deviceError reports a status message the device marked as an error.
func deviceError(answer json.RawMessage) error {
	var envelope struct {
		Status []struct {
			Status  string `json:"status"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"status"`
	}
	if err := json.Unmarshal(answer, &envelope); err != nil {
		return nil
	}
	for _, status := range envelope.Status {
		if strings.EqualFold(status.Status, "error") {
			return fmt.Errorf("%w: %s %s", ErrDeviceRefused, status.Code, status.Message)
		}
	}
	return nil
}

// ownedRoutes is the historical name for reading every usable static route on
// one interface. The result is observation, not ownership: deletion authority
// comes only from the persisted exact-route ledger in the application layer.
func (s *session) ownedRoutes(ctx context.Context, deviceInterface string) (map[netip.Prefix]struct{}, error) {
	var configured struct {
		Route []struct {
			Network   string `json:"network"`
			Mask      string `json:"mask"`
			Interface string `json:"interface"`
			Host      string `json:"host"`
		} `json:"route"`
	}
	if err := s.command(ctx, "/rci/show/sc/ip/route", nil, &configured); err != nil {
		return nil, err
	}
	routes := make(map[netip.Prefix]struct{}, len(configured.Route))
	for _, route := range configured.Route {
		if route.Interface != deviceInterface || route.Host != "" {
			continue
		}
		prefix, ok := prefixOf(route.Network, route.Mask)
		if !ok {
			continue
		}
		routes[prefix] = struct{}{}
	}
	return routes, nil
}

func prefixOf(network, mask string) (netip.Prefix, bool) {
	address, err := netip.ParseAddr(strings.TrimSpace(network))
	if err != nil || !address.Is4() {
		return netip.Prefix{}, false
	}
	maskAddress, err := netip.ParseAddr(strings.TrimSpace(mask))
	if err != nil || !maskAddress.Is4() {
		return netip.Prefix{}, false
	}
	bytes := maskAddress.As4()
	ones, bits := net.IPMask(bytes[:]).Size()
	if bits != 32 || ones <= 0 {
		return netip.Prefix{}, false
	}
	prefix := netip.PrefixFrom(address, ones)
	if prefix.Masked() != prefix {
		return netip.Prefix{}, false
	}
	return prefix, true
}

// raw performs one bounded request and returns its body.
func (s *session) raw(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("%w: build %s", ErrDeviceUnreachable, path)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDeviceUnreachable, path)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read %s", ErrDeviceUnreachable, path)
	}
	if len(payload) > MaxResponseBytes {
		return nil, fmt.Errorf("%w: %s answered beyond the byte bound", ErrDeviceAnswer, path)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s answered %d", ErrDeviceRefused, path, response.StatusCode)
	}
	return payload, nil
}

// deviceDialer refuses any destination that is not a local network device, so a
// redirect or a DNS answer cannot move a deployment to a public host.
type deviceDialer struct{ dialer Dialer }

func (d *deviceDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, netpolicy.ErrNotADeviceDestination
	}
	literal, err := netip.ParseAddr(host)
	if err != nil || !netpolicy.DeviceDestination(literal) {
		return nil, netpolicy.ErrNotADeviceDestination
	}
	return d.dialer.DialContext(ctx, network, address)
}

// refuseRedirect stops the device conversation from being moved elsewhere.
func refuseRedirect(*http.Request, []*http.Request) error {
	return fmt.Errorf("%w: the device redirected the request", ErrDeviceRefused)
}

// cookieJar keeps the device session cookie for the lifetime of one deployment
// step. It is deliberately minimal: nothing is persisted, nothing is shared
// between devices, and no cookie outlives the step that obtained it.
type cookieJar struct {
	mu      sync.Mutex
	cookies []*http.Cookie
}

func (j *cookieJar) SetCookies(_ *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cookies = append(j.cookies, cookies...)
}

func (j *cookieJar) Cookies(*url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]*http.Cookie(nil), j.cookies...)
}

// insecureDeviceTLSConfig trusts the certificate a router presents for its own
// private address. A LAN address cannot have a publicly trusted certificate, so
// this is the only boundary in the product where verification is relaxed, it is
// opt-in per deployment, and the destination policy still refuses anything that
// is not a local device.
func insecureDeviceTLSConfig() *tls.Config {
	return &tls.Config{
		// #nosec G402 -- a router's LAN certificate cannot be publicly trusted.
		// This applies only to an explicitly opted-in deployment to a private
		// address that the device destination policy already accepted.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
}
