package keenetic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/netpolicy"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

type fqdnRoute struct {
	Interface               string
	Gateway                 string
	Auto, Reject, Ambiguous bool
}

type fqdnGroup struct {
	Entries     []string
	Description string
	Excluded    bool
}

// observedGroups includes foreign objects so a collision cannot look absent.
func (s *session) observedGroups(ctx context.Context) (map[string]fqdnGroup, map[string]fqdnRoute, error) {
	var configured map[string]struct {
		Description string `json:"description"`
		Include     []struct {
			Address string `json:"address"`
		} `json:"include"`
		Exclude []struct {
			Address string `json:"address"`
		} `json:"exclude"`
	}
	if err := s.command(ctx, "/rci/object-group/fqdn", nil, &configured); err != nil {
		return nil, nil, err
	}
	groups := make(map[string]fqdnGroup, len(configured))
	for name, group := range configured {
		state := fqdnGroup{Description: group.Description, Excluded: len(group.Exclude) > 0}
		for _, entry := range group.Include {
			state.Entries = append(state.Entries, entry.Address)
		}
		groups[name] = state
	}
	var attached []struct {
		Group     string `json:"group"`
		Interface string `json:"interface"`
		Gateway   string `json:"gateway"`
		Auto      bool   `json:"auto"`
		Reject    bool   `json:"reject"`
	}
	if err := s.command(ctx, "/rci/dns-proxy/route", nil, &attached); err != nil {
		return nil, nil, err
	}
	routes := make(map[string]fqdnRoute, len(attached))
	for _, route := range attached {
		next := fqdnRoute{Interface: route.Interface, Gateway: route.Gateway, Auto: route.Auto, Reject: route.Reject}
		if previous, exists := routes[route.Group]; exists && previous != next {
			next.Ambiguous = true
		}
		routes[route.Group] = next
	}
	return groups, routes, nil
}

func scopedGroups(artifact application.DeployArtifact, wanted, existing map[string]fqdnGroup, routes map[string]fqdnRoute, beforeWrite bool) (map[string]fqdnGroup, map[string]fqdnRoute, error) {
	owned := map[string]bool{}
	for _, group := range artifact.OwnedFQDNGroups {
		owned[group.Name] = true
	}
	groups := map[string]fqdnGroup{}
	attached := map[string]fqdnRoute{}
	newCount := len(existing)
	for name := range wanted {
		_, present := existing[name]
		_, bound := routes[name]
		if beforeWrite && (present || bound) && !owned[name] {
			return nil, nil, fmt.Errorf("%w: unowned FQDN group %q; manual migration required", errors.Join(ErrDeviceAnswer, application.ErrFQDNOwnershipConflict), name)
		}
		if !present {
			newCount++
		}
	}
	for name, group := range existing {
		_, desired := wanted[name]
		if owned[name] || desired {
			if !keeneticdns.ValidGroupName(name) || group.Excluded {
				return nil, nil, fmt.Errorf("%w: unsupported FQDN group %q", ErrDeviceAnswer, name)
			}
			for _, entry := range group.Entries {
				if !keeneticdns.ValidEntry(entry) {
					return nil, nil, fmt.Errorf("%w: invalid entry in FQDN group %q", ErrDeviceAnswer, name)
				}
			}
			groups[name] = group
		}
		if owned[name] && !desired {
			newCount--
		}
	}
	if beforeWrite && newCount > keeneticdns.MaxGroups {
		return nil, nil, fmt.Errorf("%w: device FQDN group capacity exceeded", ErrDeviceAnswer)
	}
	for name, route := range routes {
		_, desired := wanted[name]
		if !owned[name] && !desired {
			continue
		}
		if !keeneticdns.ValidGroupName(name) || route.Reject || !validInterfaceWord(route.Interface) || route.Gateway != "" || route.Ambiguous {
			return nil, nil, fmt.Errorf("%w: unsupported route for group %q", ErrDeviceAnswer, name)
		}
		attached[name] = route
	}
	return groups, attached, nil
}

func (d *FQDNDeployer) FQDNEndpoint(connection application.Connection) (string, error) {
	endpoint, err := netpolicy.ValidateDeviceURL(connection.URL)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(endpoint.String(), "/"), nil
}

func (d *FQDNDeployer) DesiredFQDNGroups(device application.DeviceInfo, artifact application.DeployArtifact) ([]application.ManagedFQDNGroup, error) {
	wanted, err := artifactGroups(artifact)
	if err != nil {
		return nil, err
	}
	result := make([]application.ManagedFQDNGroup, 0, len(wanted))
	for _, name := range sortedNames(wanted) {
		group := wanted[name]
		result = append(result, application.ManagedFQDNGroup{Name: name, Entries: group.Entries, Description: group.Description, Interface: device.Interface, Auto: true})
	}
	return result, nil
}

