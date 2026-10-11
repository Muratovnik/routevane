package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
)

// A new desktop port comes from a band below the default ephemeral ranges of
// Windows, Linux and macOS and below the Kubernetes NodePort range (ADR 0048).
// Windows places its excluded port ranges and outgoing connections in the
// ephemeral range, so a port the OS assigns there can later stop binding.
const (
	desktopPortFirst    = 20000
	desktopPortCount    = 10000
	desktopPortAttempts = 32
)

var errDesktopPortBandUnavailable = errors.New("desktop port band unavailable")

// runDesktop shares the serving composition but gives its lifetime to the
// desktop parent's pipe. EOF also arrives when the parent crashes, including
// on Windows where SIGTERM does not provide a graceful child shutdown.
// The credential travels through that pipe, never through argv or a disk file.
func runDesktop(stdout io.Writer, logger *slog.Logger, args []string, deps runtimeDeps) int {
	set := flag.NewFlagSet("desktop", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	options := doctorOptions{}
	set.StringVar(&options.CatalogDir, "catalog-dir", "./catalog", "catalog directory")
	set.StringVar(&options.DataDir, "data-dir", "./data", "data directory")
	recoverPort := set.Bool("recover-desktop-port", false, "choose a new port if the saved desktop port cannot bind")
	if set.Parse(args) != nil || set.NArg() != 0 {
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
		DesktopToken:       token,
		RecoverDesktopPort: *recoverPort,
	}, deps)
}

// listenDesktopBand binds a uniformly random loopback port from the desktop
// band. A bind error moves on to another untried port; the failed saved port is
// never a candidate. The partial Fisher-Yates permutation keeps every attempt
// distinct however the injected source behaves.
func listenDesktopBand(listen func(string, string) (net.Listener, error), randomIndex func(int) int, failed int) (net.Listener, error) {
	moved := make(map[int]int)
	offset := func(position int) int {
		if value, ok := moved[position]; ok {
			return value
		}
		return position
	}
	remaining := desktopPortCount
	if failed >= desktopPortFirst && failed < desktopPortFirst+desktopPortCount {
		remaining--
		moved[failed-desktopPortFirst] = offset(remaining)
	}
	for range desktopPortAttempts {
		position := randomIndex(remaining)
		remaining--
		port := desktopPortFirst + offset(position)
		moved[position] = offset(remaining)
		if listener, err := listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			return listener, nil
		}
	}
	return nil, errDesktopPortBandUnavailable
}

// listenNewDesktopPort chooses a first or recovered desktop port. When the
// whole bounded search fails, the OS-assigned port still starts the desktop;
// the reason code says the port came from outside the band.
func listenNewDesktopPort(logger *slog.Logger, deps runtimeDeps, failed int) (net.Listener, error) {
	listener, err := listenDesktopBand(deps.Listen, deps.RandomIndex, failed)
	if err == nil {
		return listener, nil
	}
	logger.Warn("serve warning", "operation", "serve", "code", "desktop_port_band_unavailable")
	return deps.Listen("tcp4", "127.0.0.1:0")
}

// cryptoRandomIndex returns a uniform index in [0, n). crypto/rand.Reader does
// not fail on supported platforms, so the error branch only satisfies the API.
func cryptoRandomIndex(n int) int {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(value.Int64())
}
