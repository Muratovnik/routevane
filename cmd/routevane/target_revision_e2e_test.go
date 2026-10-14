package main

import (
	"bytes"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestEditingAFormatsTextKeepsItsOutputAndLink is the end-to-end oracle of ADR
// 0054 through the running service: a release that rewords a format's name and
// installation leaves an existing output buildable and its subscription link
// answering after a restart, while a release that changes a limit the artifact
// is built against still refuses the output as a changed format.
func TestEditingAFormatsTextKeepsItsOutputAndLink(t *testing.T) {
	catalog := filepath.Join(t.TempDir(), "catalog")
	if err := os.CopyFS(catalog, os.DirFS(filepath.Join("..", "..", "testdata", "expiry", "catalog"))); err != nil {
		t.Fatal(err)
	}
	// An alias of the router format: built-in target ids are pinned to their
	// exact limits, so the limit edit below would drop a built-in target
	// rather than change it, and the refusal would come from a missing target.
	targetFile := filepath.Join(catalog, "targets", "keenetic-alias.yaml")
	writeTarget := func(title, hint string, maxRules int) {
		t.Helper()
		payload := "id: keenetic-alias\nformat_key: keenetic-bat-ipv4-v1\ntitle: " + title + "\nkind: router\nrenderer: keenetic-route-bat\n" +
			"constraints:\n  supports_ipv4: true\n  supports_prefixes: true\n  max_rules: " + strconv.Itoa(maxRules) + "\n  max_artifact_size: 131072\n" +
			"manual_installation_hint: " + hint + "\n"
		if err := os.WriteFile(targetFile, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeTarget("Keenetic copy", "Upload the BAT file in the routing settings.", 1024)
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	serve := func(run func(origin string)) {
		t.Helper()
		origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
		run(origin)
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}

	var outputID, token string
	var first []byte
	serve(func(origin string) {
		var subscriptionURL string
		_, outputID, subscriptionURL = createProfileOutput(t, origin, "Дача", "keenetic-alias", "youtube")
		token = strings.TrimPrefix(subscriptionURL, origin+"/v1/subscriptions/")
		served := httpGet(t, subscriptionURL, nil)
		if served.status != http.StatusOK || !strings.Contains(string(served.body), "192.0.2.10") {
			t.Fatalf("first publication=%d %s", served.status, served.body)
		}
		first = served.body
	})

	// The next release renames the format and rewords its installation only.
	writeTarget("Keenetic (copy)", "Open the routing settings and upload the BAT file.", 1024)
	serve(func(origin string) {
		rebuilt := postGuardedBody(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
		if rebuilt.status != http.StatusOK {
			t.Fatalf("rebuild after a text edit=%d %s", rebuilt.status, rebuilt.body)
		}
		served := httpGet(t, origin+"/v1/subscriptions/"+token, nil)
		if served.status != http.StatusOK || !bytes.Equal(served.body, first) {
			t.Fatalf("the link issued before the edit answered %d %s", served.status, served.body)
		}
	})

	// An install upgraded from schema 18 holds the output under the retired
	// catalog-wide revision and marked for the rebase. That state is planted
	// here through the same exception the migration opens, and the service
	// must move the output before its first build.
	plantLegacyTargetRevision(t, filepath.Join(data, "routevane.db"), outputID)
	serve(func(origin string) {
		rebuilt := postGuardedBody(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
		if rebuilt.status != http.StatusOK {
			t.Fatalf("rebuild of an upgraded output=%d %s", rebuilt.status, rebuilt.body)
		}
		if served := httpGet(t, origin+"/v1/subscriptions/"+token, nil); served.status != http.StatusOK || !bytes.Equal(served.body, first) {
			t.Fatalf("the link of an upgraded output answered %d %s", served.status, served.body)
		}
	})

	// A release that lowers the rule limit changes what the file is built
	// against: the output is refused as before, and the link keeps serving the
	// last valid file.
	writeTarget("Keenetic (copy)", "Open the routing settings and upload the BAT file.", 1000)
	serve(func(origin string) {
		refused := postGuardedBody(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
		if refused.status != http.StatusConflict || !strings.Contains(string(refused.body), "output target changed") {
			t.Fatalf("rebuild after a limit edit=%d %s", refused.status, refused.body)
		}
		served := httpGet(t, origin+"/v1/subscriptions/"+token, nil)
		if served.status != http.StatusOK || !bytes.Equal(served.body, first) {
			t.Fatalf("the link after a refused build answered %d %s", served.status, served.body)
		}
	})
}

// plantLegacyTargetRevision gives one output the state schema 19 leaves on an
// upgraded install: the retired catalog-wide revision and a rebase marker. The
// closed marker set is reopened for the insert only and closed again with the
// migration's own trigger, so the service opens a schema it accepts.
func plantLegacyTargetRevision(t *testing.T, path, outputID string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var closed string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='legacy_target_revisions_closed'`).Scan(&closed); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP TRIGGER legacy_target_revisions_closed`,
		`INSERT INTO legacy_target_revisions(output_id) VALUES('` + outputID + `')`,
		`UPDATE outputs SET target_revision='` + strings.Repeat("e", 64) + `' WHERE id='` + outputID + `'`,
		closed,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}
