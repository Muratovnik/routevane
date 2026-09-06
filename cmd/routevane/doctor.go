package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/processlock"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
)

type doctorCheck struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Code    string `json:"code"`
	Locked  *bool  `json:"locked,omitempty"`
}

type doctorReport struct {
	Healthy bool          `json:"healthy"`
	Checks  []doctorCheck `json:"checks"`
}

func runDoctor(ctx context.Context, stdout io.Writer, logger *slog.Logger, options doctorOptions) int {
	started := time.Now().UTC()
	report := doctorReport{Healthy: true, Checks: make([]doctorCheck, 0, 9)}
	if _, err := catalogyaml.Load(ctx, options.CatalogDir); err != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, doctorCheck{Name: "catalog", Healthy: false, Code: "catalog_invalid"})
	} else {
		report.Checks = append(report.Checks, doctorCheck{Name: "catalog", Healthy: true, Code: "ok"})
	}
	root, rootErr := filesystem.ResolvePrivateDataRoot(options.DataDir)
	if rootErr != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, doctorCheck{Name: "data_root", Healthy: false, Code: "data_root_missing_or_unsafe"})
	} else {
		report.Checks = append(report.Checks, doctorCheck{Name: "data_root", Healthy: true, Code: "ok"})
		inspection := sqlite.InspectExisting(ctx, root)
		if !inspection.Healthy {
			report.Healthy = false
		}
		for _, check := range inspection.Checks {
			report.Checks = append(report.Checks, doctorCheck{Name: "sqlite_" + check.Name, Healthy: check.Healthy, Code: check.Code})
		}
		lockStatus := processlock.Inspect(root)
		if !lockStatus.Healthy {
			report.Healthy = false
		}
		locked := lockStatus.Locked
		report.Checks = append(report.Checks, doctorCheck{Name: "scheduler_lock", Healthy: lockStatus.Healthy, Code: lockStatus.Code, Locked: &locked})
		if err := filesystem.InspectArtifactTree(root); err != nil {
			report.Healthy = false
			report.Checks = append(report.Checks, doctorCheck{Name: "artifacts", Healthy: false, Code: "artifact_tree_unsafe"})
		} else {
			report.Checks = append(report.Checks, doctorCheck{Name: "artifacts", Healthy: true, Code: "ok"})
		}
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		logResult(logger, "doctor", "", "", "failed", 0, started, "output_failed")
		return 1
	}
	if !report.Healthy {
		logResult(logger, "doctor", "", "", "failed", 0, started, "doctor_unhealthy")
		return 1
	}
	logResult(logger, "doctor", "", "", "success", len(report.Checks), started, "")
	return 0
}
