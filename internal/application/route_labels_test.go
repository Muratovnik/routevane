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
	service, _ := overlayTestService(t)
	if _, err := service.UpdateCategory(context.Background(), "video", CategoryUpdate{Services: &[]string{"youtube", "discord"}}); err != nil {
		t.Fatal(err)
	}
	custom, err := service.CreateCategory(context.Background(), "Связь/(личное)+", []string{"discord"})
	if err != nil {
		t.Fatal(err)
	}
	definitions := []domain.ServiceDefinition{service.config.Definitions["discord"], service.config.Definitions["roblox"]}
	labels := routeLabelsByService(definitions, service.mergedCategories())
	wantDiscord := []string{"(Видео/Discord)", "(Игры/Discord)", "(Связь／（личное）＋/Discord)"}
	if !reflect.DeepEqual(labels["discord"], wantDiscord) {
		t.Fatalf("discord labels = %#v, custom category=%s", labels["discord"], custom.ID)
	}
	if !reflect.DeepEqual(labels["roblox"], []string{"(Игры/Roblox)"}) {
		t.Fatalf("roblox labels = %#v", labels["roblox"])
	}
}

func TestRouteLabelsKeepUnicodeAndNameAnUncategorizedList(t *testing.T) {
	definitions := []domain.ServiceDefinition{{ID: "custom-list", Title: "  Мой\tсписок/東京  "}}
	labels := routeLabelsByService(definitions, nil)
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
		TargetID:         "keenetic", ProfileKey: "keenetic-bat-ipv4-v1", Services: []string{"youtube"}, Rules: []domain.RouteRule{rule},
		PolicyVersion: planner.PolicyVersion, CatalogRevision: strings.Repeat("c", 64),
	}
	target := domain.TargetProfile{ID: plan.TargetID, ProfileKey: plan.ProfileKey, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true}}
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
