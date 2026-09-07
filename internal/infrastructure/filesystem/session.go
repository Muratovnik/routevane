package filesystem

import (
	"context"
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
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create session evidence directory: %w", err)
	}
	name := observedAt.UTC().Format("20060102T150405.000000000Z") + ".json"
	target := filepath.Join(directory, name)
	temporary, err := os.CreateTemp(directory, ".session.*.tmp")
	if err != nil {
		return "", fmt.Errorf("create session evidence temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
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
	// Link rather than rename: an existing document for the same instant is a
	// collision to report, not something to overwrite.
	if err := os.Link(temporaryName, target); err != nil {
		return "", fmt.Errorf("commit session evidence: %w", err)
	}
	committed = true
	if err := os.Remove(temporaryName); err != nil {
		return "", fmt.Errorf("remove session evidence temporary file: %w", err)
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
// inside that directory cannot reach outside it, and the handle is inspected
// once rather than the path being resolved twice.
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
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open input file: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
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
