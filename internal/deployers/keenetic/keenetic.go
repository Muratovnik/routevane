// Package keenetic deploys an already-validated Keenetic route artifact to a
// Keenetic router over its local RCI interface.
//
// The RCI authentication and command surface is documented by the vendor's
// community rather than by a vendor specification, so every request this package
// makes is narrow, explicit, and verifiable against the device's own answers:
// the deployer never assumes a command succeeded because it returned 200.
package keenetic

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/netpolicy"
	"github.com/Muratovnik/routevane/internal/planjson"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

// DeployerID is the deployer identity. It is keyed to the renderer whose
// artifacts it installs.
const DeployerID = keenetic.ID

const (
	// MinFirmware is the oldest firmware whose static-route surface this
	// deployer was written against. An older device is refused before anything
	// is changed rather than being probed for behaviour.
	MinFirmware = "5.0.4"
	// MaxResponseBytes bounds every device answer.
	MaxResponseBytes = 4 << 20
	// MaxRoutes bounds how many routes one deployment may install.
	MaxRoutes = keenetic.MaxLines

	requestTimeout   = 15 * time.Second
	dialTimeout      = 5 * time.Second
	handshakeTimeout = 5 * time.Second
)

var (
	ErrUnsupportedFirmware = errors.New("device firmware does not support this deployment")
	ErrDeviceRefused       = errors.New("device refused the request")
	ErrDeviceUnreachable   = errors.New("device is unreachable")
	ErrDeviceAnswer        = errors.New("device answer is not usable")
	ErrArtifactMismatch    = errors.New("artifact is not a Keenetic route artifact")
)

// Dialer opens the connection to a destination the device policy accepted.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Options carries the injectable parts of the transport. The zero value is the
// production configuration.
type Options struct {
	// Dialer is the connection seam. Nil uses the standard library.
	Dialer Dialer
	// InsecureDeviceTLS allows a device's own self-signed certificate on a
	// private address. A router has no publicly trusted certificate for its LAN
	// address, so this is opt-in per deployment and never applies to any other
	// boundary in this product.
	InsecureDeviceTLS bool
}

// Deployer installs Keenetic route artifacts over RCI.
type Deployer struct {
	options Options
}

func New(options Options) *Deployer { return &Deployer{options: options} }

func (*Deployer) ID() string { return DeployerID }

// Probe authenticates and reports what the device is.
//
// An unsupported firmware yields an empty FormatKey rather than an error, so
// the caller refuses the deployment on compatibility grounds with the version it
// actually found.
func (d *Deployer) Probe(ctx context.Context, connection application.Connection) (application.DeviceInfo, error) {
	session, err := d.connect(ctx, connection)
	if err != nil {
		return application.DeviceInfo{}, err
	}
	info, err := session.identity(ctx, connection, DeployerID)
	if err != nil {
		return info, err
	}
	if !SupportedFirmware(info.FirmwareVersion) {
		// Deliberately not an error: the caller reports the incompatibility
		// with the version it found, which is more useful than a transport
		// failure.
		return info, nil
	}
	if err := session.requireInterface(ctx, info.Interface); err != nil {
		return info, err
	}
	info.FormatKey = keenetic.Version
	return info, nil
}

