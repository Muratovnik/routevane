package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A list the operator removed is gone from every reader of the catalog at
// once: the picker's list of ids, the details beside them, the membership of
// every category that held it, composition validation, and the resolution a
// build plans from. There is one subtraction, in the accessor an id is
// resolved through, so none of them can disagree with the others (ADR 0029).
func TestRemovingAListSubtractsItFromEveryReaderOfTheCatalog(t *testing.T) {
	service, store := overlayTestService(t)
	ctx := context.Background()
	if err := service.RemoveService(ctx, "roblox"); err != nil {
		t.Fatal(err)
	}

	if got := joined(service.Services()); got != "discord,youtube" {
		t.Fatalf("services = %s", got)
	}
	for _, detail := range service.ServiceDetails() {
		if detail.ID == "roblox" {
			t.Fatalf("a removed list is still described: %#v", detail)
		}
	}
	if service.knownService("roblox") || service.hasDefinition("roblox") {
		t.Fatal("a removed list still resolves")
	}
	if _, err := service.ServiceContents(ctx, "roblox"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("contents err = %v", err)
	}
	if got := categoryTitles(service.Categories()); got != "games=discord video=youtube" {
		t.Fatalf("categories = %s", got)
	}
	if got := joined(service.ResolvedServices(listWith(nil, []string{"games"}, nil))); got != "discord" {
		t.Fatalf("resolved = %s", got)
	}
	if _, err := service.CreateList(ctx, "Маршрут", ListComposition{Services: []string{"roblox"}}); err == nil {
		t.Fatal("a composition named a removed list")
	}

	// The shipped catalog is never edited: the subtraction is a record beside
	// it, which is what makes it survive the next catalog load.
	if catalog := service.config.Categories["games"].Services; !reflect.DeepEqual(catalog, []string{"discord", "roblox"}) {
		t.Fatalf("the loaded catalog was rewritten: %#v", catalog)
	}
	want := []CatalogRemoval{{Kind: RemovalList, ID: "roblox", RemovedAt: service.config.Clock.Now().UTC()}}
	if !reflect.DeepEqual(store.removals, want) {
		t.Fatalf("stored removals = %#v", store.removals)
	}
	if err := service.RemoveService(ctx, "roblox"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second deletion err = %v", err)
	}
}

