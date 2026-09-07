package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/discovery"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

type discoverOptions struct {
	URL         string
	ListID      string
	Title       string
	ManualSeeds []string
	Confirm     bool
	CatalogDir  string
	DataDir     string
	Browser     string
}

func parseDiscover(args []string) (discoverOptions, bool) {
	options := discoverOptions{}
	seeds := repeatedFlag{}
	set := flag.NewFlagSet("discover", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&options.URL, "url", "", "site to inspect")
	set.StringVar(&options.ListID, "list-id", "", "local list identity")
	set.StringVar(&options.Title, "title", "", "human readable list title")
	set.Var(&seeds, "seed", "additional domain to include")
	set.BoolVar(&options.Confirm, "confirm", false, "start the browser for the printed URL")
	set.StringVar(&options.CatalogDir, "catalog-dir", "./catalog", "catalog directory")
	set.StringVar(&options.DataDir, "data-dir", "./data", "data directory")
	set.StringVar(&options.Browser, "browser", "", "path to the Chromium-family executable")
	if err := set.Parse(args); err != nil || set.NArg() != 0 || options.URL == "" {
		return discoverOptions{}, false
	}
	options.ManualSeeds = seeds.values
	return options, true
}

// repeatedFlag collects a flag that may appear more than once.
type repeatedFlag struct{ values []string }

func (f *repeatedFlag) String() string { return fmt.Sprint(f.values) }
func (f *repeatedFlag) Set(value string) error {
	if value == "" {
		return errors.New("empty value")
	}
	f.values = append(f.values, value)
	return nil
}

// discoverReport is the bounded stdout contract. It names hosts and decisions,
// never addresses: observed addresses live only in the observation store.
type discoverReport struct {
	URL               string                `json:"url"`
	Host              string                `json:"host"`
	RegistrableDomain string                `json:"registrable_domain"`
	PublicSuffix      string                `json:"public_suffix"`
	ICANNSuffix       bool                  `json:"icann_suffix"`
	ListID            string                `json:"list_id"`
	Confirmed         bool                  `json:"confirmed"`
	AcceptedHosts     []string              `json:"accepted_hosts,omitempty"`
	SeedDomains       []string              `json:"seed_domains,omitempty"`
	Candidates        []discovery.Candidate `json:"candidates,omitempty"`
	Requests          int                   `json:"requests,omitempty"`
	Sightings         int                   `json:"sightings,omitempty"`
	Relations         int                   `json:"relations,omitempty"`
	DraftPath         string                `json:"draft_path,omitempty"`
	ObservationError  string                `json:"observation_error,omitempty"`
	Hint              string                `json:"hint,omitempty"`
}

