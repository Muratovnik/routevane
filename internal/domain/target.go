package domain

type TargetConstraints struct {
	SupportsDomainExact   bool
	SupportsDomainSuffix  bool
	SupportsDynamicDNSSet bool
	SupportsIPv4          bool
	SupportsIPv6          bool
	SupportsPrefixes      bool
	MaxRules              int
	MaxArtifactSize       int
	// MaxEntriesPerList and MaxLists describe a target whose device holds named
	// lists rather than one flat file. A list over the entry bound is split into
	// sub-lists by the renderer; more lists than the device holds is a refusal,
	// because splitting makes room inside a format and never on the device.
	// Zero means the target has no such notion.
	MaxEntriesPerList int
	MaxLists          int
}

// RendererDescriptor is the format metadata of one renderer. It lives in the
// domain because renderer identity is already a domain concept through
// TargetProfile.RendererID, and because a renderer must not depend on the
// application layer that consumes it.
type RendererDescriptor struct {
	ID            string
	Version       string
	ContentType   string
	FileExtension string
}

// IsValid reports whether a descriptor can be published and served.
func (d RendererDescriptor) IsValid() bool {
	if ValidateSlug(d.ID) != nil || ValidateSlug(d.Version) != nil || d.ContentType == "" || d.FileExtension == "" {
		return false
	}
	for i := 0; i < len(d.FileExtension); i++ {
		c := d.FileExtension[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// TargetKind separates what receives the artifact: a router the file is
// installed on, or an application on the operator's machine that reads it.
// The catalog states it because nothing else can: the renderer id names a
// format, not the thing that consumes it.
const (
	TargetKindRouter = "router"
	TargetKindApp    = "app"
)

type TargetProfile struct {
	ID         string
	ProfileKey string
	// Title is what an operator calls this device. The catalog owns it, so a
	// screen never has to keep a list of names in step with the catalog, and a
	// target added by a plugin can name itself.
	Title                  string
	Kind                   string
	RendererID             string
	Constraints            TargetConstraints
	RendererOptions        []string
	ManualInstallationHint string
	// TitleEN and ManualInstallationHintEN are the same name and the same
	// instruction in English. English is the product's primary language while
	// the shipped catalog is authored in Russian, so the translation travels
	// with the target rather than in a table a screen keeps in step by hand.
	// Empty means this target has no translation: absent, not blank, and a
	// caller reports it as absent instead of rendering an empty name.
	TitleEN                  string
	ManualInstallationHintEN string
}

// RawJSONTargetProfile is the diagnostic profile: every rule shape is
// supported, so a plan rendered against it shows what the planner decided
// rather than what a device could carry.
func RawJSONTargetProfile() TargetProfile {
	return TargetProfile{
		ID: "raw-json", ProfileKey: "raw-v1", RendererID: "raw-json",
		Constraints: TargetConstraints{
			SupportsDomainExact:  true,
			SupportsDomainSuffix: true,
			SupportsIPv4:         true,
			SupportsIPv6:         true,
			SupportsPrefixes:     true,
			MaxArtifactSize:      4 << 20,
		},
	}
}
