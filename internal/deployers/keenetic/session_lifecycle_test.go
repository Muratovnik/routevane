package keenetic

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

type observedSessionConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *observedSessionConn) Close() error {
	err := c.Conn.Close()
	c.closed.Store(true)
	return err
}

type observedSessionDialer struct {
	target string
	mu     sync.Mutex
	opened []*observedSessionConn
}

func (d *observedSessionDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, network, d.target)
	if err != nil {
		return nil, err
	}
	observed := &observedSessionConn{Conn: connection}
	d.mu.Lock()
	d.opened = append(d.opened, observed)
	d.mu.Unlock()
	return observed, nil
}

func (d *observedSessionDialer) assertClosed(t *testing.T) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.opened) != 1 {
		t.Errorf("one device conversation used %d connections; keepalive should be retained within it", len(d.opened))
	}
	for _, connection := range d.opened {
		if !connection.closed.Load() {
			t.Error("the device conversation returned with an open connection")
		}
	}
}

func (d *observedSessionDialer) closeAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, connection := range d.opened {
		_ = connection.Close()
	}
}

func TestEachDeviceConversationClosesItsConnectionsOnReturn(t *testing.T) {
	for _, operation := range []string{
		"probe", "backup", "current routes", "apply routes", "verify routes", "rollback",
		"fqdn probe", "fqdn deploy", "fqdn verify", "fqdn preview", "authentication refused", "fqdn authentication refused",
	} {
		t.Run(operation, func(t *testing.T) {
			double := newDeviceDouble("5.1.2")
			double.routes["192.0.2.10/255.255.255.255"] = deviceInterface
			double.comments["192.0.2.10/255.255.255.255"] = ""
			group := keeneticdns.GroupPrefix + "example"
			double.fqdn[group] = []string{"example.com"}
			double.dnsRoutes[group], double.dnsAuto[group] = deviceInterface, true
			state, err := json.Marshal(deviceConfiguration{Routes: double.routes, Comments: double.comments, Groups: double.fqdn, DNSRoutes: double.dnsRoutes, Auto: double.dnsAuto})
			if err != nil {
				t.Fatal(err)
			}
			backup := application.BackupPayload{Payload: append(append(append([]byte(nil), double.config...), []byte("! fixture-state ")...), state...)}
			server := httptest.NewServer(double.handler())
			defer server.Close()
			dialer := &observedSessionDialer{target: server.Listener.Addr().String()}
			defer dialer.closeAll()
			routes, fqdn := New(Options{Dialer: dialer}), NewFQDNDeployer(Options{Dialer: dialer})
			connection := application.Connection{URL: "http://10.255.0.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface}
			device := application.DeviceInfo{Interface: deviceInterface}
			artifact := testArtifact(t, "192.0.2.10/32")
			dnsArtifact := fqdnArtifact(t, "example=example.com")
			dnsArtifact.OwnedFQDNGroups = []application.ManagedFQDNGroup{{Name: group, Entries: []string{"example.com"}, Interface: deviceInterface, Auto: true}}
			ctx := t.Context()
			wantError := false
			switch operation {
			case "probe":
				_, err = routes.Probe(ctx, connection)
			case "backup":
				_, err = routes.Backup(ctx, device, connection)
			case "current routes":
				_, err = routes.CurrentManagedRoutes(ctx, device, connection)
			case "apply routes":
				err = routes.ApplyManagedRoutes(ctx, device, connection, application.ManagedRouteMutation{Upsert: []application.ManagedRouteSpec{{Prefix: netip.MustParsePrefix("192.0.2.10/32")}}})
			case "verify routes":
				err = routes.Verify(ctx, device, connection, artifact)
			case "rollback":
				err = routes.Rollback(ctx, device, connection, backup)
			case "fqdn probe":
				_, err = fqdn.Probe(ctx, connection)
			case "fqdn deploy":
				err = fqdn.Deploy(ctx, device, connection, dnsArtifact)
			case "fqdn verify":
				err = fqdn.Verify(ctx, device, connection, dnsArtifact)
			case "fqdn preview":
				_, err = fqdn.PreviewFQDNGroups(ctx, device, connection, dnsArtifact)
			case "authentication refused":
				wantError, connection.Password = true, "incorrect"
				_, err = routes.Probe(ctx, connection)
			case "fqdn authentication refused":
				wantError, connection.Password = true, "incorrect"
				_, err = fqdn.Probe(ctx, connection)
			}
			if (err != nil) != wantError {
				t.Fatalf("operation error=%v; want error=%t", err, wantError)
			}
			dialer.assertClosed(t)
		})
	}
}
