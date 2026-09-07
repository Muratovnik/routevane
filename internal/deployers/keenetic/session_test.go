package keenetic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
)

func TestProbeUsesKeyedInterfaceIDsAndNeverGuessesFromDescriptions(t *testing.T) {
	double := newDeviceDouble("5.1.2")
	base := double.handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rci/show/interface" {
			writeJSON(w, map[string]any{
				"Wireguard0": map[string]string{"description": "Shared tunnel"},
				"Wireguard1": map[string]string{"description": "Shared tunnel"},
			})
			return
		}
		base.ServeHTTP(w, r)
	}))
	defer server.Close()
	options := Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}}
	connection := application.Connection{URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword}
	for _, deployer := range []application.Deployer{New(options), NewFQDNDeployer(options)} {
		connection.Interface = "Shared tunnel"
		if _, err := deployer.Probe(context.Background(), connection); !errors.Is(err, ErrDeviceAnswer) {
			t.Fatalf("description was resolved by guessing: %v", err)
		}
		connection.Interface = "Wireguard1"
		info, err := deployer.Probe(context.Background(), connection)
		if err != nil || info.Interface != "Wireguard1" || info.FormatKey == "" {
			t.Fatalf("keyed interface: %#v %v", info, err)
		}
	}
	if deploys, saves, restores := double.counts(); deploys+saves+restores != 0 {
		t.Fatal("an interface probe wrote to the device")
	}
}

func TestFQDNRouteFlagsAreNotSilentlyTreatedAsEquivalent(t *testing.T) {
	name := keeneticdns.GroupPrefix + "example"
	for _, test := range []struct {
		name     string
		flags    map[string]any
		conflict bool
		refused  bool
	}{
		{name: "reject omitted"},
		{name: "reject false", flags: map[string]any{"reject": false}},
		{name: "reject true", flags: map[string]any{"reject": true}, refused: true},
		{name: "invalid reject", flags: map[string]any{"reject": "false"}, refused: true},
		{name: "missing interface", flags: map[string]any{"interface": ""}, refused: true},
		{name: "conflicting interfaces", conflict: true, refused: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			double := newDeviceDouble("5.1.2")
			double.fqdn[name] = []string{"example.com"}
			base := double.handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rci/dns-proxy/route" {
					route := map[string]any{"group": name, "interface": deviceInterface, "auto": true}
					for key, value := range test.flags {
						route[key] = value
					}
					routes := []map[string]any{route, {"group": "foreign-group", "interface": "ISP", "reject": true}}
					if test.conflict {
						routes = append(routes, map[string]any{"group": name, "interface": "Wireguard1"})
					}
					writeJSON(w, routes)
					return
				}
				base.ServeHTTP(w, r)
			}))
			defer server.Close()
			deployer := NewFQDNDeployer(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
			connection := application.Connection{URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface}
			info, err := deployer.Probe(context.Background(), connection)
			if test.refused && !errors.Is(err, ErrDeviceAnswer) || !test.refused && err != nil {
				t.Fatal(err)
			}
			artifact := fqdnArtifact(t, "example=example.com")
			if test.refused {
				request := application.DeployRequest{Connection: connection, Artifact: artifact, Target: domain.TargetDefinition{ID: "keenetic-dns", RendererID: FQDNDeployerID, FormatKey: keeneticdns.Version}}
				result, err := application.DeployToDevice(context.Background(), request, application.DeployerRegistry{deployer.ID(): deployer}, unusedBackupStore{}, application.ClockFunc(time.Now))
				if err == nil || len(result.Events) != 1 || result.Events[0].Step != application.StepProbe || result.RolledBack {
					t.Fatalf("incompatible snapshot reached the write lifecycle: %#v %v", result, err)
				}
			}
			for _, operation := range []func(context.Context, application.DeviceInfo, application.Connection, application.DeployArtifact) error{deployer.Deploy, deployer.Verify} {
				err := operation(context.Background(), info, connection, artifact)
				if test.refused && !errors.Is(err, ErrDeviceAnswer) || !test.refused && err != nil {
					t.Fatalf("refused=%t err=%v", test.refused, err)
				}
			}
			if deploys, saves, restores := double.counts(); deploys+saves+restores != 0 {
				t.Fatal("idempotence or refusal wrote device state")
			}
		})
	}
}

type unusedBackupStore struct{}

func (unusedBackupStore) PutBackup(context.Context, string, time.Time, []byte) (application.BackupRef, error) {
	return application.BackupRef{}, errors.New("probe should refuse before backup")
}

func TestRCIReadBoundsTheResponseBodyAfterHeadersArrive(t *testing.T) {
	for _, callerDeadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "client timeout", true: "caller deadline"}[callerDeadline], func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"route":`))
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			deployer := New(Options{Dialer: fixtureDialer{target: server.Listener.Addr().String()}})
			session, err := deployer.connect(context.Background(), application.Connection{URL: "http://192.168.1.1", Username: deviceUser, Password: devicePassword, Interface: deviceInterface})
			if err != nil {
				t.Fatal(err)
			}
			if session.client.Timeout != requestTimeout {
				t.Fatal("production body timeout is absent")
			}
			session.client.Timeout = 2 * time.Second
			ctx := context.Background()
			if callerDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 75*time.Millisecond)
				defer cancel()
			} else {
				session.client.Timeout = 75 * time.Millisecond
			}
			started := time.Now()
			_, err = session.raw(ctx, http.MethodGet, "/rci/show/sc/ip/route", nil)
			if !errors.Is(err, ErrDeviceUnreachable) || time.Since(started) > time.Second {
				t.Fatalf("unbounded or accepted partial body: %v", err)
			}
		})
	}
}