func (d *FQDNDeployer) PreviewFQDNGroups(ctx context.Context, device application.DeviceInfo, connection application.Connection, artifact application.DeployArtifact) ([]string, error) {
	if !validInterfaceWord(device.Interface) {
		return nil, fmt.Errorf("%w: invalid FQDN interface", ErrDeviceAnswer)
	}
	wanted, err := artifactGroups(artifact)
	if err != nil {
		return nil, err
	}
	session, err := d.routes().connect(ctx, connection)
	if err != nil {
		return nil, err
	}
	defer session.close()
	existing, routes, err := session.observedGroups(ctx)
	if err != nil {
		return nil, err
	}
	existing, routes, err = scopedGroups(artifact, wanted, existing, routes, true)
	if err != nil {
		return nil, err
	}
	return reconcileGroups(wanted, existing, routes, device.Interface), nil
}

// PreviewFQDNRemoval lists the commands that would remove exactly the owned
// groups and their routing attachments. It reads the device and never writes.
func (d *FQDNDeployer) PreviewFQDNRemoval(ctx context.Context, device application.DeviceInfo, connection application.Connection, owned []application.ManagedFQDNGroup) ([]string, error) {
	session, err := d.routes().connect(ctx, connection)
	if err != nil {
		return nil, err
	}
	defer session.close()
	return session.removalCommands(ctx, device, owned)
}

// RemoveFQDNGroups withdraws each owned group's route and then deletes the
// group, the order the device requires (ADR 0021). A name outside the ledger is
// invisible here, so a group the operator or another output made is never in
// the batch. An owned group already gone needs nothing, and a device holding
// none of them is a verified no-op.
func (d *FQDNDeployer) RemoveFQDNGroups(ctx context.Context, device application.DeviceInfo, connection application.Connection, owned []application.ManagedFQDNGroup) (mutationSubmitted bool, err error) {
	defer func() {
		if err != nil && !mutationSubmitted {
			err = errors.Join(application.ErrDeviceUnchanged, err)
		}
	}()
	session, err := d.routes().connect(ctx, connection)
	if err != nil {
		return false, err
	}
	defer session.close()
	commands, err := session.removalCommands(ctx, device, owned)
	if err != nil || len(commands) == 0 {
		return false, err
	}
	commands = append(commands, "system configuration save")
	return true, session.parseBatch(ctx, commands)
}

// VerifyFQDNGroupsRemoved reads the device back and refuses while any owned
// group, or a routing attachment naming one, is still there.
func (d *FQDNDeployer) VerifyFQDNGroupsRemoved(ctx context.Context, device application.DeviceInfo, connection application.Connection, owned []application.ManagedFQDNGroup) error {
	session, err := d.routes().connect(ctx, connection)
	if err != nil {
		return err
	}
	defer session.close()
	existing, routes, err := session.ownedObservation(ctx, owned, false)
	if err != nil {
		return err
	}
	_, remaining := compareGroups(map[string]fqdnGroup{}, existing, routes, device.Interface)
	if len(remaining) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d owned objects still present (present=%s)", ErrDeviceAnswer, len(remaining), strings.Join(remaining, ","))
}

// removalCommands is reconcileGroups with nothing wanted, over the owned
// groups only: routes are withdrawn before the groups they name.
func (s *session) removalCommands(ctx context.Context, device application.DeviceInfo, owned []application.ManagedFQDNGroup) ([]string, error) {
	existing, routes, err := s.ownedObservation(ctx, owned, true)
	if err != nil {
		return nil, err
	}
	return reconcileGroups(map[string]fqdnGroup{}, existing, routes, device.Interface), nil
}

// ownedObservation reads the device and keeps only what the ledger owns, with
// the same refusals a send applies to an owned object it cannot handle.
func (s *session) ownedObservation(ctx context.Context, owned []application.ManagedFQDNGroup, beforeWrite bool) (map[string]fqdnGroup, map[string]fqdnRoute, error) {
	if len(owned) == 0 {
		return map[string]fqdnGroup{}, map[string]fqdnRoute{}, nil
	}
	existing, routes, err := s.observedGroups(ctx)
	if err != nil {
		return nil, nil, err
	}
	return scopedGroups(application.DeployArtifact{OwnedFQDNGroups: owned}, map[string]fqdnGroup{}, existing, routes, beforeWrite)
}

// The parse endpoint accepts CLI words, not interface descriptions. Native
// interface IDs include forms such as Wireguard0 and GigabitEthernet0/Vlan7.
func validInterfaceWord(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '/' || char == '.' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}
