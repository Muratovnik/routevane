package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

func TestPublishedWriterRejectsSwappedRootOnNoReplaceReuseConflict(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n")
	hash := sha256.Sum256(payload)
	name := hex.EncodeToString(hash[:]) + ".bat"
	output := filepath.Join(root, "artifacts", "published", "keenetic-route-bat")
	moved := filepath.Join(root, "artifacts", "published", "keenetic-route-bat-moved")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	errSwapUnavailable := errors.New("publication directory swap unavailable")
	writer := PublishedWriter{BeforeCommit: func(string) error {
		if err := os.Rename(output, moved); err != nil {
			return fmt.Errorf("%w: rename: %v", errSwapUnavailable, err)
		}
		if err := os.WriteFile(filepath.Join(moved, name), payload, 0o600); err != nil {
			return err
		}
		if err := createDirectoryRedirect(outside, output); err != nil {
			_ = os.Remove(filepath.Join(moved, name))
			_ = os.Rename(moved, output)
			return fmt.Errorf("%w: redirect: %v", errSwapUnavailable, err)
		}
		return nil
	}}
	_, err = writer.PutPublished(context.Background(), root, keeneticDescriptor(), payload)
	if errors.Is(err, errSwapUnavailable) {
		t.Skipf("platform cannot perform directory swap: %v", err)
	}
	if err == nil {
		t.Fatal("swapped current publication path was accepted as reuse")
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("current redirected path touched: entries=%v err=%v", entries, readErr)
	}
}

func TestPublishedStoreIsContentAddressedAndVerifiesReads(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store := PublishedStore{DataRoot: root}
	payload := []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n")
	file, err := store.PutPublished(context.Background(), keeneticDescriptor(), payload)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	wantHash := hex.EncodeToString(hash[:])
	wantPath := filepath.ToSlash(filepath.Join("artifacts", "published", "keenetic-route-bat", wantHash+".bat"))
	if file.Hash != wantHash || file.RelativePath != wantPath || file.Reused {
		t.Fatalf("file=%#v", file)
	}
	again, err := store.PutPublished(context.Background(), keeneticDescriptor(), payload)
	if err != nil || !again.Reused {
		t.Fatalf("reused=%#v err=%v", again, err)
	}
	record := application.ArtifactBuildRecord{RendererID: "keenetic-route-bat", ArtifactHash: file.Hash, ArtifactPath: file.RelativePath, SizeBytes: file.Size, ContentCreatedAt: time.Now()}
	read, err := store.ReadPublished(context.Background(), record, keeneticDescriptor())
	if err != nil || string(read) != string(payload) {
		t.Fatalf("read=%q err=%v", read, err)
	}
	abs := filepath.Join(root, filepath.FromSlash(file.RelativePath))
	if err := os.WriteFile(abs, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadPublished(context.Background(), record, keeneticDescriptor()); err == nil {
		t.Fatal("corrupt artifact verified")
	}
}

func TestPublishedStoreRejectsPathIdentityFromDatabase(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	record := application.ArtifactBuildRecord{RendererID: "keenetic-route-bat", ArtifactHash: string(make([]byte, 64)), ArtifactPath: "../escape.bat", SizeBytes: 1}
	if _, err := ReadPublished(context.Background(), root, record, keeneticDescriptor()); err == nil {
		t.Fatal("unsafe relative path accepted")
	}
}

// keeneticDescriptor is the format identity these storage tests publish under.
func keeneticDescriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: "keenetic-route-bat", Version: "keenetic-bat-ipv4-v1", ContentType: "application/x-bat", FileExtension: "bat"}
}
