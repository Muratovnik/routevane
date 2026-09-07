package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	deployerkeenetic "github.com/Muratovnik/routevane/internal/deployers/keenetic"
	"github.com/Muratovnik/routevane/internal/discovery"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/planner"
	"github.com/Muratovnik/routevane/internal/plugin"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
	"github.com/Muratovnik/routevane/internal/sources/dns"
	"github.com/Muratovnik/routevane/internal/sources/httpfeed"
)

var version = "dev"

// PluginsDirVariable names the directory holding installed out-of-process
// adapters. When it is unset no plugin host is started at all.
const PluginsDirVariable = "ROUTEVANE_PLUGINS_DIR"

type delayTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type realDelayTicker struct{ timer *time.Timer }

func (t realDelayTicker) Chan() <-chan time.Time { return t.timer.C }
func (t realDelayTicker) Stop()                  { t.timer.Stop() }

type runtimeDeps struct {
	Resolver     dns.Resolver
	FeedResolver httpfeed.Resolver
	FeedDialer   httpfeed.Dialer
	FeedOptions  httpfeed.Options
	// DiscoveryDialer and DiscoveryHostRules are the discovery browser's seams.
	// Tests point a public-looking hostname at a local test server without
	// weakening the destination policy, which still sees the hostname and the
	// address it resolves to.
	DiscoveryDialer      discovery.Dialer
	DiscoveryHostRules   string
	DiscoveryTrustedSPKI []string
	DiscoveryBrowserPath string
	// DeviceDialer is the deployment transport seam. Tests point a private
	// device address at a local device double without relaxing the device
	// destination policy, which still refuses anything that is not a local
	// network address.
	DeviceDialer deployerkeenetic.Dialer
	// PluginsDir is where installed out-of-process adapters live. Empty means no
	// plugin host is started at all.
	PluginsDir     string
	Now            func() time.Time
	Context        context.Context
	NewDelayTicker func(time.Duration) delayTicker
	SignalContext  func(context.Context) (context.Context, context.CancelFunc)
	ArtifactWriter filesystem.ArtifactWriter
	BuildWriter    filesystem.BuildOutputWriter
	Listen         func(string, string) (net.Listener, error)
	OpenBrowser    func(context.Context, string) error
	DesktopInput   io.Reader
}

func productionDeps() runtimeDeps {
	return runtimeDeps{
		Resolver:     net.DefaultResolver,
		FeedResolver: net.DefaultResolver,
		Now:          time.Now,
		Context:      context.Background(),
		NewDelayTicker: func(delay time.Duration) delayTicker {
			return realDelayTicker{timer: time.NewTimer(delay)}
		},
		SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
			return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
		},
		Listen:       net.Listen,
		OpenBrowser:  openSystemBrowser,
		DesktopInput: os.Stdin,
	}
}

func normalizeDeps(deps runtimeDeps) runtimeDeps {
	production := productionDeps()
	if deps.Resolver == nil {
		deps.Resolver = production.Resolver
	}
	if deps.FeedResolver == nil {
		deps.FeedResolver = production.FeedResolver
	}
	if deps.PluginsDir == "" {
		// One place, like the browser and the device credential: an installed
		// plugin set is an operator's local choice, not a per-command flag.
		deps.PluginsDir = os.Getenv(PluginsDirVariable)
	}
	if deps.Now == nil {
		deps.Now = production.Now
	}
	if deps.Context == nil {
		deps.Context = production.Context
	}
	if deps.NewDelayTicker == nil {
		deps.NewDelayTicker = production.NewDelayTicker
	}
	if deps.SignalContext == nil {
		deps.SignalContext = production.SignalContext
	}
	if deps.Listen == nil {
		deps.Listen = production.Listen
	}
	if deps.OpenBrowser == nil {
		deps.OpenBrowser = production.OpenBrowser
	}
	if deps.DesktopInput == nil {
		deps.DesktopInput = production.DesktopInput
	}
	return deps
}

func run(stdout, stderr io.Writer, args []string) int {
	return runWithDeps(stdout, stderr, args, productionDeps())
}

