package httpapi

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

// ui contains the generated Nuxt public output. routevane-ui.marker is
// tracked so a clean Go checkout can compile before the canonical tool command
// replaces the ignored generated files.
//
//go:embed all:ui
var embeddedUI embed.FS

const uiContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'"

type staticAssets struct {
	fs        fs.FS
	index     []byte
	digest    string
	available bool
}

func embeddedStaticAssets() staticAssets {
	root, err := fs.Sub(embeddedUI, "ui")
	if err != nil {
		return staticAssets{}
	}
	return loadStaticAssets(root)
}

func loadStaticAssets(root fs.FS) staticAssets {
	index, err := fs.ReadFile(root, "index.html")
	if err != nil || len(index) == 0 {
		return staticAssets{}
	}
	digest, err := staticDigest(root)
	if err != nil {
		return staticAssets{}
	}
	return staticAssets{fs: root, index: index, digest: digest, available: true}
}

func staticDigest(root fs.FS) (string, error) {
	var paths []string
	if err := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("generated UI has non-regular entry %q", name)
		}
		if name != "routevane-ui.marker" {
			paths = append(paths, name)
		}
		return nil
	}); err != nil {
		return "", err
	}
	slices.Sort(paths)
	hash := sha256.New()
	for _, name := range paths {
		payload, err := fs.ReadFile(root, name)
		if err != nil {
			return "", err
		}
		fileHash := sha256.Sum256(payload)
		if _, err := fmt.Fprintf(hash, "%x  %s\n", fileHash, name); err != nil {
			return "", err
		}
	}
	return "sha256-" + hex.EncodeToString(hash.Sum(nil)), nil
}

func (h *handler) serveUI(w http.ResponseWriter, r *http.Request) {
	if !h.assets.available {
		if r.URL.Path == "/" {
			h.status(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	requestPath := strings.TrimPrefix(r.URL.Path, "/")
	if requestPath == "" || !isStaticAssetPath(requestPath) {
		h.writeStatic(w, r, "index.html", true)
		return
	}
	h.writeStatic(w, r, requestPath, false)
}

func isStaticAssetPath(requestPath string) bool {
	if strings.HasPrefix(requestPath, "_nuxt/") || strings.HasPrefix(requestPath, "_routevane/") {
		return true
	}
	return strings.Contains(path.Base(requestPath), ".")
}

func (h *handler) writeStatic(w http.ResponseWriter, r *http.Request, name string, document bool) {
	if !fs.ValidPath(name) || name == "routevane-ui.marker" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	payload := h.assets.index
	if name != "index.html" {
		var err error
		payload, err = fs.ReadFile(h.assets.fs, name)
		if err != nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
	}

	fileHash := sha256.Sum256(payload)
	w.Header().Set("Content-Security-Policy", uiContentSecurityPolicy)
	w.Header().Set("ETag", `"`+hex.EncodeToString(fileHash[:])+`"`)
	w.Header().Set("Content-Type", staticContentType(name))
	if document {
		w.Header().Set("Cache-Control", "no-store")
	} else if strings.HasPrefix(name, "_nuxt/") {
		// A generated chunk carries its content hash in its own name, so a
		// cached copy can never be stale and never needs revalidating.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	// The embedded tree has no modification time. The zero time is what leaves
	// Last-Modified out of the answer, so a conditional request is decided by
	// the ETag above and by nothing else.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(payload))
}

func staticContentType(name string) string {
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}
