package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/discovery"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

// SessionSourceID is the provenance identity of a learning session. Session
// relations are stored under it so they are inspectable, and because it is not a
// catalog source its revision never matches an active one: session provenance
// can therefore never influence routing on its own.
const SessionSourceID = "learning-session"

// SessionValidity is how long session provenance stays fresh.
const SessionValidity = 30 * 24 * time.Hour

// MaxScenarioBytes bounds a user-supplied scenario file.
const MaxScenarioBytes = 64 << 10

type learnOptions struct {
	Scenario    string
	HAR         string
	URL         string
	ServiceID   string
	Title       string
	ManualSeeds []string
	Confirm     bool
	CatalogDir  string
	DataDir     string
	Browser     string
}

func parseLearn(args []string) (learnOptions, bool) {
	options := learnOptions{}
	seeds := repeatedFlag{}
	set := flag.NewFlagSet("learn", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&options.Scenario, "scenario", "", "exploration scenario file")
	set.StringVar(&options.HAR, "har", "", "HAR archive to import")
	set.StringVar(&options.URL, "url", "", "site the archive belongs to")
	set.StringVar(&options.ServiceID, "service-id", "", "local service identity")
	set.StringVar(&options.Title, "title", "", "human readable service title")
	set.Var(&seeds, "seed", "additional domain to include")
	set.BoolVar(&options.Confirm, "confirm", false, "run the session or write the draft")
	set.StringVar(&options.CatalogDir, "catalog-dir", "./catalog", "catalog directory")
	set.StringVar(&options.DataDir, "data-dir", "./data", "data directory")
	set.StringVar(&options.Browser, "browser", "", "path to the Chromium-family executable")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return learnOptions{}, false
	}
	// Exactly one evidence source: a scenario to run, or an archive to import.
	if (options.Scenario == "") == (options.HAR == "") {
		return learnOptions{}, false
	}
	if options.HAR != "" && options.URL == "" {
		return learnOptions{}, false
	}
	options.ManualSeeds = seeds.values
	return options, true
}

// learnReport is the bounded stdout contract. It names hosts, components, and
// decisions; observed addresses stay in the local database.
type learnReport struct {
	URL               string                `json:"url"`
	RegistrableDomain string                `json:"registrable_domain"`
	ServiceID         string                `json:"service_id"`
	Confirmed         bool                  `json:"confirmed"`
	Steps             []string              `json:"steps,omitempty"`
	Exercised         []string              `json:"exercised_components,omitempty"`
	Components        []string              `json:"components,omitempty"`
	AcceptedHosts     []string              `json:"accepted_hosts,omitempty"`
	SeedDomains       []string              `json:"seed_domains,omitempty"`
	Dependencies      []discovery.Candidate `json:"dependencies,omitempty"`
	Relations         int                   `json:"relations,omitempty"`
	Requests          int                   `json:"requests,omitempty"`
	Sightings         int                   `json:"sightings,omitempty"`
	DraftPath         string                `json:"draft_path,omitempty"`
	EvidencePath      string                `json:"evidence_path,omitempty"`
	ObservationError  string                `json:"observation_error,omitempty"`
	Hint              string                `json:"hint,omitempty"`
}

