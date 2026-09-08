package keenetic

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

// FQDNDeployerID installs the FQDN object-group artifact. It is keyed to that
// renderer, as every deployer identity is.
const FQDNDeployerID = keeneticdns.ID

const (
	// MinFQDNFirmware is the oldest firmware with DNS-based routes. KeeneticOS
	// 5.0 introduced the feature and 5.0.1 is the oldest release the published
	// client tooling accepts, so it is the floor used here.
	MinFQDNFirmware = "5.0.1"
	// MaxFQDNEntries bounds how many names and addresses one deployment may
	// install, matching the target definition's rule budget.
	MaxFQDNEntries = keeneticdns.MaxGroups * keeneticdns.MaxEntriesPerGroup
	// maxParseCommands bounds one request. The device reads a batch as a
	// script, so a very long body is refused rather than partly applied; the
	// published client tooling settled on this figure.
	maxParseCommands = 50
)

// FQDNDeployer installs FQDN object groups over the same RCI transport the
// route deployer uses. It shares this package because it is the same
// authenticated conversation with the same device, not a second protocol.
//
// Exact persisted creation evidence supplies deletion authority. Names and
// interface membership never establish ownership (ADR 0040).
type FQDNDeployer struct {
	options Options
}

func NewFQDNDeployer(options Options) *FQDNDeployer { return &FQDNDeployer{options: options} }

func (*FQDNDeployer) ID() string { return FQDNDeployerID }

// Probe reports the device and refuses a firmware without DNS-based routes by
// leaving FormatKey empty, so the caller states the incompatibility with the
// version it found.
func (d *FQDNDeployer) Probe(ctx context.Context, connection application.Connection) (application.DeviceInfo, error) {
	session, err := d.routes().connect(ctx, connection)
	if err != nil {
		return application.DeviceInfo{}, err
	}
	info, err := session.identity(ctx, connection, FQDNDeployerID)
	if err != nil {
		return info, err
	}
	if !SupportedFQDNFirmware(info.FirmwareVersion) {
		return info, nil
	}
	// The interface a group will be routed to must exist before any group is
	// written. This firmware floor is lower than the route deployer's, so the
	// check is made here rather than borrowed from it.
	if err := session.requireInterface(ctx, info.Interface); err != nil {
		return info, err
	}
	// Require readable group and route state. The ownership-scoped preview
	// checks route policy before backup; Deploy reads again to catch drift.
	if _, _, err := session.observedGroups(ctx); err != nil {
		return info, err
	}
	info.FormatKey = keeneticdns.Version
	return info, nil
}

// SupportedFQDNFirmware reports whether a reported version has DNS-based
// routes. A version this deployer cannot parse is unsupported: guessing would
// risk writing a command surface the firmware does not have.
func SupportedFQDNFirmware(version string) bool { return atLeast(version, MinFQDNFirmware) }

func (d *FQDNDeployer) Backup(ctx context.Context, device application.DeviceInfo, connection application.Connection) (application.BackupPayload, error) {
	return d.routes().Backup(ctx, device, connection)
}

func (d *FQDNDeployer) Rollback(ctx context.Context, device application.DeviceInfo, connection application.Connection, backup application.BackupPayload) error {
	return d.routes().Rollback(ctx, device, connection, backup)
}

// Deploy reconciles the device against the artifact.
//
// Idempotence comes from the shape of the operation rather than from comparing
// hashes: the deployer reads what it previously wrote, removes every group and
// entry the artifact no longer contains, adds what is missing, and points each
// group at the named interface. Applying the same artifact twice therefore
// leaves the same device state, and a list that dropped a list stops costing
// the group budget instead of accumulating across refreshes.
//
// A batch that fails part way leaves the device partly written. That is caught
// by Verify and undone by the rollback the deployment flow already performs; it
// is not something this method can make atomic, because the device applies each
// command as it reads it.
func (d *FQDNDeployer) Deploy(ctx context.Context, device application.DeviceInfo, connection application.Connection, artifact application.DeployArtifact) error {
	wanted, err := artifactGroups(artifact)
	if err != nil {
		return err
	}
	session, err := d.routes().connect(ctx, connection)
	if err != nil {
		return err
	}
	existing, routes, err := session.observedGroups(ctx)
	if err != nil {
		return err
	}
	existing, routes, err = scopedGroups(artifact, wanted, existing, routes, true)
	if err != nil {
		return err
	}
	commands := reconcileGroups(wanted, existing, routes, device.Interface)
	if len(commands) == 0 {
		// Nothing to change is a successful deployment, not a skipped one.
		return nil
	}
	commands = append(commands, "system configuration save")
	return session.parseBatch(ctx, commands)
}

