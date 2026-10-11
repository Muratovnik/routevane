package filesystem

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// MaxSessionEvidenceBytes bounds one stored evidence document.
const MaxSessionEvidenceBytes = 8 << 20

// WriteSessionEvidence stores one learning session's evidence under the data
// root and returns its path.
//
// Evidence is runtime state, not catalog content: it lives beside the database
// rather than in the catalog so a replayed scenario can be compared with an
// earlier run without changing any reviewed definition.
func WriteSessionEvidence(ctx context.Context, dataRoot, listID string, observedAt time.Time, evidence any) (string, error) {
	if ctx == nil || ctx.Err() != nil {
		return "", fmt.Errorf("invalid session evidence context")
	}
	if err := validateSegment(listID); err != nil {
		return "", err
	}
	rootPath, err := ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return "", err
	}
	payload, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode session evidence: %w", err)
	}
	payload = append(payload, '\n')
	if len(payload) > MaxSessionEvidenceBytes {
		return "", fmt.Errorf("session evidence exceeds %d bytes", MaxSessionEvidenceBytes)
	}
	directory := filepath.Join(rootPath, "discovery", listID)
	dataRootHandle, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", fmt.Errorf("open session evidence root: %w", err)
	}
	defer dataRootHandle.Close()
	_, relativeDirectory, outputRoot, err := prepareOutputDirectory(dataRootHandle, rootPath, directory)
	if err != nil {
		return "", err
	}
	defer outputRoot.Close()
	name := observedAt.UTC().Format("20060102T150405.000000000Z") + ".json"
	target := filepath.Join(directory, name)
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("create session evidence temporary identity: %w", err)
	}
	temporaryName := ".session." + hex.EncodeToString(suffix) + ".tmp"
	temporary, err := outputRoot.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create session evidence temporary file: %w", err)
	}
	defer func() {
		_ = temporary.Close()
		_ = outputRoot.Remove(temporaryName)
	}()
	if _, err := temporary.Write(payload); err != nil {
		return "", fmt.Errorf("write session evidence: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync session evidence: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close session evidence: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot); err != nil {
		return "", err
	}
	// Link rather than rename: an existing document for the same instant is a
	// collision to report, not something to overwrite.
	if err := outputRoot.Link(temporaryName, name); err != nil {
		return "", fmt.Errorf("commit session evidence: %w", err)
	}
	if err := outputRoot.Remove(temporaryName); err != nil {
		cleanupOutputCandidate(outputRoot, name)
		_ = syncOutputRoot(outputRoot)
		return "", fmt.Errorf("remove session evidence temporary file: %w", err)
	}
	if err := syncOutputRoot(outputRoot); err != nil {
		cleanupOutputCandidate(outputRoot, name)
		_ = syncOutputRoot(outputRoot)
		return "", fmt.Errorf("sync session evidence directory: %w", err)
	}
	if err := verifyOutputRootIdentity(dataRootHandle, relativeDirectory, outputRoot); err != nil {
		cleanupOutputCandidate(outputRoot, name)
		_ = syncOutputRoot(outputRoot)
		return "", err
	}
	return target, nil
}

// validateSegment refuses any identity that would become a path.
func validateSegment(value string) error {
	if value == "" || len(value) > 63 {
		return ErrUnsafePath
	}
	for index := 0; index < len(value); index++ {
		c := value[index]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return ErrUnsafePath
	}
	if value[0] == '-' || value[len(value)-1] == '-' {
		return ErrUnsafePath
	}
	return nil
}

// ReadBoundedFile reads one operator-supplied input under an explicit byte
// bound, with file access scoped to the file's own directory.
//
// Scoping through os.Root is what makes the read traversal-safe: a symlink
// inside that directory cannot reach outside it. The opened handle must be the
// regular file inspected before opening; a swapped-in FIFO cannot block open.
func ReadBoundedFile(path string, maxBytes int64) ([]byte, error) {
	if path == "" || maxBytes <= 0 {
		return nil, ErrUnsafePath
	}
	directory, name := filepath.Split(filepath.Clean(path))
	if directory == "" {
		directory = "."
	}
	if name == "" || name == "." || name == ".." {
		return nil, ErrUnsafePath
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open input directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	listed, err := root.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect input file: %w", err)
	}
	if !listed.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrUnsafePath)
	}
	// A FIFO must not block opening before the handle can be inspected. The
	// platform helper also closes the swap between this check and the open;
	// the actual handle, rather than the earlier path, decides what is read.
	file, err := openInputFile(root, name)
	if err != nil {
		return nil, fmt.Errorf("open input file: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(listed, info) {
		return nil, fmt.Errorf("%w: not a regular file", ErrUnsafePath)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", maxBytes)
	}
	// One extra byte distinguishes "exactly at the limit" from "truncated".
	payload, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read input file: %w", err)
	}
	if int64(len(payload)) > maxBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", maxBytes)
	}
	return payload, nil
}
