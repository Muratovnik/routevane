// Command static-source is an example external source.
//
// It answers from a list a plugin author controls rather than from the network,
// which keeps the example honest about what the contract requires: the host
// re-parses every value, so a plugin cannot introduce an unchecked address
// however it obtained one.
//
// Build it and install it next to its manifest:
//
//	go build -o static-source ./examples/plugins/static-source
//	sha256sum static-source            # put the digest in manifest.json
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	plugin "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

// answers maps a requested name to the values this example reports. A real
// source would fetch them; the shape of the answer is the same either way.
var answers = map[string][]string{
	"static.example.test": {"192.0.2.40", "192.0.2.41"},
	"edge.example.test":   {"198.51.100.0/24"},
}

type handler struct{ plugin.Unimplemented }

func (handler) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:            "example-static-source",
		Version:         "1.0.0",
		ProtocolVersion: plugin.ProtocolVersion,
		Kind:            plugin.KindSource,
		// The example needs no network, so it declares none. Declaring a
		// permission it does not use would misinform the operator.
		Permissions: []plugin.Permission{plugin.PermissionObserveNames},
		Source: &plugin.SourceManifest{
			Type:     "example-static",
			Revision: "example-static-v1",
		},
	}
}

func (handler) Observe(call plugin.ObserveCall) ([]plugin.Observation, error) {
	// Protocol v1 also carries the list identity as service_id. These static
	// answers do not vary by list, so the example only requires names and works
	// with both the published and current Go names for that wire field.
	if len(call.Names) == 0 {
		return nil, fmt.Errorf("observation request is incomplete")
	}
	observations := make([]plugin.Observation, 0, len(call.Names))
	seen := map[string]struct{}{}
	for _, name := range call.Names {
		values, known := answers[strings.ToLower(strings.TrimSpace(name))]
		if !known {
			// An unknown name contributes nothing rather than a guess.
			continue
		}
		for _, value := range values {
			if _, duplicate := seen[value]; duplicate {
				continue
			}
			seen[value] = struct{}{}
			observations = append(observations, plugin.Observation{Value: value, TTLSeconds: 300})
		}
	}
	if len(observations) == 0 {
		return nil, fmt.Errorf("no requested name is known to this source")
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].Value < observations[j].Value })
	// A diagnostic line goes to standard error; the host re-emits it as its own
	// structured record. Standard output carries protocol frames only.
	fmt.Fprintf(os.Stderr, "answered %d of %d names\n", len(observations), len(call.Names))
	return observations, nil
}

func main() {
	if err := plugin.Serve(handler{}); err != nil {
		fmt.Fprintln(os.Stderr, "example-static-source stopped:", err)
		os.Exit(1)
	}
}
