package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/plugin"
	"github.com/Muratovnik/routevane/internal/renderers/amnezia"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
	"github.com/Muratovnik/routevane/internal/renderers/mikrotik"
	"github.com/Muratovnik/routevane/internal/renderers/openwrtnftset"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
	"github.com/Muratovnik/routevane/internal/sources/dns"
	"github.com/Muratovnik/routevane/internal/sources/httpfeed"
	wire "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

// adapters is the resolved set of renderers and sources one command runs with.
// Built-ins are always present; installed plugins are added to the same maps, so
// nothing downstream can tell them apart or skip a step for them.
type adapters struct {
	renderers application.RendererRegistry
	sources   application.SourceRegistry
	// externalSourceRevisions binds a catalog's declared implementation revision
	// to the manifest of the source process that actually started.
	externalSourceRevisions map[domain.SourceType]string
	// plugins are the live processes this set depends on. Close ends them.
	plugins []*plugin.Client
}

// Close ends every plugin process this set started.
func (a adapters) Close() {
	for _, client := range a.plugins {
		_ = client.Close()
	}
}

// builtinRenderers is the one place a renderer id is bound to a built-in format.
func builtinRenderers() application.RendererRegistry {
	return application.RendererRegistry{
		keenetic.ID:      keenetic.Renderer{},
		keeneticdns.ID:   keeneticdns.Renderer{},
		openwrtnftset.ID: openwrtnftset.Renderer{},
		mikrotik.ID:      mikrotik.Renderer{},
		amnezia.ID:       amnezia.Renderer{},
		singbox.ID:       singbox.Renderer{},
		rawjson.ID:       rawjson.Renderer{},
	}
}

// builtinSources is the one place a source type is bound to a built-in cycle.
func builtinSources(deps runtimeDeps) application.SourceRegistry {
	return application.SourceRegistry{
		domain.SourceDNS:  dns.Source{Observer: dns.NewObserver(deps.Resolver)},
		domain.SourceHTTP: httpfeed.Source{Observer: httpfeed.NewObserver(deps.FeedResolver, deps.FeedDialer, deps.FeedOptions)},
	}
}

// resolveAdapters starts the installed plugins, if any, and merges them with the
// built-ins.
//
// A build with no plugin directory behaves exactly as one without plugins: the
// built-ins are complete on their own, and the plugin host is never started.
// An unusable installation is an error rather than a silent omission, because a
// plugin the operator installed and cannot see working is worse than a refusal.
func resolveAdapters(ctx context.Context, deps runtimeDeps, logger *slog.Logger) (adapters, error) {
	resolved := adapters{renderers: builtinRenderers(), sources: builtinSources(deps), externalSourceRevisions: map[domain.SourceType]string{}}
	installed, err := plugin.Discover(deps.PluginsDir)
	if err != nil {
		return adapters{}, err
	}
	for _, entry := range installed {
		client, err := plugin.Start(ctx, entry, plugin.Options{HostVersion: version, Logger: logger})
		if err != nil {
			resolved.Close()
			return adapters{}, fmt.Errorf("start plugin %q: %w", entry.Manifest.Name, err)
		}
		resolved.plugins = append(resolved.plugins, client)
		switch entry.Manifest.Kind {
		case wire.KindRenderer:
			renderer, err := plugin.NewRenderer(client)
			if err != nil {
				resolved.Close()
				return adapters{}, fmt.Errorf("adapt plugin %q: %w", entry.Manifest.Name, err)
			}
			if _, taken := resolved.renderers[renderer.ID()]; taken {
				// A plugin never replaces a built-in format: the built-in is what
				// the product's own tests cover.
				resolved.Close()
				return adapters{}, fmt.Errorf("plugin %q claims renderer id %q, which is already provided", entry.Manifest.Name, renderer.ID())
			}
			resolved.renderers[renderer.ID()] = renderer
		case wire.KindSource:
			source, err := plugin.NewSource(client, 0)
			if err != nil {
				resolved.Close()
				return adapters{}, fmt.Errorf("adapt plugin %q: %w", entry.Manifest.Name, err)
			}
			if _, taken := resolved.sources[source.Type()]; taken {
				resolved.Close()
				return adapters{}, fmt.Errorf("plugin %q claims source type %q, which is already provided", entry.Manifest.Name, source.Type())
			}
			resolved.sources[source.Type()] = source
			resolved.externalSourceRevisions[source.Type()] = source.Revision()
		default:
			resolved.Close()
			return adapters{}, fmt.Errorf("plugin %q has an unsupported kind %q", entry.Manifest.Name, entry.Manifest.Kind)
		}
		logger.Info("plugin ready", "operation", "plugins", "plugin", entry.Manifest.Name, "version", entry.Manifest.Version,
			"kind", string(entry.Manifest.Kind), "protocol", client.ProtocolVersion())
	}
	if err := resolved.renderers.Validate(); err != nil {
		resolved.Close()
		return adapters{}, err
	}
	return resolved, nil
}

// validateSourceDefinitions proves that every external source a catalog names
// is installed and implements the exact revision the operator reviewed. Built-
// in source revisions are already bound by the strict catalog loader.
func (a adapters) validateSourceDefinitions(definitions ...domain.ServiceDefinition) error {
	for _, definition := range definitions {
		for _, source := range definition.Sources {
			if source.Type == domain.SourceDNS || source.Type == domain.SourceHTTP {
				continue
			}
			revision, installed := a.externalSourceRevisions[source.Type]
			if !installed {
				return fmt.Errorf("external source type %q required by service %q is not installed", source.Type, definition.ID)
			}
			if revision != source.ImplementationRevision {
				return fmt.Errorf("external source type %q revision %q does not match catalog revision %q", source.Type, revision, source.ImplementationRevision)
			}
		}
	}
	return nil
}
