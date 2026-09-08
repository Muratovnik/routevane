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
	Auto, Reject, Ambiguous bool
}

// observedGroups includes foreign objects so a collision cannot look absent.
func (s *session) observedGroups(ctx context.Context) (map[string][]string, map[string]fqdnRoute, error) {
	var configured map[string]struct {
		Include []struct {
			Address string `json:"address"`
		} `json:"include"`
	}
	if err := s.command(ctx, "/rci/object-group/fqdn", nil, &configured); err != nil {
		return nil, nil, err
	}
	groups := make(map[string][]string, len(configured))
	for name, group := range configured {
		groups[name] = nil
		for _, entry := range group.Include {
			groups[name] = append(groups[name], entry.Address)
		}
	}
	var attached []struct {
		Group     string `json:"group"`
		Interface string `json:"interface"`
		Auto      bool   `json:"auto"`
		Reject    bool   `json:"reject"`
	}
	if err := s.command(ctx, "/rci/dns-proxy/route", nil, &attached); err != nil {
		return nil, nil, err
	}
	routes := make(map[string]fqdnRoute, len(attached))
	for _, route := range attached {
		next := fqdnRoute{Interface: route.Interface, Auto: route.Auto, Reject: route.Reject}
		if previous, exists := routes[route.Group]; exists && previous != next {
			next.Ambiguous = true
		}
		routes[route.Group] = next
	}
	return groups, routes, nil
}

func scopedGroups(artifact application.DeployArtifact, wanted, existing map[string][]string, routes map[string]fqdnRoute, beforeWrite bool) (map[string][]string, map[string]fqdnRoute, error) {
	owned := map[string]bool{}
	for _, group := range artifact.OwnedFQDNGroups {
		owned[group.Name] = true
	}
	groups := map[string][]string{}
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
	for name, entries := range existing {
		_, desired := wanted[name]
		if owned[name] || desired {
			groups[name] = entries
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
		if route.Reject || route.Interface == "" || route.Ambiguous {
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
		result = append(result, application.ManagedFQDNGroup{Name: name, Entries: wanted[name], Interface: device.Interface, Auto: true})
	}
	return result, nil
}

func (d *FQDNDeployer) PreviewFQDNGroups(ctx context.Context, device application.DeviceInfo, connection application.Connection, artifact application.DeployArtifact) ([]string, error) {
	wanted, err := artifactGroups(artifact)
	if err != nil {
		return nil, err
	}
	session, err := d.routes().connect(ctx, connection)
	if err != nil {
		return nil, err
	}
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
