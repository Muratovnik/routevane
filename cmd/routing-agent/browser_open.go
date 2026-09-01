package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Browser opening is an optional local convenience; failure never stops serving.
// The origin comes from our bound listener, not a user-supplied URL or shell.
func openSystemBrowser(ctx context.Context, origin string) error {
	if !localBrowserOrigin(origin) {
		return fmt.Errorf("browser origin must be local")
	}
	var command string
	var arguments []string
	switch runtime.GOOS {
	case "windows":
		command, arguments = "rundll32.exe", []string{"url.dll,FileProtocolHandler", origin}
	case "darwin":
		command, arguments = "open", []string{origin}
	default:
		command, arguments = "xdg-open", []string{origin}
	}
	// #nosec G204 -- Fixed platform opener, no shell; the sole URL is a validated numeric loopback origin.
	return exec.CommandContext(ctx, command, arguments...).Run()
}

func localBrowserOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port <= 65535 && u.Host == net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) && !u.ForceQuery
}

func openBrowserWhenReady(parent context.Context, origin string, open func(context.Context, string) error) error {
	if !localBrowserOrigin(origin) {
		return fmt.Errorf("browser origin must be local")
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		Timeout:       time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/", nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			digest := strings.TrimPrefix(response.Header.Get("X-Routevane-UI-Digest"), "sha256-")
			decoded, decodeErr := hex.DecodeString(digest)
			ready := response.StatusCode == http.StatusOK && decodeErr == nil && len(decoded) == 32 &&
				strings.HasPrefix(response.Header.Get("X-Routevane-UI-Digest"), "sha256-")
			if err := response.Body.Close(); err != nil {
				return err
			}
			if ready {
				if err := ctx.Err(); err != nil {
					return err
				}
				return open(ctx, origin)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