func runDiscover(ctx context.Context, stdout io.Writer, logger *slog.Logger, options discoverOptions, deps runtimeDeps) int {
	started := time.Now().UTC()
	browserPath := options.Browser
	if browserPath == "" {
		browserPath = deps.DiscoveryBrowserPath
	}
	if browserPath == "" {
		browserPath = discovery.DefaultBrowserPath()
	}
	request := discovery.Request{URL: options.URL, ListID: options.ListID, Title: options.Title, ManualSeeds: options.ManualSeeds, Confirm: options.Confirm}

	dependencies := discovery.Deps{
		Browser: discovery.BrowserOptions{
			ExecPath:          browserPath,
			Resolver:          deps.FeedResolver,
			Dialer:            deps.DiscoveryDialer,
			HostResolverRules: deps.DiscoveryHostRules,
			TrustedSPKI:       deps.DiscoveryTrustedSPKI,
		},
		Now: deps.Now,
	}
	if options.Confirm {
		root, err := filesystem.EnsureDataRoot(options.DataDir)
		if err != nil {
			logResult(logger, "discover", "", "", "failed", 0, started, "data_root_invalid")
			return 2
		}
		store, err := sqlite.Open(ctx, root)
		if err != nil {
			logResult(logger, "discover", "", "", "failed", 0, started, "database_unavailable")
			return 1
		}
		defer store.Close()
		dependencies.Observe = func(observeCtx context.Context, definition domain.ListDefinition) (discovery.Observation, error) {
			// The written draft is reloaded so the observation uses the catalog's
			// own revisions and the same bounded DNS cycle the product already
			// runs. There is no discovery-only storage path.
			catalog, loadErr := catalogyaml.Load(observeCtx, options.CatalogDir)
			if loadErr != nil {
				return discovery.Observation{}, loadErr
			}
			loaded, found := catalog.List(definition.ID)
			if !found {
				return discovery.Observation{}, fmt.Errorf("written draft %q did not load", definition.ID)
			}
			summary, err := application.RefreshList(observeCtx, loaded, domain.RawJSONTargetDefinition(), builtinSources(deps), store, application.ClockFunc(deps.Now))
			if err != nil && !errors.Is(err, application.ErrSourceDegraded) {
				return discovery.Observation{Sightings: summary.Sightings}, err
			}
			return discovery.Observation{Sightings: summary.Sightings}, nil
		}
		dependencies.Write = func(writeCtx context.Context, definition domain.ListDefinition) (string, error) {
			return catalogyaml.WriteLocalDraft(writeCtx, options.CatalogDir, definition)
		}
	}

	result, err := discovery.Run(ctx, request, dependencies)
	report := discoverReport{
		URL:               result.Target.URL,
		Host:              result.Target.Host,
		RegistrableDomain: result.Target.RegistrableDomain,
		PublicSuffix:      result.Target.PublicSuffix,
		ICANNSuffix:       result.Target.ICANNSuffix,
		ListID:            result.ListID,
		Confirmed:         result.Confirmed,
		AcceptedHosts:     result.Draft.AcceptedHosts,
		SeedDomains:       result.Draft.SeedDomains,
		Candidates:        result.Draft.Candidates,
		Requests:          result.Page.Requests,
		Sightings:         result.Observed.Sightings,
		Relations:         len(result.Draft.Relations),
		DraftPath:         result.Path,
		ObservationError:  result.ObservationError,
	}
	if errors.Is(err, discovery.ErrConfirmationRequired) {
		report.Hint = "review the URL above, then repeat the command with --confirm"
		if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
			logResult(logger, "discover", report.ListID, "", "failed", 0, started, "output_failed")
			return 1
		}
		logResult(logger, "discover", report.ListID, "", "success", 0, started, "confirmation_required")
		return 0
	}
	if err != nil {
		// A local command that reports only a code is not operable; the reason
		// stays on the operator's own machine.
		logger.Warn("discover failed", "operation", "discover", "error", err.Error())
		logResult(logger, "discover", report.ListID, "", "failed", 0, started, discoverErrorCode(err))
		return 1
	}
	if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
		logResult(logger, "discover", report.ListID, "", "failed", report.Sightings, started, "output_failed")
		return 1
	}
	if report.ObservationError != "" {
		logger.Warn("discover warning", "operation", "discover", "code", "first_observation_failed", "error", report.ObservationError)
		logResult(logger, "discover", report.ListID, "", "success", report.Sightings, started, "first_observation_failed")
		return 0
	}
	logResult(logger, "discover", report.ListID, "", "success", report.Sightings, started, "")
	return 0
}

func discoverErrorCode(err error) string {
	switch {
	case errors.Is(err, discovery.ErrInvalidTarget):
		return "target_invalid"
	case errors.Is(err, discovery.ErrNoRegistrableDomain):
		return "target_has_no_registrable_domain"
	case errors.Is(err, discovery.ErrUnsafeDestination):
		return "target_destination_refused"
	case errors.Is(err, discovery.ErrBrowserUnavailable):
		return "browser_unavailable"
	case errors.Is(err, discovery.ErrPageLoadFailed):
		return "page_load_failed"
	case errors.Is(err, discovery.ErrUnsafeSeed):
		return "seed_invalid"
	case errors.Is(err, discovery.ErrNoUsableEvidence):
		return "no_usable_evidence"
	case errors.Is(err, catalogyaml.ErrDraftExists):
		return "draft_exists"
	default:
		return "discover_failed"
	}
}
