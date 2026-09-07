package main

import (
	"io"
	"log/slog"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/processlock"
)

func runScheduler(stdout io.Writer, logger *slog.Logger, options runOptions, deps runtimeDeps) int {
	// Validate and snapshot the whole catalog before the scheduler mutates its
	// lock/data state or performs any DNS/SQLite work.
	if _, _, err := loadConfiguredList(deps.Context, options.commonOptions); err != nil {
		logResult(logger, "run", options.ListID, "", "failed", 0, time.Now().UTC(), "catalog_invalid")
		return 2
	}
	root, err := filesystem.EnsureDataRoot(options.DataDir)
	if err != nil {
		logResult(logger, "run", options.ListID, "", "failed", 0, time.Now().UTC(), "data_root_invalid")
		return 2
	}
	lock, err := processlock.Acquire(root)
	if err != nil {
		logResult(logger, "run", options.ListID, "", "failed", 0, time.Now().UTC(), "scheduler_lock_unavailable")
		return 1
	}
	defer lock.Close()
	ctx, cancel := deps.SignalContext(deps.Context)
	defer cancel()
	for {
		catalog, definition, err := loadConfiguredList(ctx, options.commonOptions)
		if err != nil {
			logResult(logger, "run", options.ListID, "", "failed", 0, time.Now().UTC(), "catalog_invalid")
		} else {
			started := time.Now().UTC()
			refreshLoaded(ctx, stdout, logger, options.commonOptions, catalog, definition, deps, started)
			buildOptions := buildOptions{ListIDs: []string{options.ListID}, CatalogDir: options.CatalogDir, DataDir: options.DataDir, Target: options.Target}
			buildLoaded(ctx, stdout, logger, buildOptions, catalog, []domain.ListDefinition{definition}, deps, time.Now().UTC())
		}
		if ctx.Err() != nil {
			return 0
		}
		delay := deps.NewDelayTicker(options.Interval)
		select {
		case <-ctx.Done():
			delay.Stop()
			return 0
		case <-delay.Chan():
			delay.Stop()
		}
	}
}
