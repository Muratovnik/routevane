package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// targetArtifactFields is the part of a target definition that decides what a
// built artifact is: which format, which renderer, what that renderer is asked
// to do, and the limits the planner and the renderer apply to it. An output
// records the revision of these fields when it is created and refuses to build
// once they differ (ADR 0054).
//
// Everything else a target carries is how the format is presented: its name,
// its kind on a screen and its installation instruction. Editing them changes
// no artifact, so it must not strand an output and its subscription link.
//
// A field added to TargetDefinition or TargetConstraints has to be classified
// here or in targetPresentationFields; a test refuses an unclassified one.
//
// Every value is omitted when it is zero, so a constraint added later with a
// zero default leaves the revision of every existing target unchanged, and a
// target that states it does change.
type targetArtifactFields struct {
	FormatKey       string                    `json:"format_key"`
	RendererID      string                    `json:"renderer_id"`
	RendererOptions []string                  `json:"renderer_options,omitempty"`
	Constraints     targetArtifactConstraints `json:"constraints"`
}

type targetArtifactConstraints struct {
	SupportsDomainExact   bool `json:"supports_domain_exact,omitempty"`
	SupportsDomainSuffix  bool `json:"supports_domain_suffix,omitempty"`
	SupportsDynamicDNSSet bool `json:"supports_dynamic_dns_set,omitempty"`
	SupportsIPv4          bool `json:"supports_ipv4,omitempty"`
	SupportsIPv6          bool `json:"supports_ipv6,omitempty"`
	SupportsPrefixes      bool `json:"supports_prefixes,omitempty"`
	MaxRules              int  `json:"max_rules,omitempty"`
	MaxArtifactSize       int  `json:"max_artifact_size,omitempty"`
	MaxEntriesPerList     int  `json:"max_entries_per_list,omitempty"`
	MaxLists              int  `json:"max_lists,omitempty"`
}

// targetPresentationFields names the TargetDefinition fields that are not part
// of the revision. ID is the key an output already stores and compares on its
// own; the rest is presentation.
var targetPresentationFields = []string{
	"ID",
	"Title",
	"TitleEN",
	"Kind",
	"ManualInstallationHint",
	"ManualInstallationSteps",
	"ManualInstallationHintEN",
	"ManualInstallationStepsEN",
}

// TargetRevision is the revision of one target's artifact fields. It depends
// on this target alone, so editing another target never changes it.
func TargetRevision(target TargetDefinition) string {
	fields := targetArtifactFields{
		FormatKey:  target.FormatKey,
		RendererID: target.RendererID,
		// A nil list and an empty one are the same statement: no options.
		RendererOptions: target.RendererOptions,
		// A conversion rather than a copy field by field: a constraint added to
		// TargetConstraints alone stops this from compiling.
		Constraints: targetArtifactConstraints(target.Constraints),
	}
	// The value holds strings, booleans and integers only, so encoding cannot
	// fail; a failure would be a programming error, not catalog input.
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic("encode target artifact fields: " + err.Error())
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