// Verify reads the device's own groups back. A deployment is applied only if
// the device reports exactly the artifact's groups within the persisted
// ownership scope, with its entries and automatic routes on the named interface.
func (d *FQDNDeployer) Verify(ctx context.Context, device application.DeviceInfo, connection application.Connection, artifact application.DeployArtifact) error {
	wanted, err := artifactGroups(artifact)
	if err != nil {
		return err
	}
	session, err := d.routes().connect(ctx, connection)
	if err != nil {
		return err
	}
	existing, routes, err := session.observedGroups(ctx)
	if err != nil {
		return err
	}
	existing, routes, err = scopedGroups(artifact, wanted, existing, routes, false)
	if err != nil {
		return err
	}
	missing, extra := compareGroups(wanted, existing, routes, device.Interface)
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d missing, %d unexpected (missing=%s unexpected=%s)",
		ErrDeviceAnswer, len(missing), len(extra), strings.Join(missing, ","), strings.Join(extra, ","))
}

// routes is the route deployer's transport, reused rather than reimplemented.
// Authentication, the destination policy, the backup surface and the rollback
// are properties of the device, not of the artifact format.
func (d *FQDNDeployer) routes() *Deployer { return &Deployer{options: d.options} }

// reconcileGroups is the whole reconciliation decision as a pure function of
// what the artifact wants and what the device reports. Keeping it free of the
// transport is what lets the ordering rules below be tested without a device.
//
// The order is not cosmetic. A group cannot be removed while a route still
// points at it, so routes are withdrawn first. Entries are removed before new
// ones are added, so a group that changed heavily never briefly holds more than
// the device's per-group bound. A route is attached last, when the group it
// names is complete, so a resolved answer is never sent to a half-filled group.
func reconcileGroups(wanted, existing map[string][]string, routes map[string]fqdnRoute, deviceInterface string) []string {
	commands := make([]string, 0, 16)
	observedNames := maps.Clone(existing)
	for name := range routes {
		if _, exists := observedNames[name]; !exists {
			observedNames[name] = nil
		}
	}
	for _, name := range sortedNames(observedNames) {
		if _, keep := wanted[name]; keep {
			continue
		}
		if attached, bound := routes[name]; bound {
			commands = append(commands, "no dns-proxy route object-group "+name+" "+attached.Interface)
		}
		if _, exists := existing[name]; exists {
			commands = append(commands, "no object-group fqdn "+name)
		}
	}
	for _, name := range sortedNames(wanted) {
		present, exists := existing[name]
		if !exists {
			commands = append(commands, "object-group fqdn "+name)
		}
		keep := membership(wanted[name])
		for _, entry := range sortedEntries(present) {
			if _, stays := keep[entry]; !stays {
				commands = append(commands, "no object-group fqdn "+name+" include "+entry)
			}
		}
		held := membership(present)
		for _, entry := range sortedEntries(wanted[name]) {
			if _, already := held[entry]; !already {
				commands = append(commands, "object-group fqdn "+name+" include "+entry)
			}
		}
		attached, bound := routes[name]
		if bound && attached.Interface == deviceInterface && attached.Auto {
			continue
		}
		if bound {
			commands = append(commands, "no dns-proxy route object-group "+name+" "+attached.Interface)
		}
		// auto selects the vendor's automatic-application policy. It is part
		// of desired state, not evidence of interface connectivity.
		commands = append(commands, "dns-proxy route object-group "+name+" "+deviceInterface+" auto")
	}
	return commands
}

// compareGroups states what the device is missing and what it holds that the
// artifact does not, in terms an operator can act on: a group, one entry of a
// group, or a group that is not routed where it was asked to be.
func compareGroups(wanted, existing map[string][]string, routes map[string]fqdnRoute, deviceInterface string) (missing, extra []string) {
	missing = make([]string, 0)
	extra = make([]string, 0)
	for _, name := range sortedNames(wanted) {
		present, exists := existing[name]
		if !exists {
			missing = append(missing, name)
			continue
		}
		held := membership(present)
		for _, entry := range sortedEntries(wanted[name]) {
			if _, found := held[entry]; !found {
				missing = append(missing, name+"/"+entry)
			}
		}
		keep := membership(wanted[name])
		for _, entry := range sortedEntries(present) {
			if _, stays := keep[entry]; !stays {
				extra = append(extra, name+"/"+entry)
			}
		}
		if attached, bound := routes[name]; !bound || attached.Interface != deviceInterface || !attached.Auto {
			missing = append(missing, name+"@"+deviceInterface)
		}
	}
	for _, name := range sortedNames(existing) {
		if _, wantedGroup := wanted[name]; !wantedGroup {
			extra = append(extra, name)
		}
	}
	for name := range routes {
		if _, wantedGroup := wanted[name]; !wantedGroup {
			if _, exists := existing[name]; !exists {
				extra = append(extra, name+"@route")
			}
		}
	}
	return missing, extra
}