func runLearn(ctx context.Context, stdout io.Writer, logger *slog.Logger, options learnOptions, deps runtimeDeps) int {
	started := time.Now().UTC()
	scenario, exercised, err := learnScenario(options)
	if err != nil {
		logger.Warn("learn failed", "operation", "learn", "error", err.Error())
		logResult(logger, "learn", options.ServiceID, "", "failed", 0, started, learnErrorCode(err))
		return 2
	}
	target, err := discovery.NormalizeTarget(scenario.Target)
	if err != nil {
		logger.Warn("learn failed", "operation", "learn", "error", err.Error())
		logResult(logger, "learn", options.ServiceID, "", "failed", 0, started, learnErrorCode(err))
		return 2
	}
	serviceID := options.ServiceID
	if serviceID == "" {
		serviceID, err = discovery.ServiceIDFor(target.RegistrableDomain)
		if err != nil {
			logger.Warn("learn failed", "operation", "learn", "error", err.Error())
			logResult(logger, "learn", options.ServiceID, "", "failed", 0, started, learnErrorCode(err))
			return 2
		}
	}
	report := learnReport{URL: target.URL, RegistrableDomain: target.RegistrableDomain, ServiceID: serviceID, Steps: scenarioStepIDs(scenario), Exercised: exercised}
	if !options.Confirm {
		report.Hint = "review the URL and steps above, then repeat the command with --confirm"
		if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
			logResult(logger, "learn", serviceID, "", "failed", 0, started, "output_failed")
			return 1
		}
		logResult(logger, "learn", serviceID, "", "success", 0, started, "confirmation_required")
		return 0
	}

	evidence, err := learnEvidence(ctx, options, scenario, target, deps)
	if err != nil {
		logger.Warn("learn failed", "operation", "learn", "error", err.Error())
		logResult(logger, "learn", serviceID, "", "failed", 0, started, learnErrorCode(err))
		return 1
	}
	if options.HAR != "" {
		// An archive carries its own exploration: the components its pages
		// labelled are exactly the areas it exercised.
		exercised = evidence.Steps
		report.Exercised = exercised
		report.Steps = exercised
	}
	draft, err := discovery.BuildLearnedDraft(discovery.LearnedDraftRequest{
		Target:      target,
		ServiceID:   serviceID,
		Title:       options.Title,
		Evidence:    evidence,
		Exercised:   exercised,
		ManualSeeds: options.ManualSeeds,
	})
	if err != nil {
		logger.Warn("learn failed", "operation", "learn", "error", err.Error())
		logResult(logger, "learn", serviceID, "", "failed", 0, started, learnErrorCode(err))
		return 1
	}
	report.Confirmed = true
	report.AcceptedHosts = draft.AcceptedHosts
	report.SeedDomains = draft.SeedDomains
	report.Dependencies = draft.Candidates
	report.Requests = evidence.Requests
	for _, component := range draft.Definition.Components {
		report.Components = append(report.Components, component.ID)
	}

	draftPath, err := catalogyaml.WriteLocalDraft(ctx, options.CatalogDir, draft.Definition)
	if err != nil {
		logger.Warn("learn failed", "operation", "learn", "error", err.Error())
		logResult(logger, "learn", serviceID, "", "failed", 0, started, learnErrorCode(err))
		return 1
	}
	report.DraftPath = draftPath

	root, err := filesystem.EnsureDataRoot(options.DataDir)
	if err != nil {
		logResult(logger, "learn", serviceID, "", "failed", 0, started, "data_root_invalid")
		return 1
	}
	evidencePath, err := filesystem.WriteSessionEvidence(ctx, root, serviceID, deps.Now().UTC(), evidence)
	if err != nil {
		logger.Warn("learn failed", "operation", "learn", "error", err.Error())
		logResult(logger, "learn", serviceID, "", "failed", 0, started, "evidence_write_failed")
		return 1
	}
	report.EvidencePath = evidencePath

	store, err := sqlite.Open(ctx, root)
	if err != nil {
		logResult(logger, "learn", serviceID, "", "failed", 0, started, "database_unavailable")
		return 1
	}
	defer store.Close()

	relations, err := discovery.Relations(evidence, serviceID, SessionSourceID, scenarioRevision(scenario), deps.Now().UTC(), SessionValidity)
	if err != nil {
		logger.Warn("learn failed", "operation", "learn", "error", err.Error())
		logResult(logger, "learn", serviceID, "", "failed", 0, started, learnErrorCode(err))
		return 1
	}
	if len(relations) > 0 {
		// Both timestamps come from the injected clock. Mixing wall-clock time
		// with it would make the cycle's own ordering depend on when the command
		// happened to run.
		cycleAt := deps.Now().UTC()
		cycle := application.SuccessCycle{
			ServiceID: serviceID, SourceID: SessionSourceID, SourceRevision: scenarioRevision(scenario),
			StartedAt: cycleAt, CompletedAt: cycleAt, Relations: relations,
		}
		if err := store.ApplySuccess(ctx, cycle); err != nil {
			logger.Warn("learn failed", "operation", "learn", "error", err.Error())
			logResult(logger, "learn", serviceID, "", "failed", 0, started, "relations_write_failed")
			return 1
		}
	}
	report.Relations = len(relations)

	// The observation cycle runs against the definition the catalog loaded, so
	// it uses the catalog's own revisions.
	catalog, err := catalogyaml.Load(ctx, options.CatalogDir)
	if err == nil {
		if loaded, found := catalog.Service(serviceID); found {
			summary, refreshErr := application.RefreshService(ctx, loaded, domain.RawJSONTargetProfile(), builtinSources(deps), store, application.ClockFunc(deps.Now))
			report.Sightings = summary.Sightings
			if refreshErr != nil && !errors.Is(refreshErr, application.ErrSourceDegraded) {
				report.ObservationError = refreshErr.Error()
			}
		}
	} else {
		report.ObservationError = err.Error()
	}

	if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
		logResult(logger, "learn", serviceID, "", "failed", report.Sightings, started, "output_failed")
		return 1
	}
	if report.ObservationError != "" {
		logger.Warn("learn warning", "operation", "learn", "code", "first_observation_failed", "error", report.ObservationError)
		logResult(logger, "learn", serviceID, "", "success", report.Sightings, started, "first_observation_failed")
		return 0
	}
	logResult(logger, "learn", serviceID, "", "success", report.Sightings, started, "")
	return 0
}