func runWithDeps(stdout, stderr io.Writer, args []string, deps runtimeDeps) int {
	deps = normalizeDeps(deps)
	logger := newLogger(stderr)
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "routevane %s\n", version)
		return 0
	}
	if len(args) > 0 && args[0] == "preview" {
		return runPreview(stdout, stderr, logger, args[1:], deps)
	}
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}
	switch args[0] {
	case "desktop":
		return runDesktop(stdout, logger, args[1:], deps)
	case "refresh":
		options, ok := parseRefresh(args[1:])
		if !ok {
			writeUsage(stderr)
			return 2
		}
		return runRefresh(deps.Context, stdout, logger, options, deps)
	case "build":
		options, ok := parseBuild(args[1:])
		if !ok {
			writeUsage(stderr)
			return 2
		}
		return runBuild(deps.Context, stdout, logger, options, deps)
	case "doctor":
		options, ok := parseDoctor(args[1:])
		if !ok {
			writeUsage(stderr)
			return 2
		}
		return runDoctor(deps.Context, stdout, logger, options)
	case "run":
		options, ok := parseRun(args[1:])
		if !ok {
			writeUsage(stderr)
			return 2
		}
		return runScheduler(stdout, logger, options, deps)
	case "discover":
		options, ok := parseDiscover(args[1:])
		if !ok {
			writeUsage(stderr)
			return 2
		}
		return runDiscover(deps.Context, stdout, logger, options, deps)
	case "learn":
		options, ok := parseLearn(args[1:])
		if !ok {
			writeUsage(stderr)
			return 2
		}
		return runLearn(deps.Context, stdout, logger, options, deps)
	case "deploy":
		options, ok := parseDeploy(args[1:])
		if !ok {
			writeUsage(stderr)
			return 2
		}
		return runDeploy(deps.Context, stdout, logger, options, deps)
	case "serve":
		options, ok := parseServe(args[1:])
		if !ok {
			writeUsage(stderr)
			return 2
		}
		return runServe(stdout, logger, options, deps)
	default:
		writeUsage(stderr)
		return 2
	}
}

// runPreview renders the built-in example service to stdout without a
// database. It exists so a fresh checkout can show a routing plan before any
// state is created.
func runPreview(stdout, stderr io.Writer, logger *slog.Logger, args []string, deps runtimeDeps) int {
	if len(args) != 4 || args[0] != "--service" || args[1] != "example" || args[2] != "--target" || args[3] != "raw-json" {
		writeUsage(stderr)
		return 2
	}
	operationStarted := time.Now().UTC()
	cutoff := deps.Now().UTC()
	if cutoff.IsZero() {
		logResult(logger, "preview", "example", "", "failed", 0, operationStarted, "clock_failed")
		return 1
	}
	ctx, cancel := context.WithTimeout(deps.Context, 5*time.Second)
	defer cancel()
	definition := domainExampleList()
	observer := dns.NewObserver(deps.Resolver)
	observations, err := observer.Observe(ctx, dns.Query{ListID: definition.ID, ComponentID: "web", SourceID: "dns", Names: definition.DNSNames}, cutoff)
	if err != nil {
		logResult(logger, "preview", "example", "dns", "failed", 0, operationStarted, "dns_observation_failed")
		return 1
	}
	store := dns.NewMemoryStore()
	store.Add(observations.Sightings, observations.Relations)
	plan, err := planner.BuildPlanWithRelations(definition, store.Sightings(cutoff), store.Relations(cutoff), domain.RawJSONTargetDefinition(), cutoff)
	if err != nil {
		logResult(logger, "preview", "example", "", "failed", 0, operationStarted, "plan_failed")
		return 1
	}
	if err := rawjson.Write(stdout, plan); err != nil {
		logResult(logger, "preview", "example", "", "failed", 0, operationStarted, "render_failed")
		return 1
	}
	return 0
}

func domainExampleList() domain.ListDefinition { return domain.ExampleListDefinition() }

func newLogger(stderr io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && (attr.Key == slog.TimeKey || attr.Key == slog.LevelKey || attr.Key == slog.MessageKey) {
				return slog.Attr{}
			}
			return attr
		},
	}))
}

func logResult(logger *slog.Logger, operation, list, source, status string, count int, started time.Time, errorCode string) {
	duration := int64(0)
	if !started.IsZero() {
		duration = time.Since(started).Milliseconds()
		if duration < 0 {
			duration = 0
		}
	}
	logger.Info("operation", "operation", operation, "service", list, "source", source, "status", status, "count", count, "duration", duration, "error_code", errorCode)
}

func writeUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: routevane version | preview --service example --target raw-json | refresh --service ID [--catalog-dir DIR --data-dir DIR] | build --target raw-json --service ID [--catalog-dir DIR --data-dir DIR] | build --target keenetic --service ID [--service ID ... --output DIR --catalog-dir DIR --data-dir DIR] | doctor [--catalog-dir DIR --data-dir DIR] | run --target raw-json --service ID [--interval 30m --catalog-dir DIR --data-dir DIR] | serve [--port 8765 --catalog-dir DIR --data-dir DIR --open-browser] | discover --url URL [--confirm --service-id ID --title TEXT --seed DOMAIN --browser PATH --catalog-dir DIR --data-dir DIR] | learn --scenario FILE | --har FILE --url URL [--confirm --service-id ID --title TEXT --seed DOMAIN --browser PATH --catalog-dir DIR --data-dir DIR] | deploy --artifact ID --target ID --device URL|file://PATH [--user NAME --interface NAME --confirm --device-tls-untrusted --catalog-dir DIR --data-dir DIR]")
}

func main() {
	if handled, exitCode := plugin.RunProcessRunner(os.Args[1:]); handled {
		os.Exit(exitCode)
	}
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}