// artifactGroups parses the artifact with the renderer's own validator, so a
// corrupted or foreign file can never reach the device.
func artifactGroups(artifact application.DeployArtifact) (map[string][]string, error) {
	if artifact.RendererID != keeneticdns.ID {
		return nil, fmt.Errorf("%w: renderer %q", ErrArtifactMismatch, artifact.RendererID)
	}
	parsed, err := keeneticdns.Parse(artifact.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactMismatch, err)
	}
	if len(parsed) == 0 || len(parsed) > keeneticdns.MaxGroups {
		return nil, fmt.Errorf("%w: %d groups", ErrArtifactMismatch, len(parsed))
	}
	groups := make(map[string][]string, len(parsed))
	total := 0
	for _, group := range parsed {
		groups[group.Name] = group.Entries
		total += len(group.Entries)
	}
	if total > MaxFQDNEntries {
		return nil, fmt.Errorf("%w: %d entries", ErrArtifactMismatch, total)
	}
	return groups, nil
}

func sortedNames(groups map[string][]string) []string {
	names := slices.Sorted(maps.Keys(groups))
	return names
}

func sortedEntries(entries []string) []string {
	ordered := append([]string(nil), entries...)
	slices.Sort(ordered)
	return ordered
}

func membership(entries []string) map[string]struct{} {
	set := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		set[entry] = struct{}{}
	}
	return set
}

// Requirements describes what this transport needs. The interface is required
// because a group that is not pointed anywhere routes nothing, and the operator
// is the one who knows which tunnel this list belongs on.
func (*FQDNDeployer) Requirements() application.ConnectionRequirements {
	return application.ConnectionRequirements{
		AddressLabel:    "Адрес устройства",
		AddressExample:  "http://192.168.1.1",
		NeedsCredential: true,
		NeedsInterface:  true,
		InterfaceLabel:  "Интерфейс устройства",
	}
}

func (d *FQDNDeployer) ValidateConnection(connection application.Connection) error {
	return d.routes().ValidateConnection(connection)
}

func (d *FQDNDeployer) ValidateStoredConnection(connection application.Connection) error {
	return d.routes().ValidateStoredConnection(connection)
}

// parseBatch issues CLI commands through the RCI parse surface. The object
// group commands have no documented structured form, and the command line is
// what the device's own reference and the published client both use, so it is
// the form this deployer speaks rather than a JSON tree it would be inventing.
func (s *session) parseBatch(ctx context.Context, commands []string) error {
	for start := 0; start < len(commands); start += maxParseCommands {
		end := min(start+maxParseCommands, len(commands))
		requests := make([]map[string]string, 0, end-start)
		for _, command := range commands[start:end] {
			if command == "" || strings.ContainsAny(command, "\n\r") {
				return fmt.Errorf("%w: refusing a command that is not one line", ErrDeviceRefused)
			}
			requests = append(requests, map[string]string{"parse": command})
		}
		encoded, err := json.Marshal(requests)
		if err != nil {
			return fmt.Errorf("%w: encode command batch", ErrDeviceRefused)
		}
		payload, err := s.raw(ctx, http.MethodPost, "/rci/", encoded)
		if err != nil {
			return err
		}
		if err := parseAnswers(payload); err != nil {
			return err
		}
	}
	return nil
}

// parseAnswers refuses the batch if the device reported an error for any
// command. The device answers 200 even for a rejected command, so the body is
// what decides success.
func parseAnswers(payload []byte) error {
	payload = []byte(strings.TrimSpace(string(payload)))
	if len(payload) == 0 || (payload[0] != '{' && payload[0] != '[') {
		return fmt.Errorf("%w: command answer is not an object or array", ErrDeviceAnswer)
	}
	var answers []json.RawMessage
	if err := json.Unmarshal(payload, &answers); err != nil {
		var single json.RawMessage
		if err := json.Unmarshal(payload, &single); err != nil {
			return fmt.Errorf("%w: batch answer is not JSON", ErrDeviceAnswer)
		}
		answers = []json.RawMessage{single}
	}
	for _, answer := range answers {
		if err := parseAnswerError(answer); err != nil {
			return err
		}
	}
	return nil
}

// parseAnswerError reads the status of one parsed command. The status list sits
// under "parse" rather than at the top level, which is the only way this answer
// differs from a structured command's.
func parseAnswerError(answer json.RawMessage) error {
	var envelope struct {
		Parse json.RawMessage `json:"parse"`
	}
	if err := json.Unmarshal(answer, &envelope); err == nil && len(envelope.Parse) > 0 {
		return deviceError(envelope.Parse)
	}
	return deviceError(answer)
}
