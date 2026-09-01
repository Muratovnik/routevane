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
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

var ErrArtifactCollision = errors.New("artifact identity collision")

// ArtifactWriter exposes one failure hook solely for atomicity tests. Product
// composition uses the zero value.
type ArtifactWriter struct {
	BeforeRename func(tempPath string) error
}

type ArtifactStore struct {
	DataRoot string
	Writer   ArtifactWriter
}

func (s ArtifactStore) Put(ctx context.Context, rendererID, serviceID string, cutoff time.Time, semanticHash string, payload []byte) (string, bool, error) {
	return s.Writer.Put(ctx, s.DataRoot, rendererID, serviceID, cutoff, semanticHash, payload)
}

func (w ArtifactWriter) Put(ctx context.Context, dataRoot, rendererID, serviceID string, cutoff time.Time, semanticHash string, payload []byte) (string, bool, error) {
	if ctx == nil {
		return "", false, fmt.Errorf("artifact context is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if rendererID != "raw-json" || domain.ValidateSlug(serviceID) != nil || cutoff.IsZero() || !isSHA256(semanticHash) || len(payload) == 0 {
		return "", false, fmt.Errorf("invalid artifact identity")
	}
	root, err := ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return "", false, err
	}
	artifacts := filepath.Join(root, "artifacts")
	rendererDir := filepath.Join(artifacts, rendererID)
	serviceDir := filepath.Join(rendererDir, serviceID)
	for _, dir := range []string{artifacts, rendererDir, serviceDir} {
		if err := EnsurePrivateDir(dir); err != nil {
			return "", false, fmt.Errorf("prepare artifact directory: %w", err)
		}
	}
	stamp := cutoff.UTC().Format("20060102T150405.000000000Z")
	payloadHash := sha256.Sum256(payload)
	finalPath := filepath.Join(serviceDir, stamp+"_"+semanticHash+"_"+hex.EncodeToString(payloadHash[:])+".json")
	if reused, err := verifyExisting(finalPath, payload); err != nil || reused {
		return finalPath, reused, err
	}

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", false, fmt.Errorf("create artifact temporary identity: %w", err)
	}
	serviceRoot, err := os.OpenRoot(serviceDir)
	if err != nil {
		return "", false, fmt.Errorf("open artifact root: %w", err)
	}
	defer func() { _ = serviceRoot.Close() }()
	tempName := "." + filepath.Base(finalPath) + "." + hex.EncodeToString(suffix) + ".tmp"
	tempPath := filepath.Join(serviceDir, tempName)
	file, err := serviceRoot.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", false, fmt.Errorf("create artifact temporary file: %w", err)
	}
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return "", false, fmt.Errorf("write artifact temporary file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", false, fmt.Errorf("sync artifact temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", false, fmt.Errorf("close artifact temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if w.BeforeRename != nil {
		if err := w.BeforeRename(tempPath); err != nil {
			return "", false, fmt.Errorf("artifact pre-commit check: %w", err)
		}
	}
	if err := renameNoReplace(tempPath, finalPath); err != nil {
		if reused, verifyErr := verifyExisting(finalPath, payload); verifyErr != nil || reused {
			return finalPath, reused, verifyErr
		}
		return "", false, fmt.Errorf("commit artifact: %w", err)
	}
	removeTemp = false
	if err := syncDirectory(serviceDir); err != nil {
		_ = os.Remove(finalPath)
		_ = syncDirectory(serviceDir)
		return "", false, fmt.Errorf("sync artifact directory: %w", err)
	}
	abs, err := filepath.Abs(finalPath)
	if err != nil {
		return "", false, fmt.Errorf("resolve artifact path: %w", err)
	}
	return abs, false, nil
}

func verifyExisting(path string, payload []byte) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || IsLinkOrReparse(info) || info.Size() != int64(len(payload)) {
		return false, ErrArtifactCollision
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return false, err
	}
	defer file.Close()
	existing, err := io.ReadAll(io.LimitReader(file, int64(len(payload))+1))
	if err != nil {
		return false, err
	}
	if !bytes.Equal(existing, payload) {
		return false, ErrArtifactCollision
	}
	abs, err := filepath.Abs(path)
	if err != nil || strings.TrimSpace(abs) == "" {
		return false, fmt.Errorf("resolve existing artifact path")
	}
	return true, nil
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