// learnScenario resolves the evidence description. An imported archive is
// modelled as a scenario with no steps to run, so both paths share one report.
func learnScenario(options learnOptions) (discovery.Scenario, []string, error) {
	if options.HAR != "" {
		return discovery.Scenario{Target: options.URL}, nil, nil
	}
	payload, err := readScenarioFile(options.Scenario, MaxScenarioBytes)
	if err != nil {
		return discovery.Scenario{}, nil, err
	}
	var scenario discovery.Scenario
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	decoder.KnownFields(true)
	if err := decoder.Decode(&scenario); err != nil {
		return discovery.Scenario{}, nil, fmt.Errorf("%w: %v", discovery.ErrInvalidScenario, err)
	}
	if err := scenario.Validate(); err != nil {
		return discovery.Scenario{}, nil, err
	}
	return scenario, scenario.ExercisedComponents(), nil
}

func learnEvidence(ctx context.Context, options learnOptions, scenario discovery.Scenario, target discovery.Target, deps runtimeDeps) (discovery.SessionEvidence, error) {
	if options.HAR != "" {
		payload, err := readScenarioFile(options.HAR, discovery.MaxHARBytes)
		if err != nil {
			return discovery.SessionEvidence{}, err
		}
		return discovery.ImportHAR(target, payload)
	}
	browserPath := options.Browser
	if browserPath == "" {
		browserPath = deps.DiscoveryBrowserPath
	}
	if browserPath == "" {
		browserPath = discovery.DefaultBrowserPath()
	}
	return discovery.RunScenario(ctx, scenario, discovery.BrowserOptions{
		ExecPath:          browserPath,
		Resolver:          deps.FeedResolver,
		Dialer:            deps.DiscoveryDialer,
		HostResolverRules: deps.DiscoveryHostRules,
		TrustedSPKI:       deps.DiscoveryTrustedSPKI,
	})
}

// scenarioRevision binds stored session provenance to the exact exploration that
// produced it, so replaying a changed scenario never mixes two meanings.
func scenarioRevision(scenario discovery.Scenario) string {
	encoded, err := json.Marshal(scenario)
	if err != nil {
		return "unknown"
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func scenarioStepIDs(scenario discovery.Scenario) []string {
	ids := make([]string, 0, len(scenario.Steps))
	for _, step := range scenario.Steps {
		ids = append(ids, step.ID+":"+step.Component)
	}
	return ids
}

// readScenarioFile reads an operator-supplied input through the root-scoped
// filesystem boundary and reports a scenario-shaped error.
func readScenarioFile(path string, maxBytes int) ([]byte, error) {
	payload, err := filesystem.ReadBoundedFile(path, int64(maxBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", discovery.ErrInvalidScenario, path, err)
	}
	return payload, nil
}

func learnErrorCode(err error) string {
	switch {
	case errors.Is(err, discovery.ErrInvalidScenario):
		return "scenario_invalid"
	case errors.Is(err, discovery.ErrInvalidHAR):
		return "har_invalid"
	case errors.Is(err, discovery.ErrHARTooBig):
		return "har_too_large"
	default:
		return discoverErrorCode(err)
	}
}
