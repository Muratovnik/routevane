package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var ErrFQDNOwnershipConflict = errors.New("FQDN group ownership requires manual migration")

// ManagedFQDNGroup is an exact group and attachment created by one output.
// An observed name, even with identical content, does not establish ownership.
type ManagedFQDNGroup struct {
	Name      string   `json:"name"`
	Entries   []string `json:"entries"`
	Interface string   `json:"interface"`
	Auto      bool     `json:"auto"`
}

type ManagedFQDNOwnership struct {
	Endpoint string
	OutputID string
	Groups   []ManagedFQDNGroup
}

func (s ManagedFQDNOwnership) Validate() error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !isHexID(s.OutputID) || len(s.Groups) > 128 {
		return fmt.Errorf("invalid FQDN ownership scope")
	}
	seen := map[string]bool{}
	for _, g := range s.Groups {
		if g.Name == "" || len(g.Name) > 64 || seen[g.Name] || g.Interface == "" || len(g.Entries) == 0 || len(g.Entries) > 300 {
			return fmt.Errorf("invalid owned FQDN group")
		}
		for _, c := range g.Name {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return fmt.Errorf("invalid owned FQDN name")
			}
		}
		if strings.ContainsAny(g.Interface, "\r\n\x00") {
			return fmt.Errorf("invalid owned FQDN interface")
		}
		seen[g.Name] = true
	}
	return nil
}

type ManagedFQDNRepository interface {
	ManagedFQDNOwnership(context.Context, string, string) (ManagedFQDNOwnership, error)
	ReplaceManagedFQDNOwnership(context.Context, ManagedFQDNOwnership) error
	RetireManagedFQDNOwnership(context.Context, string, string) error
}

// ManagedFQDNDeployer supplies device-specific desired state and validates the
// read-only mutation plan before the lifecycle creates a backup or writes.
type ManagedFQDNDeployer interface {
	FQDNEndpoint(Connection) (string, error)
	DesiredFQDNGroups(DeviceInfo, DeployArtifact) ([]ManagedFQDNGroup, error)
	PreviewFQDNGroups(context.Context, DeviceInfo, Connection, DeployArtifact) ([]string, error)
}
