package filesystem

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

const maxM2OutputBytes = 128 << 10

// BuildOutputWriter owns the non-publication file commit. The hook exists
// only for failure/cancellation tests; production composition uses zero value.
type BuildOutputWriter struct {
	BeforeRename func(tempPath string) error
}

type BuildOutputStore struct {
	DataRoot  string
	OutputDir string
	Writer    BuildOutputWriter
}

func (s BuildOutputStore) Put(ctx context.Context, descriptor domain.RendererDescriptor, semanticHash string, payload []byte) (string, bool, error) {
	return s.Writer.Put(ctx, s.DataRoot, s.OutputDir, descriptor, semanticHash, payload)
}

func (w BuildOutputWriter) Put(ctx context.Context, dataRoot, outputDir string, descriptor domain.RendererDescriptor, semanticHash string, payload []byte) (string, bool, error) {
	if ctx == nil {
		return "", false, fmt.Errorf("build output context is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	// The format identity comes from the renderer descriptor, so an installed
	// plugin renderer writes through exactly this path rather than needing one of
	// its own. Its validation is what makes the directory segment and the file
	// suffix safe.
	if !descriptor.IsValid() || !isSHA256(semanticHash) || len(payload) == 0 || len(payload) > maxM2OutputBytes {
		return "", false, fmt.Errorf("invalid build output identity")
	}
	rootPath, err := ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return "", false, err
	}
	dataRootHandle, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", false, fmt.Errorf("open data root for build output: %w", err)
	}
	defer func() { _ = dataRootHandle.Close() }()
	directory, relativeDirectory, outputRoot, err := prepareOutputDirectory(dataRootHandle, rootPath, outputDir)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = outputRoot.Close() }()
	payloadHash := sha256.Sum256(payload)
	finalName := semanticHash + "_" + hex.EncodeToString(payloadHash[:]) + "." + descriptor.FileExtension
	finalPath := filepath.Join(directory, finalName)
	if reused, err := verifyExistingInRoot(outputRoot, finalName, payload); err != nil || reused {
		if err == nil {
			err = verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot)
		}
		return finalPath, reused, err
	}

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", false, fmt.Errorf("create build output temporary identity: %w", err)
	}
	tempName := "." + finalName + "." + hex.EncodeToString(suffix) + ".tmp"
	tempPath := filepath.Join(directory, tempName)
	file, err := outputRoot.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", false, fmt.Errorf("create build output temporary file: %w", err)
	}
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = outputRoot.Remove(tempName)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return "", false, fmt.Errorf("write build output temporary file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", false, fmt.Errorf("sync build output temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", false, fmt.Errorf("close build output temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if w.BeforeRename != nil {
		if err := w.BeforeRename(tempPath); err != nil {
			return "", false, fmt.Errorf("build output pre-commit check: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if err := verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot); err != nil {
		return "", false, err
	}
	if err := outputRoot.Link(tempName, finalName); err != nil {
		if reused, verifyErr := verifyExistingInRoot(outputRoot, finalName, payload); verifyErr != nil || reused {
			if verifyErr == nil {
				verifyErr = verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot)
			}
			return finalPath, reused, verifyErr
		}
		return "", false, fmt.Errorf("commit build output: %w", err)
	}
	if err := verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot); err != nil {
		cleanupOutputCandidate(outputRoot, finalName)
		return "", false, err
	}
	if err := outputRoot.Remove(tempName); err != nil {
		cleanupOutputCandidate(outputRoot, finalName)
		_ = syncOutputRoot(outputRoot)
		return "", false, fmt.Errorf("remove committed build output temporary file: %w", err)
	}
	removeTemp = false
	if err := syncOutputRoot(outputRoot); err != nil {
		cleanupOutputCandidate(outputRoot, finalName)
		_ = syncOutputRoot(outputRoot)
		return "", false, fmt.Errorf("sync build output directory: %w", err)
	}
	if err := verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot); err != nil {
		cleanupOutputCandidate(outputRoot, finalName)
		_ = syncOutputRoot(outputRoot)
		return "", false, err
	}
	if reused, err := verifyExistingInRoot(outputRoot, finalName, payload); err != nil || !reused {
		cleanupOutputCandidate(outputRoot, finalName)
		_ = syncOutputRoot(outputRoot)
		if err != nil {
			return "", false, fmt.Errorf("verify committed build output: %w", err)
		}
		return "", false, fmt.Errorf("verify committed build output identity")
	}
	abs, err := filepath.Abs(finalPath)
	if err != nil || strings.TrimSpace(abs) == "" {
		return "", false, fmt.Errorf("resolve build output path")
	}
	return abs, false, nil
}

