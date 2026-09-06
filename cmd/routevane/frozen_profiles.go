package main

import (
	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/renderers/amnezia"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
	"github.com/Muratovnik/routevane/internal/renderers/mikrotik"
	"github.com/Muratovnik/routevane/internal/renderers/openwrtnftset"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

// catalogTargets returns the catalog targets this build can actually serve. A
// target is dropped when its renderer is missing, when its profile key does not
// match that renderer, when it declares a capability the renderer cannot render,
// or when a device-specific expectation refuses it. Dropping is deliberate: an
// unserviceable target must never be selectable.
//
// The registry is passed in rather than rebuilt so a target backed by an
// installed plugin is judged by exactly the same rules as a built-in one.
func catalogTargets(catalog catalogyaml.Catalog, registry application.RendererRegistry) map[string]domain.TargetProfile {
	targets := make(map[string]domain.TargetProfile, len(catalog.Targets))
	for id, target := range catalog.Targets {
		if id != target.ID {
			continue
		}
		renderer, err := registry.For(target)
		if err != nil {
			continue
		}
		if !renderableTarget(target, renderer) {
			continue
		}
		if frozen, pinned := frozenProfiles[id]; pinned && !frozen(target) {
			continue
		}
		targets[id] = target
	}
	return targets
}

// renderableTarget refuses a catalog file that claims a capability the format
// cannot express. Without this a target could promise domain routing to a
// renderer that only emits IPv4 routes.
func renderableTarget(target domain.TargetProfile, renderer application.Renderer) bool {
	supported := make(map[domain.RuleKind]struct{}, 6)
	for _, kind := range renderer.SupportedRuleKinds() {
		supported[kind] = struct{}{}
	}
	claimed := make([]domain.RuleKind, 0, 6)
	constraints := target.Constraints
	if constraints.SupportsDomainExact {
		claimed = append(claimed, domain.RuleDomainExact)
	}
	if constraints.SupportsDomainSuffix {
		claimed = append(claimed, domain.RuleDomainSuffix)
	}
	if constraints.SupportsIPv4 {
		claimed = append(claimed, domain.RuleIPv4)
	}
	if constraints.SupportsIPv6 {
		claimed = append(claimed, domain.RuleIPv6)
	}
	if constraints.SupportsPrefixes && constraints.SupportsIPv4 {
		claimed = append(claimed, domain.RulePrefix4)
	}
	if constraints.SupportsPrefixes && constraints.SupportsIPv6 {
		claimed = append(claimed, domain.RulePrefix6)
	}
	if len(claimed) == 0 {
		return false
	}
	for _, kind := range claimed {
		if _, ok := supported[kind]; !ok {
			return false
		}
	}
	return true
}

// frozenProfiles pins device-specific truth a catalog file must not widen.
var frozenProfiles = map[string]func(domain.TargetProfile) bool{
	"keenetic":     isFrozenKeeneticProfile,
	"keenetic-dns": isFrozenKeeneticDNSProfile,
	"singbox":      isFrozenSingboxProfile,
	"openwrt":      isFrozenOpenWrtProfile,
	"mikrotik":     isFrozenMikrotikProfile,
	"amnezia":      isFrozenAmneziaProfile,
}

func isFrozenKeeneticProfile(target domain.TargetProfile) bool {
	constraints := target.Constraints
	return target.ID == "keenetic" && target.ProfileKey == keenetic.Version && target.RendererID == keenetic.ID && len(target.RendererOptions) == 0 &&
		!constraints.SupportsDomainExact && !constraints.SupportsDomainSuffix && !constraints.SupportsDynamicDNSSet && constraints.SupportsIPv4 && !constraints.SupportsIPv6 && constraints.SupportsPrefixes &&
		constraints.MaxRules == keenetic.MaxLines && constraints.MaxArtifactSize == keenetic.MaxArtifactSize
}

// isFrozenKeeneticDNSProfile pins what an FQDN object group can hold. The
// device expands the subdomains of a listed name itself, so exact-domain
// support would be a promise the group cannot keep, and the two list bounds
// must match the renderer or a split would be sized against a limit nothing
// enforces.
func isFrozenKeeneticDNSProfile(target domain.TargetProfile) bool {
	constraints := target.Constraints
	return target.ID == "keenetic-dns" && target.ProfileKey == keeneticdns.Version && target.RendererID == keeneticdns.ID && len(target.RendererOptions) == 0 &&
		!constraints.SupportsDomainExact && constraints.SupportsDomainSuffix && constraints.SupportsDynamicDNSSet &&
		constraints.SupportsIPv4 && constraints.SupportsIPv6 && constraints.SupportsPrefixes &&
		constraints.MaxRules == keeneticdns.MaxGroups*keeneticdns.MaxEntriesPerGroup &&
		constraints.MaxArtifactSize == keeneticdns.MaxArtifactSize &&
		constraints.MaxEntriesPerList == keeneticdns.MaxEntriesPerGroup && constraints.MaxLists == keeneticdns.MaxGroups
}

func isFrozenSingboxProfile(target domain.TargetProfile) bool {
	constraints := target.Constraints
	return target.ID == "singbox" && target.ProfileKey == singbox.Version && target.RendererID == singbox.ID && len(target.RendererOptions) == 0 &&
		constraints.SupportsDomainExact && constraints.SupportsDomainSuffix && !constraints.SupportsDynamicDNSSet && constraints.SupportsIPv4 && constraints.SupportsIPv6 && constraints.SupportsPrefixes &&
		constraints.MaxRules == singbox.MaxEntries && constraints.MaxArtifactSize == singbox.MaxArtifactSize
}

// isFrozenOpenWrtProfile pins the one capability shape this format can honor.
// The device resolves the listed domains itself, so a catalog file that claimed
// an address capability would promise coverage the fragment cannot carry, and
// exact-domain support would silently widen every rule to its subdomains.
func isFrozenOpenWrtProfile(target domain.TargetProfile) bool {
	constraints := target.Constraints
	return target.ID == "openwrt" && target.ProfileKey == openwrtnftset.Version && target.RendererID == openwrtnftset.ID && len(target.RendererOptions) == 0 &&
		!constraints.SupportsDomainExact && constraints.SupportsDomainSuffix && constraints.SupportsDynamicDNSSet &&
		!constraints.SupportsIPv4 && !constraints.SupportsIPv6 && !constraints.SupportsPrefixes &&
		constraints.MaxRules == openwrtnftset.MaxLines && constraints.MaxArtifactSize == openwrtnftset.MaxArtifactSize
}

// isFrozenMikrotikProfile pins what an address-list entry can hold. A suffix is
// not an address-list value, so a catalog file that claimed one would promise
// coverage the script cannot carry.
func isFrozenMikrotikProfile(target domain.TargetProfile) bool {
	constraints := target.Constraints
	return target.ID == "mikrotik" && target.ProfileKey == mikrotik.Version && target.RendererID == mikrotik.ID && len(target.RendererOptions) == 0 &&
		constraints.SupportsDomainExact && !constraints.SupportsDomainSuffix && !constraints.SupportsDynamicDNSSet &&
		constraints.SupportsIPv4 && constraints.SupportsIPv6 && constraints.SupportsPrefixes &&
		constraints.MaxRules == mikrotik.MaxLines && constraints.MaxArtifactSize == mikrotik.MaxArtifactSize
}

// isFrozenAmneziaProfile pins what the client will actually route. It collects
// IPv4 answers when it resolves a site and resolves the one name it was given,
// so a catalog file claiming IPv6 or suffix support would promise coverage the
// client silently drops.
func isFrozenAmneziaProfile(target domain.TargetProfile) bool {
	constraints := target.Constraints
	return target.ID == "amnezia" && target.ProfileKey == amnezia.Version && target.RendererID == amnezia.ID && len(target.RendererOptions) == 0 &&
		constraints.SupportsDomainExact && !constraints.SupportsDomainSuffix && !constraints.SupportsDynamicDNSSet &&
		constraints.SupportsIPv4 && !constraints.SupportsIPv6 && constraints.SupportsPrefixes &&
		constraints.MaxRules == amnezia.MaxEntries && constraints.MaxArtifactSize == amnezia.MaxArtifactSize
}