// identity reads what the device is. It decides no compatibility of its own:
// which firmware is enough depends on the artifact format, and this router
// serves two of them.
func (s *session) identity(ctx context.Context, connection application.Connection, deployerID string) (application.DeviceInfo, error) {
	var version struct {
		Release      string `json:"release"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		Model        string `json:"model"`
		Device       string `json:"device"`
		Manufacturer string `json:"manufacturer"`
	}
	if err := s.command(ctx, "/rci/show/version", nil, &version); err != nil {
		return application.DeviceInfo{}, err
	}
	firmware := strings.TrimSpace(version.Release)
	if firmware == "" {
		firmware = strings.TrimSpace(version.Title)
	}
	model := strings.TrimSpace(version.Model)
	if model == "" {
		model = strings.TrimSpace(version.Device)
	}
	vendor := strings.TrimSpace(version.Manufacturer)
	if vendor == "" {
		vendor = "Keenetic"
	}
	info := application.DeviceInfo{
		DeployerID:      deployerID,
		Vendor:          vendor,
		Model:           model,
		FirmwareVersion: firmware,
		Interface:       strings.TrimSpace(connection.Interface),
	}
	if info.Interface == "" {
		return info, fmt.Errorf("%w: no route interface was named", ErrDeviceAnswer)
	}
	return info, nil
}

// requireInterface refuses a name the device does not have, before anything is
// written to it.
func (s *session) requireInterface(ctx context.Context, name string) error {
	var interfaces map[string]json.RawMessage
	if err := s.command(ctx, "/rci/show/interface", nil, &interfaces); err != nil {
		return err
	}
	if _, present := interfaces[name]; !present {
		return fmt.Errorf("%w: the device has no interface %q", ErrDeviceAnswer, name)
	}
	return nil
}

// SupportedFirmware reports whether a reported version is at least MinFirmware.
// A version this deployer cannot parse is unsupported: guessing would risk
// writing a route surface the firmware does not have.
func SupportedFirmware(version string) bool { return atLeast(version, MinFirmware) }

// atLeast compares two reported versions rather than their strings, so "5.10"
// is newer than "5.9" and an unparsable value is never newer than anything.
func atLeast(version, minimum string) bool {
	want, ok := parseVersion(minimum)
	if !ok {
		return false
	}
	got, ok := parseVersion(version)
	if !ok {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return got[index] > want[index]
		}
	}
	return true
}

func parseVersion(value string) ([3]int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return [3]int{}, false
	}
	// A firmware string may carry a suffix such as "5.0.4-1" or "5.1.2 (AAAA)".
	for index, char := range value {
		if (char < '0' || char > '9') && char != '.' {
			value = value[:index]
			break
		}
	}
	parts := strings.Split(strings.Trim(value, "."), ".")
	if len(parts) == 0 || len(parts) > 3 {
		return [3]int{}, false
	}
	parsed := [3]int{}
	for index, part := range parts {
		if part == "" || len(part) > 6 {
			return [3]int{}, false
		}
		number := 0
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return [3]int{}, false
			}
			number = number*10 + int(digit-'0')
		}
		parsed[index] = number
	}
	return parsed, true
}

// Backup fetches the device's running configuration.
func (d *Deployer) Backup(ctx context.Context, device application.DeviceInfo, connection application.Connection) (application.BackupPayload, error) {
	session, err := d.connect(ctx, connection)
	if err != nil {
		return application.BackupPayload{}, err
	}
	payload, err := session.raw(ctx, http.MethodGet, "/ci/startup-config", nil)
	if err != nil {
		return application.BackupPayload{}, err
	}
	if len(payload) == 0 {
		return application.BackupPayload{}, fmt.Errorf("%w: the configuration backup was empty", ErrDeviceAnswer)
	}
	return application.BackupPayload{Payload: payload}, nil
}

// Deploy is the fail-safe path used when no ownership repository is available.
// It adds missing desired routes and never infers deletion authority from
// interface membership.
func (d *Deployer) Deploy(ctx context.Context, device application.DeviceInfo, connection application.Connection, artifact application.DeployArtifact) error {
	desired, err := d.DesiredManagedRoutes(artifact)
	if err != nil {
		return err
	}
	current, err := d.CurrentManagedRoutes(ctx, device, connection)
	if err != nil {
		return err
	}
	present := make(map[netip.Prefix]struct{}, len(current))
	for _, route := range current {
		present[route.Prefix] = struct{}{}
	}
	mutation := application.ManagedRouteMutation{}
	for _, route := range desired {
		if _, exists := present[route.Prefix]; exists {
			continue
		}
		mutation.Upsert = append(mutation.Upsert, route)
	}
	return d.ApplyManagedRoutes(ctx, device, connection, mutation)
}

// ManagedRouteScope canonicalizes the durable ownership identity without
// carrying a credential or a registration-local device ID.
func (d *Deployer) ManagedRouteScope(target domain.TargetDefinition, connection application.Connection) (application.ManagedRouteScope, error) {
	if target.ID == "" || target.RendererID != DeployerID {
		return application.ManagedRouteScope{}, fmt.Errorf("%w: target does not use this deployer", ErrDeviceRefused)
	}
	if err := d.ValidateStoredConnection(connection.Redacted()); err != nil {
		return application.ManagedRouteScope{}, err
	}
	endpoint, err := netpolicy.ValidateDeviceURL(connection.URL)
	if err != nil {
		return application.ManagedRouteScope{}, err
	}
	return application.ManagedRouteScope{
		Endpoint: strings.TrimSuffix(endpoint.String(), "/"), TargetID: target.ID,
		Interface: strings.TrimSpace(connection.Interface),
	}, nil
}

func (*Deployer) DesiredManagedRoutes(artifact application.DeployArtifact) ([]application.ManagedRouteSpec, error) {
	prefixes, err := artifactRouteSpecs(artifact)
	if err != nil {
		return nil, err
	}
	return prefixes, nil
}

func (d *Deployer) CurrentManagedRoutes(ctx context.Context, device application.DeviceInfo, connection application.Connection) ([]application.ManagedRouteSpec, error) {
	session, err := d.connect(ctx, connection)
	if err != nil {
		return nil, err
	}
	existing, err := session.ownedRoutes(ctx, device.Interface)
	if err != nil {
		return nil, err
	}
	result := make([]application.ManagedRouteSpec, 0, len(existing))
	for prefix, description := range existing {
		result = append(result, application.ManagedRouteSpec{Prefix: prefix, Description: description})
	}
	slices.SortFunc(result, func(a, b application.ManagedRouteSpec) int { return cmp.Compare(a.Prefix.String(), b.Prefix.String()) })
	return result, nil
}

// ApplyManagedRoutes applies only the exact additions and deletions authorized
// by the application-owned ledger.
func (d *Deployer) ApplyManagedRoutes(ctx context.Context, device application.DeviceInfo, connection application.Connection, mutation application.ManagedRouteMutation) error {
	seen := make(map[netip.Prefix]bool, len(mutation.Upsert)+len(mutation.Remove))
	commands := make([]any, 0, len(mutation.Upsert)+len(mutation.Remove))
	for _, prefix := range mutation.Remove {
		if !prefix.IsValid() || !prefix.Addr().Is4() || prefix != prefix.Masked() || seen[prefix] {
			return fmt.Errorf("%w: invalid managed route removal", ErrDeviceRefused)
		}
		seen[prefix] = true
		commands = append(commands, routeCommand(prefix, device.Interface, "", true))
	}
	for _, route := range mutation.Upsert {
		prefix := route.Prefix
		if !prefix.IsValid() || !prefix.Addr().Is4() || prefix != prefix.Masked() || seen[prefix] || !validRouteComment(route.Description) {
			return fmt.Errorf("%w: invalid managed route addition", ErrDeviceRefused)
		}
		seen[prefix] = true
		commands = append(commands, routeCommand(prefix, device.Interface, route.Description, false))
	}
	if len(commands) == 0 {
		return nil
	}
	session, err := d.connect(ctx, connection)
	if err != nil {
		return err
	}
	sortCommands(commands)
	if err := session.batch(ctx, commands); err != nil {
		return err
	}
	return session.command(ctx, "/rci/system/configuration/save", map[string]any{}, nil)
}

func validRouteComment(comment string) bool {
	if !utf8.ValidString(comment) || len(comment) > application.MaxManagedRouteDescriptionBytes {
		return false
	}
	for _, r := range comment {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Verify is the no-ledger verification path. Desired routes must be present,
// but unrelated routes are accepted because their ownership is unknown.
func (d *Deployer) Verify(ctx context.Context, device application.DeviceInfo, connection application.Connection, artifact application.DeployArtifact) error {
	prefixes, err := artifactRouteSpecs(artifact)
	if err != nil {
		return err
	}
	session, err := d.connect(ctx, connection)
	if err != nil {
		return err
	}
	existing, err := session.ownedRoutes(ctx, device.Interface)
	if err != nil {
		return err
	}
	missing := make([]string, 0)
	for _, route := range prefixes {
		if _, present := existing[route.Prefix]; !present {
			missing = append(missing, route.Prefix.String())
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	return fmt.Errorf("%w: %d missing (missing=%s)", ErrDeviceAnswer, len(missing), strings.Join(missing, ","))
}

// Rollback restores the configuration captured before the deployment.
func (d *Deployer) Rollback(ctx context.Context, device application.DeviceInfo, connection application.Connection, backup application.BackupPayload) error {
	if len(backup.Payload) == 0 {
		return fmt.Errorf("%w: no backup to restore", ErrDeviceAnswer)
	}
	session, err := d.connect(ctx, connection)
	if err != nil {
		return err
	}
	if _, err := session.raw(ctx, http.MethodPost, "/ci/startup-config", backup.Payload); err != nil {
		return err
	}
	return session.command(ctx, "/rci/system/configuration/save", map[string]any{}, nil)
}

func routeCommand(prefix netip.Prefix, deviceInterface, description string, remove bool) map[string]any {
	route := map[string]any{
		"network":   prefix.Addr().String(),
		"mask":      maskOf(prefix),
		"interface": deviceInterface,
		"auto":      false,
	}
	if remove {
		route["no"] = true
	} else {
		route["comment"] = description
	}
	return map[string]any{"ip": map[string]any{"route": route}}
}

func maskOf(prefix netip.Prefix) string {
	mask := net.CIDRMask(prefix.Bits(), 32)
	return net.IP(mask).String()
}

func sortCommands(commands []any) {
	slices.SortFunc(commands, func(a, b any) int {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return cmp.Compare(string(left), string(right))
	})
}

// artifactRouteSpecs parses prefixes with the renderer's own validator and
// joins them to immutable snapshot provenance. The BAT bytes remain unchanged.
func artifactRouteSpecs(artifact application.DeployArtifact) ([]application.ManagedRouteSpec, error) {
	if artifact.RendererID != keenetic.ID {
		return nil, fmt.Errorf("%w: renderer %q", ErrArtifactMismatch, artifact.RendererID)
	}
	rules, err := keenetic.Parse(artifact.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactMismatch, err)
	}
	if len(rules) == 0 || len(rules) > MaxRoutes {
		return nil, fmt.Errorf("%w: %d routes", ErrArtifactMismatch, len(rules))
	}
	prefixes := make(map[netip.Prefix][]string, len(rules))
	for _, rule := range rules {
		prefixes[rule.Prefix.Masked()] = nil
	}
	if len(artifact.PlanSnapshot) != 0 {
		if err := planjson.Validate(artifact.PlanSnapshot); err != nil {
			return nil, fmt.Errorf("%w: plan snapshot: %v", ErrArtifactMismatch, err)
		}
		var snapshot planjson.Plan
		if err := json.Unmarshal(artifact.PlanSnapshot, &snapshot); err != nil {
			return nil, fmt.Errorf("%w: plan snapshot: %v", ErrArtifactMismatch, err)
		}
		if artifact.RoutingPlanHash == "" || snapshot.SemanticHash != artifact.RoutingPlanHash || snapshot.TargetID != "keenetic" || snapshot.FormatKey != keenetic.Version {
			return nil, fmt.Errorf("%w: plan snapshot hash", ErrArtifactMismatch)
		}
		for _, rule := range snapshot.Rules {
			stableLabels := domain.StableStrings(rule.Labels)
			if !slices.Equal(stableLabels, rule.Labels) || (len(stableLabels) > 0 && application.CompactManagedRouteDescription(stableLabels) == "") {
				return nil, fmt.Errorf("%w: non-canonical route labels", ErrArtifactMismatch)
			}
			prefix, ok := snapshotRulePrefix(rule)
			if !ok {
				continue
			}
			if _, rendered := prefixes[prefix]; rendered {
				prefixes[prefix] = append(prefixes[prefix], stableLabels...)
			}
		}
	}
	result := make([]application.ManagedRouteSpec, 0, len(prefixes))
	for prefix, labels := range prefixes {
		labels = domain.StableStrings(labels)
		result = append(result, application.ManagedRouteSpec{Prefix: prefix, Labels: labels, Description: application.CompactManagedRouteDescription(labels)})
	}
	slices.SortFunc(result, func(a, b application.ManagedRouteSpec) int { return cmp.Compare(a.Prefix.String(), b.Prefix.String()) })
	return result, nil
}

func snapshotRulePrefix(rule planjson.Rule) (netip.Prefix, bool) {
	switch domain.RuleKind(rule.Kind) {
	case domain.RuleIPv4:
		address, err := netip.ParseAddr(rule.Value)
		if err != nil || !address.Is4() {
			return netip.Prefix{}, false
		}
		return netip.PrefixFrom(address, 32), true
	case domain.RulePrefix4:
		prefix, err := netip.ParsePrefix(rule.Value)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return netip.Prefix{}, false
		}
		return prefix, true
	default:
		return netip.Prefix{}, false
	}
}

// Requirements describes the RCI interface: an address on the local network, an
// account, its password, and the interface routes attach to.
func (*Deployer) Requirements() application.ConnectionRequirements {
	return application.ConnectionRequirements{
		AddressLabel:    "Адрес устройства",
		AddressExample:  "http://192.168.1.1",
		NeedsCredential: true,
		NeedsInterface:  true,
		InterfaceLabel:  "Интерфейс устройства",
	}
}

// ValidateConnection refuses a connection this transport cannot authenticate
// with. The RCI interface is an authenticated device surface, so an account and
// a password are required and the address must satisfy the device destination
// policy.
func (d *Deployer) ValidateConnection(connection application.Connection) error {
	stored := connection.Redacted()
	if err := d.ValidateStoredConnection(stored); err != nil {
		return err
	}
	if connection.Password == "" {
		return fmt.Errorf("%w: device credentials are required", ErrDeviceRefused)
	}
	return nil
}

// ValidateStoredConnection applies the device destination policy to metadata
// before it is persisted, and refuses a password at that boundary.
func (*Deployer) ValidateStoredConnection(connection application.Connection) error {
	if _, err := netpolicy.ValidateDeviceURL(connection.URL); err != nil {
		return err
	}
	if connection.Username == "" {
		return fmt.Errorf("%w: a device account is required", ErrDeviceRefused)
	}
	if connection.Password != "" {
		return fmt.Errorf("%w: a password must not be persisted", ErrDeviceRefused)
	}
	if connection.Interface == "" {
		return fmt.Errorf("%w: a device interface is required", ErrDeviceRefused)
	}
	return nil
}