func prepareOutputDirectory(dataRoot *os.Root, dataRootPath, outputDir string) (string, string, *os.Root, error) {
	if strings.TrimSpace(outputDir) == "" {
		outputDir = filepath.Join(dataRootPath, "artifacts")
	}
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: resolve output directory", ErrUnsafePath)
	}
	abs = filepath.Clean(abs)
	relative, err := filepath.Rel(dataRootPath, abs)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", nil, fmt.Errorf("%w: output directory must be inside the data root", ErrUnsafePath)
	}
	if err := dataRoot.MkdirAll(relative, 0o700); err != nil {
		return "", "", nil, fmt.Errorf("%w: create output directory", ErrUnsafePath)
	}
	current := ""
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			return "", "", nil, fmt.Errorf("%w: invalid output directory component", ErrUnsafePath)
		}
		current = filepath.Join(current, component)
		info, err := dataRoot.Lstat(current)
		if err != nil || !info.IsDir() || IsLinkOrReparse(info) {
			return "", "", nil, fmt.Errorf("%w: output directory is unsafe", ErrUnsafePath)
		}
		if runtime.GOOS != "windows" {
			if err := protectRootDirectory(dataRoot, current); err != nil {
				return "", "", nil, fmt.Errorf("protect output directory: %w", err)
			}
		}
	}
	outputRoot, err := dataRoot.OpenRoot(relative)
	if err != nil {
		return "", "", nil, fmt.Errorf("open validated build output directory: %w", err)
	}
	if err := verifyOutputRootIdentity(dataRoot, relative, outputRoot); err != nil {
		_ = outputRoot.Close()
		return "", "", nil, err
	}
	return abs, relative, outputRoot, nil
}

func protectRootDirectory(root *os.Root, name string) error {
	directory, err := root.Open(name)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Chmod(0o700)
}

func inspectPlainRootDirectory(root *os.Root, relative string) (os.FileInfo, error) {
	current := ""
	var terminal os.FileInfo
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			return nil, fmt.Errorf("%w: invalid output directory component", ErrUnsafePath)
		}
		current = filepath.Join(current, component)
		info, err := root.Lstat(current)
		if err != nil || !info.IsDir() || IsLinkOrReparse(info) {
			return nil, fmt.Errorf("%w: output directory identity changed", ErrUnsafePath)
		}
		terminal = info
	}
	return terminal, nil
}

func verifyOutputRootIdentity(dataRoot *os.Root, relative string, outputRoot *os.Root) error {
	current, err := inspectPlainRootDirectory(dataRoot, relative)
	if err != nil {
		return err
	}
	opened, err := outputRoot.Stat(".")
	if err != nil || !opened.IsDir() || !os.SameFile(current, opened) {
		return fmt.Errorf("%w: output directory identity changed", ErrUnsafePath)
	}
	return nil
}

func verifyExistingInRoot(root *os.Root, name string, payload []byte) (bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || IsLinkOrReparse(info) || info.Size() != int64(len(payload)) {
		return false, ErrArtifactCollision
	}
	file, err := root.Open(name)
	if err != nil {
		return false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() != int64(len(payload)) || !os.SameFile(info, opened) {
		return false, ErrArtifactCollision
	}
	existing, err := io.ReadAll(io.LimitReader(file, int64(len(payload))+1))
	if err != nil {
		return false, err
	}
	current, err := root.Lstat(name)
	if err != nil || !current.Mode().IsRegular() || IsLinkOrReparse(current) || !os.SameFile(opened, current) {
		return false, ErrArtifactCollision
	}
	if !bytes.Equal(existing, payload) {
		return false, ErrArtifactCollision
	}
	return true, nil
}

func cleanupOutputCandidate(outputRoot *os.Root, finalName string) {
	_ = outputRoot.Remove(finalName)
}

func syncOutputRoot(root *os.Root) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
