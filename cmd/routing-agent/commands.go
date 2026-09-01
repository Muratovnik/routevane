package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
)

type commonOptions struct {
	ServiceID  string
	CatalogDir string
	DataDir    string
}

type buildOptions struct {
	ServiceIDs []string
	CatalogDir string
	DataDir    string
	Target     string
	OutputDir  string
}

type runOptions struct {
	commonOptions
	Target   string
	Interval time.Duration
}

type doctorOptions struct {
	CatalogDir string
	DataDir    string
}

type serveOptions struct {
	Port        int
	CatalogDir  string
	DataDir     string
	OpenBrowser bool
}

func commonFlags(name string) (*flag.FlagSet, *commonOptions) {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	options := &commonOptions{}
	set.StringVar(&options.ServiceID, "service", "", "service id")
	set.StringVar(&options.CatalogDir, "catalog-dir", "./catalog", "catalog directory")
	set.StringVar(&options.DataDir, "data-dir", "./data", "data directory")
	return set, options
}

func parseRefresh(args []string) (commonOptions, bool) {
	set, options := commonFlags("refresh")
	if set.Parse(args) != nil || set.NArg() != 0 || domain.ValidateSlug(options.ServiceID) != nil {
		return commonOptions{}, false
	}
	return *options, true
}

func parseBuild(args []string) (buildOptions, bool) {
	set := flag.NewFlagSet("build", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var services repeatedServices
	set.Var(&services, "service", "service id (repeatable)")
	target := set.String("target", "", "target id")
	catalogDir := set.String("catalog-dir", "./catalog", "catalog directory")
	dataDir := set.String("data-dir", "./data", "data directory")
	outputDir := set.String("output", "", "output directory")
	// The target is validated against the catalog and the resolved renderer
	// registry, not against a list here: a target served by an installed plugin
	// is as legitimate as a built-in one. "raw-json" stays the one special case
	// because it is the diagnostic path rather than a catalog target.
	if set.Parse(args) != nil || set.NArg() != 0 || len(services) == 0 || domain.ValidateSlug(*target) != nil {
		return buildOptions{}, false
	}
	for _, serviceID := range services {
		if domain.ValidateSlug(serviceID) != nil {
			return buildOptions{}, false
		}
	}
	services = domain.StableStrings(services)
	outputExplicit := false
	set.Visit(func(value *flag.Flag) {
		if value.Name == "output" {
			outputExplicit = true
		}
	})
	if *target == "raw-json" && (len(services) != 1 || outputExplicit) {
		return buildOptions{}, false
	}
	return buildOptions{ServiceIDs: services, CatalogDir: *catalogDir, DataDir: *dataDir, Target: *target, OutputDir: *outputDir}, true
}

type repeatedServices []string

func (values *repeatedServices) String() string { return "" }

func (values *repeatedServices) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func parseDoctor(args []string) (doctorOptions, bool) {
	set := flag.NewFlagSet("doctor", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	options := doctorOptions{}
	set.StringVar(&options.CatalogDir, "catalog-dir", "./catalog", "catalog directory")
	set.StringVar(&options.DataDir, "data-dir", "./data", "data directory")
	if set.Parse(args) != nil || set.NArg() != 0 {
		return doctorOptions{}, false
	}
	return options, true
}

func parseRun(args []string) (runOptions, bool) {
	set, common := commonFlags("run")
	target := set.String("target", "", "target id")
	interval := set.Duration("interval", 30*time.Minute, "fixed delay")
	if set.Parse(args) != nil || set.NArg() != 0 || domain.ValidateSlug(common.ServiceID) != nil || *target != "raw-json" || *interval <= 0 {
		return runOptions{}, false
	}
	return runOptions{commonOptions: *common, Target: *target, Interval: *interval}, true
}

func parseServe(args []string) (serveOptions, bool) {
	set := flag.NewFlagSet("serve", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	port := set.Int("port", 8765, "loopback port")
	openBrowser := set.Bool("open-browser", false, "open the UI after it is ready")
	catalogDir := set.String("catalog-dir", "./catalog", "catalog directory")
	dataDir := set.String("data-dir", "./data", "data directory")
	if set.Parse(args) != nil || set.NArg() != 0 || *port < 1 || *port > 65535 {
		return serveOptions{}, false
	}
	return serveOptions{Port: *port, CatalogDir: *catalogDir, DataDir: *dataDir, OpenBrowser: *openBrowser}, true
}

func loadConfiguredService(ctx context.Context, options commonOptions) (catalogyaml.Catalog, domain.ServiceDefinition, error) {
	catalog, definitions, err := loadConfiguredServices(ctx, options.CatalogDir, []string{options.ServiceID})
	if err != nil {
		return catalogyaml.Catalog{}, domain.ServiceDefinition{}, fmt.Errorf("catalog configuration invalid")
	}
	return catalog, definitions[0], nil
}

func loadConfiguredServices(ctx context.Context, catalogDir string, serviceIDs []string) (catalogyaml.Catalog, []domain.ServiceDefinition, error) {
	catalog, err := catalogyaml.Load(ctx, catalogDir)
	if err != nil {
		return catalogyaml.Catalog{}, nil, fmt.Errorf("catalog configuration invalid")
	}
	ids := domain.StableStrings(serviceIDs)
	definitions := make([]domain.ServiceDefinition, 0, len(ids))
	for _, serviceID := range ids {
		definition, ok := catalog.Service(serviceID)
		if !ok {
			return catalogyaml.Catalog{}, nil, fmt.Errorf("service configuration missing")
		}
		definitions = append(definitions, definition)
	}
	return catalog, definitions, nil
}

func runRefresh(ctx context.Context, stdout io.Writer, logger *slog.Logger, options commonOptions, deps runtimeDeps) int {
	started := time.Now().UTC()
	catalog, definition, err := loadConfiguredService(ctx, options)
	if err != nil {
		logResult(logger, "refresh", options.ServiceID, "", "failed", 0, started, "catalog_invalid")
		return 2
	}
	return refreshLoaded(ctx, stdout, logger, options, catalog, definition, deps, started)
}

func refreshLoaded(ctx context.Context, stdout io.Writer, logger *slog.Logger, options commonOptions, _ catalogyaml.Catalog, definition domain.ServiceDefinition, deps runtimeDeps, started time.Time) int {
	root, err := filesystem.EnsureDataRoot(options.DataDir)
	if err != nil {
		logResult(logger, "refresh", options.ServiceID, "", "failed", 0, started, "data_root_invalid")
		return 2
	}
	store, err := sqlite.Open(ctx, root)
	if err != nil {
		logResult(logger, "refresh", options.ServiceID, "", "failed", 0, started, "database_unavailable")
		return 1
	}
	defer store.Close()
	resolved, err := resolveAdapters(ctx, deps, logger)
	if err != nil {
		logger.Warn("refresh failed", "operation", "refresh", "error", err.Error())
		logResult(logger, "refresh", options.ServiceID, "", "failed", 0, started, "adapters_unavailable")
		return 1
	}
	defer resolved.Close()
	if err := resolved.validateSourceDefinitions(definition); err != nil {
		logger.Warn("refresh failed", "operation", "refresh", "error", err.Error())
		logResult(logger, "refresh", options.ServiceID, "", "failed", 0, started, "adapters_unavailable")
		return 1
	}
	summary, refreshErr := application.RefreshService(ctx, definition, domain.RawJSONTargetProfile(), resolved.sources, store, application.ClockFunc(deps.Now))
	if encodeErr := json.NewEncoder(stdout).Encode(summary); encodeErr != nil {
		logResult(logger, "refresh", options.ServiceID, "", "failed", summary.Sightings, started, "output_failed")
		return 1
	}
	if errors.Is(refreshErr, application.ErrSourceDegraded) {
		// The stored observations are still usable inside the grace window, so
		// this cycle is a warning rather than a failed refresh.
		logger.Warn("refresh warning", "operation", "refresh", "service", options.ServiceID, "code", "source_degraded", "sources", strings.Join(summary.DegradedSources, ","))
		logResult(logger, "refresh", options.ServiceID, "", "success", summary.Sightings, started, "source_degraded")
		return 0
	}
	if refreshErr != nil {
		code := "refresh_failed"
		if errors.Is(refreshErr, application.ErrSourceFailed) {
			code = "source_failed"
		}
		logResult(logger, "refresh", options.ServiceID, "", "failed", summary.Sightings, started, code)
		return 1
	}
	logResult(logger, "refresh", options.ServiceID, "", "success", summary.Sightings, started, "")
	return 0
}

func runBuild(ctx context.Context, stdout io.Writer, logger *slog.Logger, options buildOptions, deps runtimeDeps) int {
	started := time.Now().UTC()
	catalog, definitions, err := loadConfiguredServices(ctx, options.CatalogDir, options.ServiceIDs)
	if err != nil {
		logResult(logger, "build", buildServiceLogID(options.ServiceIDs), "", "failed", 0, started, "catalog_invalid")
		return 2
	}
	return buildLoaded(ctx, stdout, logger, options, catalog, definitions, deps, started)
}

func buildLoaded(ctx context.Context, stdout io.Writer, logger *slog.Logger, options buildOptions, catalog catalogyaml.Catalog, definitions []domain.ServiceDefinition, deps runtimeDeps, started time.Time) int {
	serviceLogID := buildServiceLogID(options.ServiceIDs)
	root, err := filesystem.ResolvePrivateDataRoot(options.DataDir)
	if err != nil {
		logResult(logger, "build", serviceLogID, "", "failed", 0, started, "data_root_unavailable")
		return 1
	}
	store, err := sqlite.OpenExisting(ctx, root)
	if err != nil {
		logResult(logger, "build", serviceLogID, "", "failed", 0, started, "database_unavailable")
		return 1
	}
	defer store.Close()
	var result application.BuildResult
	switch options.Target {
	case "raw-json":
		if len(definitions) != 1 {
			logResult(logger, "build", serviceLogID, "", "failed", 0, started, "catalog_invalid")
			return 2
		}
		definition := definitions[0]
		artifactStore := filesystem.ArtifactStore{DataRoot: root, Writer: deps.ArtifactWriter}
		result, err = application.BuildService(ctx, definition, catalog.ActiveSourceRevisions(definition.ID), store, rawjson.Renderer{}, artifactStore, application.ClockFunc(deps.Now))
	default:
		// Catalog targets resolve through the shared renderer registry, so this
		// handler holds no per-format branch of its own.
		resolved, adapterErr := resolveAdapters(ctx, deps, logger)
		if adapterErr != nil {
			logger.Warn("build failed", "operation", "build", "error", adapterErr.Error())
			logResult(logger, "build", serviceLogID, "", "failed", 0, started, "adapters_unavailable")
			return 1
		}
		defer resolved.Close()
		target, ok := catalogTargets(catalog, resolved.renderers)[options.Target]
		if !ok {
			logResult(logger, "build", serviceLogID, "", "failed", 0, started, "target_invalid")
			return 2
		}
		renderer, rendererErr := resolved.renderers.For(target)
		if rendererErr != nil {
			logResult(logger, "build", serviceLogID, "", "failed", 0, started, "target_invalid")
			return 2
		}
		revisions := make(map[string]map[string]string, len(definitions))
		for _, definition := range definitions {
			revisions[definition.ID] = catalog.ActiveSourceRevisions(definition.ID)
		}
		outputStore := filesystem.BuildOutputStore{DataRoot: root, OutputDir: options.OutputDir, Writer: deps.BuildWriter}
		result, err = application.BuildServices(ctx, definitions, revisions, target, store, renderer, outputStore, application.ClockFunc(deps.Now))
	}
	if err != nil {
		// A local command that reports only a code is not operable; the reason
		// stays on the operator's own machine.
		logger.Warn("build failed", "operation", "build", "error", err.Error())
		logResult(logger, "build", serviceLogID, "", "failed", 0, started, buildErrorCode(err))
		return 1
	}
	for _, diagnostic := range result.Diagnostics {
		logger.Warn("build warning", "operation", "build", "code", diagnostic.Code, "count", diagnostic.Count)
	}
	if _, err := fmt.Fprintln(stdout, result.Path); err != nil {
		logResult(logger, "build", serviceLogID, "", "failed", result.RuleCount, started, "output_failed")
		return 1
	}
	logResult(logger, "build", serviceLogID, "", "success", result.RuleCount, started, "")
	return 0
}

func buildServiceLogID(serviceIDs []string) string {
	if len(serviceIDs) == 1 {
		return serviceIDs[0]
	}
	return "multiple"
}

func buildErrorCode(err error) string {
	switch {
	case errors.Is(err, application.ErrProfileMismatch):
		return "profile_mismatch"
	case errors.Is(err, sqlite.ErrProfileNotFound):
		return "profile_missing"
	case errors.Is(err, filesystem.ErrArtifactCollision):
		return "artifact_collision"
	case errors.Is(err, application.ErrRuleLimit):
		return "rule_limit"
	case errors.Is(err, application.ErrPartialCoverage):
		return "partial_coverage"
	case errors.Is(err, application.ErrPreflight):
		return "preflight_failed"
	default:
		return "build_failed"
	}
}
