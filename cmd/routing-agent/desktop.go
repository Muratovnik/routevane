package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"io"
	"log/slog"
)

// runDesktop shares the serving composition but gives its lifetime to the
// desktop parent's pipe. EOF also arrives when the parent crashes, including
// on Windows where SIGTERM does not provide a graceful child shutdown.
// The credential travels through that pipe, never through argv or a disk file.
func runDesktop(stdout io.Writer, logger *slog.Logger, args []string, deps runtimeDeps) int {
	options, ok := parseDoctor(args)
	if !ok {
		return 2
	}
	input := bufio.NewReaderSize(deps.DesktopInput, 128)
	line, err := input.ReadSlice('\n')
	if err != nil || len(line) != 65 {
		return 2
	}
	token := string(line[:64])
	if _, err := hex.DecodeString(token); err != nil {
		return 2
	}
	ctx, cancel := context.WithCancel(deps.Context)
	defer cancel()
	go func() {
		// There are no further commands. Either a byte or EOF ends the lease.
		_, _ = input.ReadByte()
		cancel()
	}()
	deps.Context = ctx
	return runServe(stdout, logger, serveOptions{
		Port: 0, CatalogDir: options.CatalogDir, DataDir: options.DataDir,
		DesktopToken: token,
	}, deps)
}
