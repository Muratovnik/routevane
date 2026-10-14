package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestServeNamesTheListsABuildCouldNotCover is the visible outcome of ADR 0053:
// a profile holding lists whose required part no rule of the format covers is
// refused, the refusal names every such list by identity and title and not the
// list that is covered, and the failed attempt keeps them, so the profile page
// names them again after a reload and after a restart.
func TestServeNamesTheListsABuildCouldNotCover(t *testing.T) {
	catalog := writeAlphaBetaCatalog(t)
	// Both lists have a required component and nothing that can reach this
	// format: no seed, no source.
	for id, title := range map[string]string{"gamma": "Гамма", "delta": "Дельта"} {
		list := "id: " + id + "\ntitle: " + title + "\ncomponents:\n  web:\n    required: true\n"
		if err := os.WriteFile(filepath.Join(catalog, "builtin", id+".yaml"), []byte(list), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	want := []map[string]string{{"id": "delta", "title": "Дельта"}, {"id": "gamma", "title": "Гамма"}}

	type refusal struct {
		Code           string              `json:"code"`
		UncoveredLists []map[string]string `json:"uncovered_lists"`
	}
	var profileID, outputID string
	run := func(first bool) {
		t.Helper()
		origin, cancel, done, stderr := startAlphaBetaServeServer(t, catalog, data, now)
		defer func() {
			cancel()
			if code := <-done; code != 0 {
				t.Errorf("server code=%d stderr=%s", code, stderr.String())
			}
		}()
		if first {
			var created struct {
				Profile struct {
					ID string `json:"id"`
				} `json:"profile"`
			}
			if err := json.Unmarshal(postJSON(t, origin+"/v1/profiles", `{"name":"Неполный","lists":["alpha","delta","gamma"]}`), &created); err != nil {
				t.Fatal(err)
			}
			profileID = created.Profile.ID
			var output struct {
				Output struct {
					ID string `json:"id"`
				} `json:"output"`
			}
			if err := json.Unmarshal(postJSON(t, origin+"/v1/profiles/"+profileID+"/outputs", `{"target_id":"keenetic"}`), &output); err != nil {
				t.Fatal(err)
			}
			outputID = output.Output.ID

			// The lists are read once before a build can plan from them.
			postJSON(t, origin+"/v1/profiles/"+profileID+"/refresh", `{}`)
			built := postGuardedBody(t, origin+"/v1/outputs/"+outputID+"/build", `{}`)
			var body refusal
			if err := json.Unmarshal(built.body, &body); err != nil {
				t.Fatalf("build body=%s: %v", built.body, err)
			}
			if built.status != http.StatusUnprocessableEntity || body.Code != "partial_coverage" || !reflect.DeepEqual(body.UncoveredLists, want) {
				t.Fatalf("build status=%d body=%s", built.status, built.body)
			}
		}
		var detail struct {
			Outputs []struct {
				Latest      json.RawMessage `json:"latest"`
				LastAttempt struct {
					Status         string              `json:"status"`
					Code           string              `json:"code"`
					UncoveredLists []map[string]string `json:"uncovered_lists"`
				} `json:"last_attempt"`
			} `json:"outputs"`
		}
		page := httpGet(t, origin+"/v1/profiles/"+profileID, nil)
		if err := json.Unmarshal(page.body, &detail); err != nil || len(detail.Outputs) != 1 {
			t.Fatalf("profile page=%s err=%v", page.body, err)
		}
		attempt := detail.Outputs[0].LastAttempt
		if attempt.Status != "failed" || attempt.Code != "partial_coverage" || !reflect.DeepEqual(attempt.UncoveredLists, want) {
			t.Fatalf("stored attempt (first run: %v) = %#v", first, attempt)
		}
		// A refused build publishes nothing.
		if latest := string(detail.Outputs[0].Latest); latest != "" && latest != "null" {
			t.Fatalf("a refused build published %s", latest)
		}
	}
	run(true)
	run(false)
}
