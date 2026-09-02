package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

func TestServeOverlapForecastExplainsTwoProjectionsWithoutRewritingLists(t *testing.T) {
	origin, cancel, done, stderr := startServeServerWithHostResolver(t, filepath.Join("..", "..", "catalog"), filepath.Join(t.TempDir(), "data"), &hostAddressResolver{}, func() time.Time { return time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC) })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Errorf("server=%d %s", code, stderr.String())
		}
	}()
	ids := []string{}
	for _, fixture := range []struct{ title, domain, value string }{{"Overlap Alpha", "alpha.example", "192.0.2.0/24"}, {"Overlap Beta", "beta.example", "192.0.2.1"}} {
		body, _ := json.Marshal(map[string]any{"title": fixture.title, "domains": []string{"shared.example", fixture.domain}})
		var created struct {
			Service struct {
				ID string `json:"id"`
			} `json:"service"`
		}
		if err := json.Unmarshal(postJSON(t, origin+"/v1/services", string(body)), &created); err != nil {
			t.Fatal(err)
		}
		id := created.Service.ID
		ids = append(ids, id)
		value, _ := json.Marshal(map[string]any{"values": []string{fixture.value}, "verdict": "include"})
		postJSON(t, origin+"/v1/services/"+id+"/domains", string(value))
		postJSON(t, origin+"/v1/services/"+id+"/refresh", `{}`)
	}
	read := func(selected []string) []application.CompositionForecast {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"services": selected, "targets": []string{"keenetic-dns", "singbox"}})
		var result struct {
			Targets []application.CompositionForecast `json:"targets"`
		}
		if err := json.Unmarshal(postJSON(t, origin+"/v1/lists/preview", string(body)), &result); err != nil {
			t.Fatal(err)
		}
		return result.Targets
	}
	beforeLists := httpGet(t, origin+"/v1/lists", nil)
	beforeContents := [][]byte{}
	for _, id := range ids {
		beforeContents = append(beforeContents, httpGet(t, origin+"/v1/services/"+id+"/contents", nil).body)
	}
	a, b := read(ids[:1]), read(ids[1:])
	ab := read(ids)
	if !reflect.DeepEqual(a, read(ids[:1])) || !reflect.DeepEqual(b, read(ids[1:])) {
		t.Fatal("combined forecast rewrote individual lists")
	}
	if after := httpGet(t, origin+"/v1/lists", nil); after.status != http.StatusOK || !slices.Equal(beforeLists.body, after.body) {
		t.Fatal("forecast created a route or output")
	}
	for i, id := range ids {
		if !slices.Equal(beforeContents[i], httpGet(t, origin+"/v1/services/"+id+"/contents", nil).body) {
			t.Fatal("forecast changed list contents")
		}
	}
	for i, forecast := range ab {
		if forecast.Overlaps.Truncated || len(a[i].Overlaps.Items) != 0 || len(b[i].Overlaps.Items) != 0 {
			t.Fatalf("forecast=%#v", forecast)
		}
		wantCount, wantRelations := 6, 2
		if forecast.TargetID == "singbox" {
			wantCount, wantRelations = 5, 2
		}
		if forecast.ProjectedRules != wantCount || len(forecast.Overlaps.Items) != wantRelations {
			t.Fatalf("forecast=%#v", forecast)
		}
		duplicate := forecast.Overlaps.Items[0]
		owners := slices.Clone(ids)
		slices.Sort(owners)
		if duplicate.Kind != "duplicate" || duplicate.Entry.Value != "shared.example" || !slices.Equal(duplicate.Entry.Services, owners) {
			t.Fatalf("duplicate=%#v", duplicate)
		}
		if wantRelations == 2 {
			coverage := forecast.Overlaps.Items[1]
			if coverage.Kind != "covered" || coverage.Covering == nil || coverage.Entry.Value != "192.0.2.1" || coverage.Covering.Value != "192.0.2.0/24" {
				t.Fatalf("coverage=%#v", coverage)
			}
		}
		// Publishing afterward proves the forecast against real bytes. Keenetic
		// keeps the shared domain in both service groups; sing-box deduplicates it.
		listID, outputID, _ := createListOutput(t, origin, "Overlap route", forecast.TargetID, ids...)
		built := guardedRefreshAndBuild(t, origin, listID, outputID)
		payload := downloadArtifact(t, origin, built.Artifact.ID).body
		actual := 0
		if forecast.TargetID == "keenetic-dns" {
			groups, err := keeneticdns.Parse(payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(groups) != 2 {
				t.Fatalf("lost group ownership: %#v", groups)
			}
			for _, group := range groups {
				actual += len(group.Entries)
			}
		} else {
			parsed, err := singbox.Parse(payload)
			if err != nil {
				t.Fatal(err)
			}
			actual = len(parsed.Rule.Domain) + len(parsed.Rule.DomainSuffix) + len(parsed.Rule.IPCIDR)
		}
		planned := 0
		for _, share := range forecast.PerService {
			planned += share.Rules
		}
		if actual != forecast.ProjectedRules || built.Summary.RuleCount != planned {
			t.Fatalf("forecast=%d artifact=%d summary=%d", forecast.ProjectedRules, actual, built.Summary.RuleCount)
		}
	}
}
