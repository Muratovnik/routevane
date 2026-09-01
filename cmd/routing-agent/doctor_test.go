package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorIsUnhealthyWithoutCreatingMissingDataRoot(t *testing.T) {
	catalogRoot := writeExampleCatalog(t, exampleServiceYAML)
	missing := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer
	code := runWithDeps(&stdout, &stderr, []string{"doctor", "--catalog-dir", catalogRoot, "--data-dir", missing}, runtimeDeps{})
	if code != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("doctor created or changed missing data root: %v", err)
	}
	var report doctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.Healthy {
		t.Fatalf("report=%q err=%v", stdout.String(), err)
	}
}