// Deleting a list the operator created deletes the list, not a record of it:
// there is no shipped definition left to subtract, and a record would outlive
// whatever later took the identity. It leaves the categories that held it the
// same way a catalog list does.
func TestDeletingAnOperatorCreatedListLeavesNoRemovalRecord(t *testing.T) {
	service, store := overlayTestService(t)
	ctx := context.Background()
	own, err := service.CreateCustomService(ctx, "Мой список", []string{"a.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateCategory(ctx, "video", CategoryUpdate{Services: &[]string{"youtube", own.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveService(ctx, own.ID); err != nil {
		t.Fatal(err)
	}
	if service.knownService(own.ID) {
		t.Fatal("a deleted list still resolves")
	}
	if got := joined(service.Services()); got != "discord,roblox,youtube" {
		t.Fatalf("services = %s", got)
	}
	if got := categoryTitles(service.Categories()); got != "games=discord+roblox video=youtube" {
		t.Fatalf("categories = %s", got)
	}
	if len(store.removals) != 0 {
		t.Fatalf("an operator-created list recorded a removal: %#v", store.removals)
	}
	if _, stored := store.custom[own.ID]; stored {
		t.Fatal("the row survived its deletion")
	}
	if rows := store.memberships["video"]; len(rows) != 0 {
		t.Fatalf("membership survived the list it named: %#v", rows)
	}
}

// A stored removal survives a restart as the same subtraction, and a row the
// grammar refuses stops the process rather than serving a library nobody can
// explain.
func TestLoadCategoriesRestoresAndValidatesRemovals(t *testing.T) {
	service, store := overlayTestService(t)
	ctx := context.Background()
	if err := service.RemoveService(ctx, "roblox"); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveCategory(ctx, "video", CategoryListsDetach); err != nil {
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
	if restarted.knownService("roblox") {
		t.Fatal("a removed list came back on restart")
	}
	if !restarted.knownService("youtube") {
		t.Fatal("detaching a category took its list with it across a restart")
	}

	now := time.Unix(1, 0).UTC()
	refused := []CatalogRemoval{
		{Kind: "quarantined", ID: "roblox", RemovedAt: now},
		{Kind: RemovalList, ID: "Not A Slug", RemovedAt: now},
		{Kind: RemovalCategory, ID: "custom-1234567890abcdef", RemovedAt: now},
		{Kind: RemovalList, ID: "roblox"},
	}
	for _, removal := range refused {
		fresh, freshStore := overlayTestService(t)
		freshStore.removals = []CatalogRemoval{removal}
		if err := fresh.LoadCategories(ctx); err == nil {
			t.Errorf("%#v: accepted", removal)
		}
	}
}

// Deleting a category asks one question, and the two answers are different
// outcomes for the operator's data: detach leaves the lists in the library
// belonging to no category, delete takes them along.
func TestDeletingACategoryDetachesOrDeletesTheListsItHeld(t *testing.T) {
	detached, _ := overlayTestService(t)
	ctx := context.Background()
	if err := detached.RemoveCategory(ctx, "games", CategoryListsDetach); err != nil {
		t.Fatal(err)
	}
	if got := categoryTitles(detached.Categories()); got != "video=youtube" {
		t.Fatalf("categories = %s", got)
	}
	if got := joined(detached.Services()); got != "discord,roblox,youtube" {
		t.Fatalf("detached services = %s", got)
	}
	for _, detail := range detached.ServiceDetails() {
		if detail.ID == "discord" && len(detail.Categories) != 0 {
			t.Fatalf("a detached list still names a category: %#v", detail.Categories)
		}
	}
	// A detached list is still nameable by a route: it is in the library, it
	// simply belongs to no category.
	if _, err := detached.CreateList(ctx, "Маршрут", ListComposition{Services: []string{"discord"}}); err != nil {
		t.Fatalf("a detached list could not be named: %v", err)
	}

	deleted, store := overlayTestService(t)
	if err := deleted.RemoveCategory(ctx, "games", CategoryListsDelete); err != nil {
		t.Fatal(err)
	}
	if got := categoryTitles(deleted.Categories()); got != "video=youtube" {
		t.Fatalf("categories = %s", got)
	}
	if got := joined(deleted.Services()); got != "youtube" {
		t.Fatalf("deleted services = %s", got)
	}
	// One category deletion, three removal records: the category and the two
	// lists it took with it, all from the same moment.
	if len(store.removals) != 3 {
		t.Fatalf("stored removals = %#v", store.removals)
	}
	for _, removal := range store.removals {
		if removal.RemovedAt != store.removals[0].RemovedAt {
			t.Fatalf("one deletion recorded several moments: %#v", store.removals)
		}
	}
}

// A route that names the deleted object directly refuses the deletion, naming
// itself. Under delete the same refusal covers the lists the category holds:
// they are as much a direct reference as the category is. An archived route
// counts, because restoring one whose content vanished meanwhile is exactly
// the silent change the refusal exists to prevent.
func TestDeletingIsRefusedByARouteNamingTheObjectDirectly(t *testing.T) {
	service, store := overlayTestService(t)
	ctx := context.Background()
	store.lists = []List{
		{ID: strings.Repeat("b", 32), Name: "Дом", Services: []string{"roblox"}},
		{ID: strings.Repeat("a", 32), Name: "Офис", Services: []string{"roblox"}, ArchivedAt: time.Unix(0, 1).UTC()},
		{ID: strings.Repeat("c", 32), Name: "Через категорию", Categories: []string{"games"}},
	}
	naming := []ListReference{
		{ID: strings.Repeat("a", 32), Title: "Офис"},
		{ID: strings.Repeat("b", 32), Title: "Дом"},
	}

	var listInUse ServiceInUseError
	if err := service.RemoveService(ctx, "roblox"); !errors.As(err, &listInUse) {
		t.Fatalf("err = %v", err)
	}
	if listInUse.ServiceID != "roblox" || !reflect.DeepEqual(listInUse.Lists, naming) {
		t.Fatalf("in use = %#v", listInUse)
	}
	if !service.knownService("roblox") {
		t.Fatal("a refused deletion removed the list anyway")
	}

	// Deleting the category with its lists inherits that refusal and adds its
	// own: one shape names every route the deletion would break, whether it
	// referenced the category or one of the lists.
	var categoryInUse CategoryInUseError
	if err := service.RemoveCategory(ctx, "games", CategoryListsDelete); !errors.As(err, &categoryInUse) {
		t.Fatalf("delete err = %v", err)
	}
	wantAll := append(append([]ListReference{}, naming...), ListReference{ID: strings.Repeat("c", 32), Title: "Через категорию"})
	if !reflect.DeepEqual(categoryInUse.Lists, wantAll) {
		t.Fatalf("category in use = %#v", categoryInUse)
	}
	if _, ok := service.mergedCategory("games"); !ok {
		t.Fatal("a refused deletion removed the category anyway")
	}

	// Detaching touches no list, so it inherits none of their references: only
	// the route naming the category itself refuses it. The two dispositions
	// answer to different references, which is why the request states one.
	throughCategory := []ListReference{{ID: strings.Repeat("c", 32), Title: "Через категорию"}}
	if err := service.RemoveCategory(ctx, "games", CategoryListsDetach); !errors.As(err, &categoryInUse) {
		t.Fatalf("detach err = %v", err)
	}
	if !reflect.DeepEqual(categoryInUse.Lists, throughCategory) {
		t.Fatalf("detach in use = %#v", categoryInUse)
	}

	// With that route gone, detaching goes through while deleting still
	// refuses, and the lists stay in the library with their direct references
	// intact: a deletion never rewrote a stored route.
	store.lists = store.lists[:2]
	if err := service.RemoveCategory(ctx, "games", CategoryListsDelete); !errors.As(err, &categoryInUse) {
		t.Fatalf("delete err = %v", err)
	}
	if !reflect.DeepEqual(categoryInUse.Lists, naming) {
		t.Fatalf("delete in use = %#v", categoryInUse)
	}
	if err := service.RemoveCategory(ctx, "games", CategoryListsDetach); err != nil {
		t.Fatalf("detach err = %v", err)
	}
	if !service.knownService("roblox") || !service.knownService("discord") {
		t.Fatal("detaching took the category's lists with it")
	}
	if err := service.RemoveService(ctx, "roblox"); !errors.As(err, &listInUse) {
		t.Fatalf("a direct reference stopped refusing after the category was detached: %v", err)
	}
}

// The point of the subtraction: a route that named a category carries less on
// its next build, and the plan says so. The forecast and the planner read the
// same expansion, so they change together or the artifact would not match what
// the screen promised.
func TestRemovingAListChangesWhatARouteNamingItsCategoryWouldBuild(t *testing.T) {
	store := &publicationFakeStore{}
	service := forecastTestService(t, store, &publicationFakeFiles{})
	ctx := context.Background()
	if _, err := service.UpdateCategory(ctx, "video", CategoryUpdate{Services: &[]string{"youtube", "discord"}}); err != nil {
		t.Fatal(err)
	}
	route := listWith(nil, []string{"video"}, nil)
	store.lists = []List{{ID: strings.Repeat("c", 32), Name: "Через категорию", Categories: []string{"video"}}}
	target, renderer, err := service.target("unbounded")
	if err != nil {
		t.Fatal(err)
	}

	composition := ListComposition{Categories: []string{"video"}}
	before, err := service.ForecastComposition(ctx, composition, []string{"unbounded"})
	if err != nil {
		t.Fatal(err)
	}
	wantBefore := []ServiceRuleForecast{{ServiceID: "discord", Rules: 2}, {ServiceID: "youtube", Rules: 2}}
	if len(before) != 1 || !reflect.DeepEqual(before[0].PerService, wantBefore) {
		t.Fatalf("forecast before = %#v", before)
	}
	planBefore, _, err := service.prepareList(ctx, route, target, renderer)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.RemoveService(ctx, "youtube"); err != nil {
		t.Fatal(err)
	}

	after, err := service.ForecastComposition(ctx, composition, []string{"unbounded"})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || !reflect.DeepEqual(after[0].PerService, []ServiceRuleForecast{{ServiceID: "discord", Rules: 2}}) {
		t.Fatalf("forecast after = %#v", after)
	}
	if after[0].ProjectedRules >= before[0].ProjectedRules {
		t.Fatalf("projection did not shrink: before=%d after=%d", before[0].ProjectedRules, after[0].ProjectedRules)
	}
	planAfter, _, err := service.prepareList(ctx, route, target, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(planAfter.Plan.Services, []string{"discord"}) {
		t.Fatalf("planned services = %#v", planAfter.Plan.Services)
	}
	if planAfter.Plan.SemanticHash == planBefore.Plan.SemanticHash {
		t.Fatal("the semantic hash did not change, so the next build would reuse the previous artifact")
	}
}

// A deletion the store refused changes nothing the process serves. The
// registry is written after the store, never beside it, so a failed write
// cannot leave the two disagreeing about what the library holds.
func TestAStoreThatRefusesADeletionLeavesTheLibraryUntouched(t *testing.T) {
	service, store := overlayTestService(t)
	ctx := context.Background()
	before := categoryTitles(service.Categories())
	store.removalErr = errors.New("storage failed")

	if err := service.RemoveCategory(ctx, "games", CategoryListsDelete); err == nil {
		t.Fatal("a refused write was reported as a deletion")
	}
	if err := service.RemoveService(ctx, "roblox"); err == nil {
		t.Fatal("a refused write was reported as a deletion")
	}
	if after := categoryTitles(service.Categories()); after != before {
		t.Fatalf("categories = %s, want %s", after, before)
	}
	if got := joined(service.Services()); got != "discord,roblox,youtube" {
		t.Fatalf("services = %s", got)
	}
}
