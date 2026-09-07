package filesystem

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// maxPublishedBytes is a defence-in-depth ceiling for any format, not the
// policy bound. The per-format limit is the target definition's MaxArtifactSize,
// which the application enforces before an artifact reaches this layer.
const maxPublishedBytes = 16 << 20

type PublishedWriter struct {
	BeforeCommit func(string) error
}
type PublishedStore struct {
	DataRoot string
	Writer   PublishedWriter
}

func (s PublishedStore) PutPublished(ctx context.Context, descriptor domain.RendererDescriptor, payload []byte) (application.PublishedFile, error) {
	return s.Writer.PutPublished(ctx, s.DataRoot, descriptor, payload)
}
func (s PublishedStore) ReadPublished(ctx context.Context, artifact application.ArtifactBuildRecord, descriptor domain.RendererDescriptor) ([]byte, error) {
	return ReadPublished(ctx, s.DataRoot, artifact, descriptor)
}

func (w PublishedWriter) PutPublished(ctx context.Context, dataRoot string, descriptor domain.RendererDescriptor, payload []byte) (application.PublishedFile, error) {
	if ctx == nil || ctx.Err() != nil || !descriptor.IsValid() || len(payload) == 0 || len(payload) > maxPublishedBytes {
		return application.PublishedFile{}, fmt.Errorf("invalid published artifact")
	}
	rendererID := descriptor.ID
	rootPath, err := ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return application.PublishedFile{}, err
	}
	dataRootHandle, err := os.OpenRoot(rootPath)
	if err != nil {
		return application.PublishedFile{}, fmt.Errorf("open publication root: %w", err)
	}
	defer dataRootHandle.Close()
	directory := filepath.Join(rootPath, "artifacts", "published", rendererID)
	_, relativeDirectory, outputRoot, err := prepareOutputDirectory(dataRootHandle, rootPath, directory)
	if err != nil {
		return application.PublishedFile{}, err
	}
	defer outputRoot.Close()
	hash := sha256.Sum256(payload)
	hashText := hex.EncodeToString(hash[:])
	name := hashText + "." + descriptor.FileExtension
	relativePath := filepath.ToSlash(filepath.Join(relativeDirectory, name))
	if reused, verifyErr := verifyExistingInRoot(outputRoot, name, payload); verifyErr != nil || reused {
		if verifyErr == nil {
			verifyErr = verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot)
		}
		return application.PublishedFile{RelativePath: relativePath, Hash: hashText, Size: int64(len(payload)), Reused: reused}, verifyErr
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return application.PublishedFile{}, fmt.Errorf("create publication temporary identity: %w", err)
	}
	tempName := "." + name + "." + hex.EncodeToString(suffix) + ".tmp"
	file, err := outputRoot.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return application.PublishedFile{}, fmt.Errorf("create publication temporary file: %w", err)
	}
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = outputRoot.Remove(tempName)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return application.PublishedFile{}, fmt.Errorf("write publication temporary file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return application.PublishedFile{}, fmt.Errorf("sync publication temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return application.PublishedFile{}, fmt.Errorf("close publication temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return application.PublishedFile{}, err
	}
	if err := verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot); err != nil {
		return application.PublishedFile{}, err
	}
	if w.BeforeCommit != nil {
		if err := w.BeforeCommit(filepath.Join(directory, tempName)); err != nil {
			return application.PublishedFile{}, fmt.Errorf("publication pre-commit check: %w", err)
		}
	}
	if err := outputRoot.Link(tempName, name); err != nil {
		if reused, verifyErr := verifyExistingInRoot(outputRoot, name, payload); verifyErr == nil && reused {
			if verifyErr = verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot); verifyErr == nil {
				return application.PublishedFile{RelativePath: relativePath, Hash: hashText, Size: int64(len(payload)), Reused: true}, nil
			}
		}
		return application.PublishedFile{}, fmt.Errorf("commit published artifact: %w", err)
	}
	if err := outputRoot.Remove(tempName); err != nil {
		cleanupOutputCandidate(outputRoot, name)
		return application.PublishedFile{}, fmt.Errorf("remove publication temporary file: %w", err)
	}
	removeTemp = false
	if err := syncOutputRoot(outputRoot); err != nil {
		cleanupOutputCandidate(outputRoot, name)
		return application.PublishedFile{}, fmt.Errorf("sync publication directory: %w", err)
	}
	if err := verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot); err != nil {
		cleanupOutputCandidate(outputRoot, name)
		return application.PublishedFile{}, err
	}
	if reused, err := verifyExistingInRoot(outputRoot, name, payload); err != nil || !reused {
		cleanupOutputCandidate(outputRoot, name)
		if err != nil {
			return application.PublishedFile{}, err
		}
		return application.PublishedFile{}, fmt.Errorf("verify published artifact")
	}
	return application.PublishedFile{RelativePath: relativePath, Hash: hashText, Size: int64(len(payload))}, nil
}

func ReadPublished(ctx context.Context, dataRoot string, artifact application.ArtifactBuildRecord, descriptor domain.RendererDescriptor) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || !descriptor.IsValid() || artifact.RendererID != descriptor.ID || !isSHA256(artifact.ArtifactHash) || artifact.SizeBytes <= 0 || artifact.SizeBytes > maxPublishedBytes {
		return nil, fmt.Errorf("invalid published artifact identity")
	}
	expected := filepath.ToSlash(filepath.Join("artifacts", "published", artifact.RendererID, artifact.ArtifactHash+"."+descriptor.FileExtension))
	if artifact.ArtifactPath != expected {
		return nil, ErrUnsafePath
	}
	rootPath, err := ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	directory := filepath.FromSlash(filepath.ToSlash(filepath.Dir(expected)))
	info, err := inspectPlainRootDirectory(root, directory)
	if err != nil {
		return nil, err
	}
	openedRoot, err := root.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer openedRoot.Close()
	openedInfo, err := openedRoot.Stat(".")
	if err != nil || !os.SameFile(info, openedInfo) {
		return nil, ErrUnsafePath
	}
	name := filepath.Base(expected)
	listed, err := openedRoot.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !listed.Mode().IsRegular() || IsLinkOrReparse(listed) || listed.Size() != artifact.SizeBytes {
		return nil, ErrArtifactCollision
	}
	file, err := openedRoot.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(listed, actual) {
		return nil, ErrArtifactCollision
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxPublishedBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) != artifact.SizeBytes {
		return nil, ErrArtifactCollision
	}
	current, err := openedRoot.Lstat(name)
	if err != nil || !current.Mode().IsRegular() || IsLinkOrReparse(current) || !os.SameFile(actual, current) {
		return nil, ErrArtifactCollision
	}
	hash := sha256.Sum256(payload)
	if !strings.EqualFold(hex.EncodeToString(hash[:]), artifact.ArtifactHash) {
		return nil, ErrArtifactCollision
	}
	return payload, nil
}
