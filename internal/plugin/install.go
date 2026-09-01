package plugin

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
	wire "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

// Installed is one plugin the operator installed: its directory, the manifest on
// disk, and the executable path verified when the installation was loaded. Start
// verifies the file again while making the private snapshot it actually runs.
type Installed struct {
	Directory  string
	Manifest   wire.Manifest
	Executable string
}

// Discover reads every installed plugin under one directory.
//
// A missing directory is not an error: a build with no plugins installed must
// work exactly as it did before plugins existed. A directory that exists but
// holds an unusable plugin is an error, because silently ignoring it would look
// like the plugin was working.
func Discover(root string) ([]Installed, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPluginRoot, err)
	}
	if len(entries) > MaxPluginDirectories {
		return nil, fmt.Errorf("%w: %d entries exceed the bound %d", ErrPluginRoot, len(entries), MaxPluginDirectories)
	}
	installed := make([]Installed, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		loaded, err := Load(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		installed = append(installed, loaded)
	}
	slices.SortFunc(installed, func(a, b Installed) int { return cmp.Compare(a.Manifest.Name, b.Manifest.Name) })
	for index := 1; index < len(installed); index++ {
		if installed[index-1].Manifest.Name == installed[index].Manifest.Name {
			return nil, fmt.Errorf("%w: duplicate plugin name %q", ErrManifestInvalid, installed[index].Manifest.Name)
		}
	}
	return installed, nil
}

// Load reads and verifies one plugin directory.
//
// The executable is hashed here for early diagnostics. Start repeats the check
// while making the private execution snapshot, so replacement between Load and
// Start is also a refusal.
func Load(directory string) (Installed, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Installed{}, fmt.Errorf("%w: %v", ErrPluginRoot, err)
	}
	defer func() { _ = root.Close() }()

	manifestFile, err := root.Open("manifest.json")
	if err != nil {
		return Installed{}, fmt.Errorf("%w: %s has no manifest.json", ErrManifestInvalid, directory)
	}
	defer func() { _ = manifestFile.Close() }()
	manifestPayload, err := io.ReadAll(io.LimitReader(manifestFile, MaxManifestBytes+1))
	if err != nil || len(manifestPayload) > MaxManifestBytes {
		return Installed{}, fmt.Errorf("%w: manifest is unreadable or too large", ErrManifestInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(manifestPayload))
	decoder.DisallowUnknownFields()
	var manifest wire.Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Installed{}, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Installed{}, err
	}

	executable, err := root.Open(manifest.Executable)
	if err != nil {
		return Installed{}, fmt.Errorf("%w: executable %q is missing", ErrManifestInvalid, manifest.Executable)
	}
	defer func() { _ = executable.Close() }()
	info, err := executable.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Installed{}, fmt.Errorf("%w: executable is not a regular file", ErrManifestInvalid)
	}
	if info.Size() <= 0 || info.Size() > MaxExecutableBytes {
		return Installed{}, fmt.Errorf("%w: executable size %d is outside the bound", ErrManifestInvalid, info.Size())
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(executable, MaxExecutableBytes)); err != nil {
		return Installed{}, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != manifest.SHA256 {
		return Installed{}, fmt.Errorf("%w: %s", ErrChecksumMismatch, manifest.Name)
	}
	absolute, err := filepath.Abs(filepath.Join(directory, manifest.Executable))
	if err != nil {
		return Installed{}, fmt.Errorf("%w: %v", ErrPluginRoot, err)
	}
	return Installed{Directory: directory, Manifest: manifest, Executable: absolute}, nil
}

// ValidateManifest refuses an installed manifest that is incoherent, claims an
// unknown kind, or asks for a permission its kind may not hold.
func ValidateManifest(manifest wire.Manifest) error {
	if manifest.Executable == "" || strings.ContainsAny(manifest.Executable, `/\`) || manifest.Executable == "." || manifest.Executable == ".." {
		// The executable is a plain name inside the plugin directory, so a
		// manifest cannot point the host at a file elsewhere on the machine.
		return fmt.Errorf("%w: executable %q must be a plain file name", ErrManifestInvalid, manifest.Executable)
	}
	if len(manifest.SHA256) != 64 {
		return fmt.Errorf("%w: checksum", ErrManifestInvalid)
	}
	if _, err := hex.DecodeString(manifest.SHA256); err != nil {
		return fmt.Errorf("%w: checksum", ErrManifestInvalid)
	}
	return validateReported(manifest)
}

// validateReported checks the parts a plugin knows about itself. The executable
// name and its checksum are install-time facts a plugin cannot know, so they are
// required of the installed manifest and never of the reported one.
func validateReported(manifest wire.Manifest) error {
	if manifest.Name == "" || len(manifest.Name) > 64 || !isPluginName(manifest.Name) {
		return fmt.Errorf("%w: name %q", ErrManifestInvalid, manifest.Name)
	}
	if manifest.Version == "" || len(manifest.Version) > 64 {
		return fmt.Errorf("%w: version", ErrManifestInvalid)
	}
	if manifest.ProtocolVersion <= 0 {
		return fmt.Errorf("%w: protocol version", ErrManifestInvalid)
	}
	allowed := wire.RequiredPermissions(manifest.Kind)
	if len(allowed) == 0 {
		return fmt.Errorf("%w: kind %q", ErrManifestInvalid, manifest.Kind)
	}
	allowedSet := map[wire.Permission]struct{}{}
	for _, permission := range allowed {
		allowedSet[permission] = struct{}{}
	}
	for _, permission := range manifest.Permissions {
		if !wire.KnownPermission(permission) {
			return fmt.Errorf("%w: unknown permission %q", ErrManifestInvalid, permission)
		}
		if _, ok := allowedSet[permission]; !ok {
			return fmt.Errorf("%w: %q may not hold %q", ErrPermissionDenied, manifest.Kind, permission)
		}
	}
	switch manifest.Kind {
	case wire.KindRenderer:
		if manifest.Renderer == nil || manifest.Source != nil {
			return fmt.Errorf("%w: a renderer manifest describes exactly one renderer", ErrManifestInvalid)
		}
		renderer := manifest.Renderer
		if renderer.ID == "" || renderer.FormatVersion == "" || renderer.ContentType == "" || renderer.FileExtension == "" || len(renderer.SupportedRuleKinds) == 0 {
			return fmt.Errorf("%w: renderer metadata is incomplete", ErrManifestInvalid)
		}
	case wire.KindSource:
		if manifest.Source == nil || manifest.Renderer != nil {
			return fmt.Errorf("%w: a source manifest describes exactly one source", ErrManifestInvalid)
		}
		if domain.ValidateSlug(manifest.Source.Type) != nil || domain.ValidateSlug(manifest.Source.Revision) != nil {
			return fmt.Errorf("%w: source metadata is incomplete", ErrManifestInvalid)
		}
	}
	return nil
}

func isPluginName(value string) bool {
	for index := 0; index < len(value); index++ {
		c := value[index]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return value[0] != '-' && value[len(value)-1] != '-'
}
