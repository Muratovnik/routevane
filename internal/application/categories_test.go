package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

// countingEntropy hands out a different value on every read. A reader that
// repeats itself would make every generated identity the same one, which is a
// property of the fixture rather than of the code under test.
type countingEntropy struct{ n byte }

func (e *countingEntropy) Read(p []byte) (int, error) {
	for i := range p {
		e.n++
		p[i] = e.n
	}
	return len(p), nil
}

// overlayTestService wires the shape ADR 0028 is about: a shipped catalog with
// its own categories, and a store the operator's overlay lives in.
func overlayTestService(t *testing.T) (*PublicationService, *publicationFakeStore) {
	t.Helper()
	definitions := map[string]domain.ServiceDefinition{}
	for _, id := range []string{"youtube", "discord", "roblox"} {
		definitions[id] = domain.ServiceDefinition{
			ID: id, Title: strings.ToUpper(id[:1]) + id[1:], CatalogRevision: strings.Repeat("c", 64),
			Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
			Seeds:      []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:seed", SourceClass: domain.SourceManual}},
		}
	}
	categories := map[string]domain.CategoryDefinition{
		"video": {ID: "video", Title: "Видео", Services: []string{"youtube"}},
		"games": {ID: "games", Title: "Игры", Services: []string{"discord", "roblox"}},
	}
	target := domain.TargetProfile{ID: "keenetic", ProfileKey: keenetic.Version, RendererID: keenetic.ID, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize}}
	store := &publicationFakeStore{}
	service, err := NewPublicationService(PublicationConfig{
		Definitions: definitions, Categories: categories,
		Targets: map[string]domain.TargetProfile{target.ID: target}, TargetRevision: strings.Repeat("t", 64),
		Store: store, Files: &publicationFakeFiles{},
		Renderers: RendererRegistry{keenetic.ID: keenetic.Renderer{}},
		Sources:   SourceRegistry{domain.SourceDNS: publicationFakeSource{}},
		Clock:     ClockFunc(func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }),
		Entropy:   &countingEntropy{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func categoryTitles(details []CategoryDetail) string {
	parts := make([]string, 0, len(details))
	for _, detail := range details {
		mark := ""
		if detail.Custom {
			mark = "*"
		}
		parts = append(parts, detail.ID+mark+"="+strings.Join(detail.Services, "+"))
	}
	return strings.Join(parts, " ")
}

// A category the operator created joins every reader at once: the listing, the
// membership a service reports, and composition validation.
func TestCreateCategoryStoresItsMembershipAndJoinsEveryReader(t *testing.T) {
	service, store := overlayTestService(t)
	created, err := service.CreateCategory(context.Background(), "  Мои списки  ", []string{"roblox", "youtube", "roblox"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ID, CustomCategoryIDPrefix) || len(created.ID) != len(CustomCategoryIDPrefix)+16 {
		t.Fatalf("identity = %q", created.ID)
	}
	if created.Title != "Мои списки" || !created.Custom || !reflect.DeepEqual(created.Services, []string{"roblox", "youtube"}) {
		t.Fatalf("created = %#v", created)
	}
	// The store holds the verdicts, not a copy of the catalog: a category the
	// operator created has no catalog membership, so every row is 'added'.
	rows := store.memberships[created.ID]
	if len(rows) != 2 {
		t.Fatalf("stored membership = %#v", rows)
	}
	for _, row := range rows {
		if row.State != MembershipAdded {
			t.Fatalf("stored verdict = %#v", row)
		}
	}
	listed := service.Categories()
	if got := categoryTitles(listed); got != created.ID+"*=roblox+youtube games=discord+roblox video=youtube" {
		t.Fatalf("categories = %s", got)
	}
	// A shipped category answers custom:false rather than omitting the field.
	for _, detail := range listed {
		if detail.ID == "video" && detail.Custom {
			t.Fatalf("a shipped category reported itself as operator-created: %#v", detail)
		}
	}
	for _, detail := range service.ServiceDetails() {
		if detail.ID != "roblox" {
			continue
		}
		if !reflect.DeepEqual(detail.Categories, []string{created.ID, "games"}) {
			t.Fatalf("service categories = %#v", detail.Categories)
		}
	}
	// A route may name it exactly as it names a shipped one.
	list, err := service.CreateList(context.Background(), "Маршрут", ListComposition{Categories: []string{created.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved := service.ResolvedServices(list); !reflect.DeepEqual(resolved, []string{"roblox", "youtube"}) {
		t.Fatalf("resolved = %#v", resolved)
	}
}

// Adding a service to a shipped category reaches every reader without touching
// the catalog the process loaded: the overlay is a difference, not a rewrite.
func TestAddingAServiceToACatalogCategoryReachesEveryReaderAsAnOverlay(t *testing.T) {
	service, store := overlayTestService(t)
	updated, err := service.UpdateCategory(context.Background(), "video", CategoryUpdate{Services: &[]string{"youtube", "discord"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Custom || updated.Title != "Видео" || !reflect.DeepEqual(updated.Services, []string{"discord", "youtube"}) {
		t.Fatalf("updated = %#v", updated)
	}
	// Only the disagreement is stored. youtube is already the catalog's answer,
	// so recording it would be a second copy of the catalog waiting to go stale.
	rows := store.memberships["video"]
	if len(rows) != 1 || rows[0].ServiceID != "discord" || rows[0].State != MembershipAdded {
		t.Fatalf("stored membership = %#v", rows)
	}
	if catalog := service.config.Categories["video"].Services; !reflect.DeepEqual(catalog, []string{"youtube"}) {
		t.Fatalf("the loaded catalog was rewritten: %#v", catalog)
	}
	list := listWith(nil, []string{"video"}, nil)
	if got := joined(service.ResolvedServices(list)); got != "discord,youtube" {
		t.Fatalf("resolved = %s", got)
	}
}

// A removed verdict hides a catalog membership without deleting anything the
// catalog says. The catalog keeps its answer; the installation gives another.
func TestRemovedHidesACatalogMembershipWithoutRewritingTheCatalog(t *testing.T) {
	service, store := overlayTestService(t)
	updated, err := service.UpdateCategory(context.Background(), "games", CategoryUpdate{Services: &[]string{"discord"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.Services, []string{"discord"}) {
		t.Fatalf("updated = %#v", updated)
	}
	rows := store.memberships["games"]
	if len(rows) != 1 || rows[0].ServiceID != "roblox" || rows[0].State != MembershipRemoved {
		t.Fatalf("stored membership = %#v", rows)
	}
	if catalog := service.config.Categories["games"].Services; !reflect.DeepEqual(catalog, []string{"discord", "roblox"}) {
		t.Fatalf("the loaded catalog was rewritten: %#v", catalog)
	}
	if got := joined(service.ResolvedServices(listWith(nil, []string{"games"}, nil))); got != "discord" {
		t.Fatalf("resolved = %s", got)
	}
	// Restating the catalog's own membership clears the overlay rather than
	// storing an agreement.
	if _, err := service.UpdateCategory(context.Background(), "games", CategoryUpdate{Services: &[]string{"discord", "roblox"}}); err != nil {
		t.Fatal(err)
	}
	if rows := store.memberships["games"]; len(rows) != 0 {
		t.Fatalf("agreement was stored: %#v", rows)
	}
}

// An operator category carries whatever the operator puts in it, shipped and
// operator-defined services alike. The merged accessor does not care which
// half of the catalog a service came from.
func TestACustomCategoryCarriesShippedAndOperatorDefinedServices(t *testing.T) {
	service, _ := overlayTestService(t)
	custom, err := service.CreateCustomService(context.Background(), "Мой сервис", []string{"a.example"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateCategory(context.Background(), "Смешанная", []string{"youtube", custom.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.Services, []string{custom.ID, "youtube"}) {
		t.Fatalf("created = %#v", created)
	}
	if got := joined(service.ResolvedServices(listWith(nil, []string{created.ID}, nil))); got != custom.ID+",youtube" {
		t.Fatalf("resolved = %s", got)
	}
	// A shipped category may take an operator-defined service too.
	if _, err := service.UpdateCategory(context.Background(), "video", CategoryUpdate{Services: &[]string{"youtube", custom.ID}}); err != nil {
		t.Fatal(err)
	}
	if got := joined(service.ResolvedServices(listWith(nil, []string{"video"}, nil))); got != custom.ID+",youtube" {
		t.Fatalf("resolved = %s", got)
	}
}

// A shipped category is renamed by no one, because the next catalog load would
// answer differently; whether it exists at all belongs to the operator, who
// owns the library (ADR 0029). A category the operator created is theirs on
// both counts.
func TestACatalogCategoryIsRenamedByNoOneAndDeletedByTheOperator(t *testing.T) {
	service, _ := overlayTestService(t)
	title := "Видео 2"
	if _, err := service.UpdateCategory(context.Background(), "video", CategoryUpdate{Title: &title}); !errors.Is(err, ErrCatalogCategory) {
		t.Fatalf("rename err = %v", err)
	}
	if err := service.RemoveCategory(context.Background(), "video", CategoryListsDetach); err != nil {
		t.Fatalf("delete err = %v", err)
	}
	if _, ok := service.mergedCategory("video"); ok {
		t.Fatal("a deleted catalog category is still readable")
	}
	// Detaching leaves the list it held in the library, belonging to no
	// category rather than gone with the category.
	if !service.knownService("youtube") {
		t.Fatal("detaching took the category's list with it")
	}
	created, err := service.CreateCategory(context.Background(), "Мои списки", []string{"youtube"})
	if err != nil {
		t.Fatal(err)
	}
	renamed := "Мои списки 2"
	updated, err := service.UpdateCategory(context.Background(), created.ID, CategoryUpdate{Title: &renamed})
	if err != nil {
		t.Fatal(err)
	}
	// A rename mentions no membership, so it must leave it alone.
	if updated.Title != renamed || !reflect.DeepEqual(updated.Services, []string{"youtube"}) {
		t.Fatalf("renamed = %#v", updated)
	}
	if err := service.RemoveCategory(context.Background(), created.ID, CategoryListsDetach); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.mergedCategory(created.ID); ok {
		t.Fatal("a deleted category is still readable")
	}
	if err := service.RemoveCategory(context.Background(), created.ID, CategoryListsDetach); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second deletion err = %v", err)
	}
}

// Deleting a category a route still names is refused with the routes named. A
// route that silently lost a category would build something its author did not
// choose, and an archived route counts: restoring it would do exactly that.
func TestRemovingACategoryARouteNamesIsRefusedWithTheRoutes(t *testing.T) {
	service, store := overlayTestService(t)
	created, err := service.CreateCategory(context.Background(), "Мои списки", []string{"youtube"})
	if err != nil {
		t.Fatal(err)
	}
	store.lists = []List{
		{ID: strings.Repeat("b", 32), Name: "Дом", Categories: []string{created.ID}},
		{ID: strings.Repeat("a", 32), Name: "Офис", Categories: []string{created.ID}, ArchivedAt: time.Unix(0, 1).UTC()},
		{ID: strings.Repeat("c", 32), Name: "Другой", Categories: []string{"video"}},
	}
	err = service.RemoveCategory(context.Background(), created.ID, CategoryListsDetach)
	var inUse CategoryInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("err = %v", err)
	}
	want := []ListReference{
		{ID: strings.Repeat("a", 32), Title: "Офис"},
		{ID: strings.Repeat("b", 32), Title: "Дом"},
	}
	if inUse.CategoryID != created.ID || !reflect.DeepEqual(inUse.Lists, want) {
		t.Fatalf("in use = %#v", inUse)
	}
	if _, ok := service.mergedCategory(created.ID); !ok {
		t.Fatal("a refused deletion removed the category anyway")
	}
	// Once no route names it, the deletion goes through.
	store.lists = []List{{ID: strings.Repeat("c", 32), Name: "Другой", Categories: []string{"video"}}}
	if err := service.RemoveCategory(context.Background(), created.ID, CategoryListsDetach); err != nil {
		t.Fatal(err)
	}
}

// The point of a category: a membership change reaches the routes that name it
// on their next build, and reaches nothing else. The forecast gains the added
// service's rules and the plan's semantic hash changes, which is what makes the
// next build produce a different artifact rather than reuse the last one.
func TestAMembershipChangeReachesTheNextForecastAndPlan(t *testing.T) {
	store := &publicationFakeStore{}
	service := forecastTestService(t, store, &publicationFakeFiles{})
	ctx := context.Background()
	composition := ListComposition{Categories: []string{"video"}}
	route := listWith(nil, []string{"video"}, nil)
	target, renderer, err := service.target("unbounded")
	if err != nil {
		t.Fatal(err)
	}

	before, err := service.ForecastComposition(ctx, composition, []string{"unbounded"})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || !reflect.DeepEqual(before[0].PerService, []ServiceRuleForecast{{ServiceID: "youtube", Rules: 3}}) {
		t.Fatalf("forecast before = %#v", before)
	}
	planBefore, _, err := service.prepareList(ctx, route, target, renderer)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.UpdateCategory(ctx, "video", CategoryUpdate{Services: &[]string{"youtube", "discord"}}); err != nil {
		t.Fatal(err)
	}

	after, err := service.ForecastComposition(ctx, composition, []string{"unbounded"})
	if err != nil {
		t.Fatal(err)
	}
	wantPerService := []ServiceRuleForecast{{ServiceID: "discord", Rules: 2}, {ServiceID: "youtube", Rules: 2}}
	if len(after) != 1 || !reflect.DeepEqual(after[0].PerService, wantPerService) {
		t.Fatalf("forecast after = %#v", after)
	}
	if after[0].ProjectedRules <= before[0].ProjectedRules {
		t.Fatalf("projection did not grow: before=%d after=%d", before[0].ProjectedRules, after[0].ProjectedRules)
	}
	planAfter, _, err := service.prepareList(ctx, route, target, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(planAfter.Plan.Services, []string{"discord", "youtube"}) {
		t.Fatalf("planned services = %#v", planAfter.Plan.Services)
	}
	if planAfter.Plan.SemanticHash == planBefore.Plan.SemanticHash {
		t.Fatal("the semantic hash did not change, so the next build would reuse the previous artifact")
	}
	// It reaches only the routes that name the category. One naming the service
	// set directly describes the same plan it did before.
	direct := listWith([]string{"youtube"}, nil, nil)
	planDirect, _, err := service.prepareList(ctx, direct, target, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if planDirect.Plan.SemanticHash != planBefore.Plan.SemanticHash {
		t.Fatal("a route naming no category changed with the overlay")
	}
}

func TestCategoryEditsRejectInvalidInput(t *testing.T) {
	service, _ := overlayTestService(t)
	ctx := context.Background()
	created, err := service.CreateCategory(ctx, "Мои списки", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A category may legitimately start empty; it simply publishes nothing yet.
	if len(created.Services) != 0 {
		t.Fatalf("created = %#v", created)
	}
	longTitle := strings.Repeat("я", maxCategoryTitleRunes+1)
	creations := []struct {
		name     string
		title    string
		services []string
	}{
		{"empty title", "   ", nil},
		{"control character", "a\x00b", nil},
		{"title beyond the bound", longTitle, nil},
		{"unknown service", "Списки", []string{"absent"}},
		{"invalid service id", "Списки", []string{"Not A Slug"}},
		{"too many services", "Списки", manyServices(maxCategoryMembers + 1)},
	}
	for _, tc := range creations {
		if _, err := service.CreateCategory(ctx, tc.title, tc.services); err == nil {
			t.Errorf("create %s: accepted", tc.name)
		}
	}
	if _, err := service.UpdateCategory(ctx, "absent", CategoryUpdate{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown category err = %v", err)
	}
	if err := service.RemoveCategory(ctx, "absent", CategoryListsDetach); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown deletion err = %v", err)
	}
	// The disposition is part of the request's shape: an unstated or unknown
	// answer is refused before the library is consulted, because "detach" and
	// "delete" are different outcomes for the operator's own data.
	for _, disposition := range []CategoryListDisposition{"", "purge", "Detach"} {
		if err := service.RemoveCategory(ctx, created.ID, disposition); err == nil {
			t.Errorf("disposition %q was accepted", disposition)
		}
	}
	if _, err := service.UpdateCategory(ctx, created.ID, CategoryUpdate{Title: &longTitle}); err == nil {
		t.Error("a title beyond the bound was accepted")
	}
	if _, err := service.UpdateCategory(ctx, created.ID, CategoryUpdate{Services: &[]string{"absent"}}); err == nil {
		t.Error("an unknown service was accepted")
	}
	// A route may not name a category that does not exist, before or after the
	// overlay is consulted.
	if _, err := service.CreateList(ctx, "Маршрут", ListComposition{Categories: []string{"absent"}}); err == nil {
		t.Error("a composition named an unknown category")
	}
}

func manyServices(count int) []string {
	services := make([]string, 0, count)
	for i := 0; i < count; i++ {
		services = append(services, "service-"+strings.Repeat("a", i%8+1)+"-"+string(rune('a'+i%26)))
	}
	return services
}

func TestLoadCategoriesRefusesAStoreThatCannotBeServed(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	cases := []struct {
		name    string
		overlay CategoryOverlay
	}{
		{"identity outside the reserved prefix", CategoryOverlay{
			Categories: []CustomCategory{{ID: "stolen-id", Title: "X", CreatedAt: now, UpdatedAt: now}},
		}},
		{"identity colliding with the catalog", CategoryOverlay{
			Categories: []CustomCategory{{ID: "video", Title: "X", CreatedAt: now, UpdatedAt: now}},
		}},
		{"title the grammar refuses", CategoryOverlay{
			Categories: []CustomCategory{{ID: "custom-1234567890abcdef", Title: "a\x00b", CreatedAt: now, UpdatedAt: now}},
		}},
		{"removed verdict on an operator category", CategoryOverlay{
			Categories:  []CustomCategory{{ID: "custom-1234567890abcdef", Title: "X", CreatedAt: now, UpdatedAt: now}},
			Memberships: []CategoryMembership{{CategoryID: "custom-1234567890abcdef", ServiceID: "youtube", State: MembershipRemoved, UpdatedAt: now}},
		}},
		{"unknown verdict", CategoryOverlay{
			Memberships: []CategoryMembership{{CategoryID: "video", ServiceID: "youtube", State: "quarantined", UpdatedAt: now}},
		}},
	}
	for _, tc := range cases {
		service, store := overlayTestService(t)
		store.categories = map[string]CustomCategory{}
		for _, category := range tc.overlay.Categories {
			store.categories[category.ID] = category
		}
		store.memberships = map[string][]CategoryMembership{}
		for _, membership := range tc.overlay.Memberships {
			store.memberships[membership.CategoryID] = append(store.memberships[membership.CategoryID], membership)
		}
		if err := service.LoadCategories(context.Background()); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

// A stored overlay survives a restart as the same merged answer.
func TestLoadCategoriesRestoresTheMergedAnswer(t *testing.T) {
	service, store := overlayTestService(t)
	ctx := context.Background()
	created, err := service.CreateCategory(ctx, "Мои списки", []string{"roblox"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateCategory(ctx, "games", CategoryUpdate{Services: &[]string{"discord", "youtube"}}); err != nil {
		t.Fatal(err)
	}
	before := categoryTitles(service.Categories())

	restarted, _ := overlayTestService(t)
	restarted.config.Store = store
	if err := restarted.LoadCategories(ctx); err != nil {
		t.Fatal(err)
	}
	if after := categoryTitles(restarted.Categories()); after != before {
		t.Fatalf("restart answered %s, want %s", after, before)
	}
	if _, ok := restarted.mergedCategory(created.ID); !ok {
		t.Fatal("the created category did not survive the restart")
	}
}

// The overlay is a difference against the catalog, so applying it to the
// catalog must reproduce exactly the membership the operator asked for —
// whatever the two sets are.
func FuzzCategoryMembershipDifferenceReproducesTheRequest(f *testing.F) {
	universe := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	f.Add(uint8(0b00000), uint8(0b11111))
	f.Add(uint8(0b10101), uint8(0b01010))
	f.Add(uint8(0b11111), uint8(0b11111))
	f.Fuzz(func(t *testing.T, catalogMask, desiredMask uint8) {
		catalog := selected(universe, catalogMask)
		desired := selected(universe, desiredMask)
		rows := membershipRows("video", desired, catalog, time.Unix(1, 0).UTC())
		verdicts := membershipIndex(rows)
		merged := make([]string, 0, len(universe))
		for _, id := range catalog {
			if verdicts[id] != MembershipRemoved {
				merged = append(merged, id)
			}
		}
		for id, state := range verdicts {
			if state == MembershipAdded {
				merged = append(merged, id)
			}
		}
		if got := domain.StableStrings(merged); !reflect.DeepEqual(got, domain.StableStrings(desired)) {
			t.Fatalf("catalog=%v desired=%v rows=%v merged=%v", catalog, desired, rows, got)
		}
		// Nothing the two already agree on is stored: an overlay that repeated
		// the catalog would be a copy of it waiting to go stale.
		carried := make(map[string]struct{}, len(catalog))
		for _, id := range catalog {
			carried[id] = struct{}{}
		}
		for _, row := range rows {
			_, inCatalog := carried[row.ServiceID]
			if (row.State == MembershipAdded) == inCatalog {
				t.Fatalf("agreement stored: catalog=%v desired=%v row=%#v", catalog, desired, row)
			}
		}
	})
}

func selected(universe []string, mask uint8) []string {
	chosen := make([]string, 0, len(universe))
	for i, id := range universe {
		if mask&(1<<uint(i)) != 0 {
			chosen = append(chosen, id)
		}
	}
	return chosen
}

// A title the grammar accepts is trimmed, bounded, free of control characters,
// and unchanged by a second pass: a name that changed on re-validation would
// mean the store and the screen disagree about what was saved.
func FuzzCategoryTitleGrammar(f *testing.F) {
	for _, seed := range []string{"Видео", "  Игры  ", "", "a\x00b", strings.Repeat("я", 200), "\x7f", "ok\tname"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		clean, ok := validCategoryTitle(raw)
		if !ok {
			if clean != "" {
				t.Fatalf("a refused title returned %q", clean)
			}
			return
		}
		if clean != strings.TrimSpace(clean) || clean == "" {
			t.Fatalf("accepted %q", clean)
		}
		if count := len([]rune(clean)); count > maxCategoryTitleRunes {
			t.Fatalf("accepted %d runes", count)
		}
		for _, r := range clean {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("accepted a control character in %q", clean)
			}
		}
		again, ok := validCategoryTitle(clean)
		if !ok || again != clean {
			t.Fatalf("second pass turned %q into %q (ok=%v)", clean, again, ok)
		}
	})
}

// A stored membership naming a service the catalog no longer ships is dropped
// at read time rather than refused at startup. Refusing would make a catalog
// edit unbootable; promising the service would name something no plan can
// carry. It is the same answer composition resolution gives, for the same
// reason, and the verdict stays stored so a returning service returns with it.
func TestAMembershipNamingAVanishedServiceIsDroppedRatherThanPromised(t *testing.T) {
	service, store := overlayTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store.categories = map[string]CustomCategory{
		"custom-1234567890abcdef": {ID: "custom-1234567890abcdef", Title: "Мои списки", CreatedAt: now, UpdatedAt: now},
	}
	store.memberships = map[string][]CategoryMembership{
		"custom-1234567890abcdef": {
			{CategoryID: "custom-1234567890abcdef", ServiceID: "youtube", State: MembershipAdded, UpdatedAt: now},
			{CategoryID: "custom-1234567890abcdef", ServiceID: "retired", State: MembershipAdded, UpdatedAt: now},
		},
		"video": {{CategoryID: "video", ServiceID: "retired", State: MembershipAdded, UpdatedAt: now}},
	}
	if err := service.LoadCategories(ctx); err != nil {
		t.Fatalf("a catalog that dropped a service made the process unbootable: %v", err)
	}
	created, ok := service.mergedCategory("custom-1234567890abcdef")
	if !ok || !reflect.DeepEqual(created.Services, []string{"youtube"}) {
		t.Fatalf("operator category = %#v ok=%v", created, ok)
	}
	shipped, _ := service.mergedCategory("video")
	if !reflect.DeepEqual(shipped.Services, []string{"youtube"}) {
		t.Fatalf("shipped category = %#v", shipped)
	}
	if got := joined(service.ResolvedServices(listWith(nil, []string{"video"}, nil))); got != "youtube" {
		t.Fatalf("resolved = %s", got)
	}
	// The verdict is still stored: a service that returns to the catalog
	// returns to the category the operator put it in.
	if rows := store.memberships["video"]; len(rows) != 1 || rows[0].ServiceID != "retired" {
		t.Fatalf("the read discarded a stored verdict: %#v", rows)
	}
}
