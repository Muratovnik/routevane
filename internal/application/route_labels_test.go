package application

import (
	"context"
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planjson"
	"github.com/Muratovnik/routevane/internal/planner"
)

func TestRouteLabelsFollowMergedCategoryOverlayAndUseTitles(t *testing.T) {
	publication, _ := overlayTestService(t)
	if _, err := publication.UpdateCategory(context.Background(), "video", CategoryUpdate{Lists: &[]string{"youtube", "discord"}}); err != nil {
		t.Fatal(err)
	}
	custom, err := publication.CreateCategory(context.Background(), "Связь/(личное)+", []string{"discord"})
	if err != nil {
		t.Fatal(err)
	}
	definitions := []domain.ListDefinition{publication.config.Definitions["discord"], publication.config.Definitions["roblox"]}
	labels := routeLabelsByList(definitions, publication.mergedCategories())
	wantDiscord := []string{"(Видео/Discord)", "(Игры/Discord)", "(Связь／（личное）＋/Discord)"}
	if !reflect.DeepEqual(labels["discord"], wantDiscord) {
		t.Fatalf("discord labels = %#v, custom category=%s", labels["discord"], custom.ID)
	}
	if !reflect.DeepEqual(labels["roblox"], []string{"(Игры/Roblox)"}) {
		t.Fatalf("roblox labels = %#v", labels["roblox"])
	}
}

func TestRouteLabelsKeepUnicodeAndNameAnUncategorizedList(t *testing.T) {
	definitions := []domain.ListDefinition{{ID: "custom-list", Title: "  Мой\tсписок/東京  "}}
	labels := routeLabelsByList(definitions, nil)
	want := []string{"(Без категории/Мой список／東京)"}
	if !reflect.DeepEqual(labels["custom-list"], want) {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestRouteLabelsAreStoredInPlanJSONAndChangeTheSemanticHash(t *testing.T) {
	rule, err := domain.NewAddrRule(netip.MustParseAddr("192.0.2.1"), "youtube", "web", domain.SourceOfficial, []string{planner.ReasonOfficialRule}, []string{"catalog"})
	if err != nil {
		t.Fatal(err)
	}
	plan := domain.RoutingPlan{
		InterfaceVersion: domain.RoutingPlanInterfaceVersion,
		TargetID:         "keenetic", FormatKey: "keenetic-bat-ipv4-v1", Lists: []string{"youtube"}, Rules: []domain.RouteRule{rule},
		PolicyVersion: planner.PolicyVersion, CatalogRevision: strings.Repeat("c", 64),
	}
	target := domain.TargetDefinition{ID: plan.TargetID, FormatKey: plan.FormatKey, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true}}
	without := planner.SemanticHash(plan, target)
	applyRouteLabels(&plan, map[string][]string{"youtube": {"(Видео/YouTube)"}})
	planner.CanonicalizePlan(&plan)
	plan.SemanticHash = planner.SemanticHash(plan, target)
	if plan.SemanticHash == without {
		t.Fatal("route labels did not change the semantic hash")
	}
	encoded, err := planjson.Encode(plan)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot planjson.Plan
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Rules) != 1 || !reflect.DeepEqual(snapshot.Rules[0].Labels, []string{"(Видео/YouTube)"}) {
		t.Fatalf("snapshot = %s", encoded)
	}
}
