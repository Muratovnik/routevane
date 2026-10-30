package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type switchContentsRow struct {
	Value      string `json:"value"`
	Origin     string `json:"origin"`
	Enabled    bool   `json:"enabled"`
	Definition bool   `json:"definition"`
}

// TestServeOperatorListSwitchesItsOwnDomainsEndToEnd is the list card's
// switch on a list the operator created: the domain they wrote is switched
// off and back on, the contents table names it as part of the definition so
// a client knows which verdict does that, and the published file follows.
func TestServeOperatorListSwitchesItsOwnDomainsEndToEnd(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	dnsTarget, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", "keenetic-dns.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalog, "targets", "keenetic-dns.yaml"), dnsTarget, 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(t.TempDir(), "data")
	origin, cancel, done, stderr := startAlphaBetaServeServer(t, catalog, data, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	var created struct {
		List struct {
			ID string `json:"id"`
		} `json:"list"`
	}
	if err := json.Unmarshal(postJSON(t, origin+"/v1/lists", `{"title":"Свои","domains":["myip.wtf","2ip.io"]}`), &created); err != nil {
		t.Fatal(err)
	}
	listID := created.List.ID
	// One value added by verdict, to show the row the client withdraws with
	// auto beside the rows it must switch off with exclude.
	postJSON(t, origin+"/v1/lists/"+listID+"/domains", `{"values":["added.example"],"verdict":"include"}`)

	rows := func(body []byte) map[string]switchContentsRow {
		t.Helper()
		var contents struct {
			Rows []switchContentsRow `json:"rows"`
		}
		if err := json.Unmarshal(body, &contents); err != nil {
			t.Fatalf("contents %s: %v", body, err)
		}
		byValue := make(map[string]switchContentsRow, len(contents.Rows))
		for _, row := range contents.Rows {
			byValue[row.Value] = row
		}
		return byValue
	}
	contents := httpGet(t, origin+"/v1/lists/"+listID+"/contents", nil)
	initial := rows(contents.body)
	for _, value := range []string{"myip.wtf", "2ip.io"} {
		if row := initial[value]; row.Origin != "manual" || !row.Enabled || !row.Definition {
			t.Fatalf("definition row %q = %+v in %s", value, row, contents.body)
		}
	}
	if row := initial["added.example"]; row.Origin != "manual" || !row.Enabled || row.Definition {
		t.Fatalf("included row = %+v in %s", row, contents.body)
	}

	profileID, outputID, subscriptionURL := createProfileOutput(t, origin, "Свои сайты", "keenetic-dns", listID)
	rebuild := func() string {
		t.Helper()
		postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
		postJSON(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
		file := httpGet(t, subscriptionURL, nil)
		if file.status != 200 {
			t.Fatalf("published file=%d %q", file.status, file.body)
		}
		return string(file.body)
	}
	if first := rebuild(); !strings.Contains(first, "2ip.io") || !strings.Contains(first, "myip.wtf") {
		t.Fatalf("first file = %q", first)
	}

	// Switching the definition domain off is an exclusion; the mutation answers
	// with the contents the card reconciles to.
	off := rows(postJSON(t, origin+"/v1/lists/"+listID+"/domains", `{"values":["2ip.io"],"verdict":"exclude"}`))
	if row := off["2ip.io"]; row.Enabled || !row.Definition {
		t.Fatalf("switched-off row = %+v", row)
	}
	if switched := rebuild(); strings.Contains(switched, "2ip.io") || !strings.Contains(switched, "myip.wtf") {
		t.Fatalf("switched-off file = %q", switched)
	}

	on := rows(postJSON(t, origin+"/v1/lists/"+listID+"/domains", `{"values":["2ip.io"],"verdict":"auto"}`))
	if row := on["2ip.io"]; !row.Enabled || !row.Definition {
		t.Fatalf("switched-on row = %+v", row)
	}
	if restored := rebuild(); !strings.Contains(restored, "2ip.io") || !strings.Contains(restored, "myip.wtf") {
		t.Fatalf("restored file = %q", restored)
	}
}
