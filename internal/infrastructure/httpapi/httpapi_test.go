package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

type fakeBackend struct {
	artifact   application.ArtifactPayload
	token      string
	blockBuild bool
	started    chan struct{}
	canceled   chan struct{}

	// deployCommands records what the handler passed through, so a test can
	// prove the credential reached the deployer and nothing else.
	deployCommands []application.DeployCommand
	deployResult   application.DeployResult
	deployErr      error

	defaultInterval    application.RefreshInterval
	defaultPriority    []string
	defaultPriorityErr error
	storedCredential   string
	archived           bool
	createdComposition application.ProfileComposition
	// forecastComposition and forecastTargets record what the preview route
	// passed through, so a test can prove the forecast asked about the same
	// composition the request carried rather than a summary of it.
	forecastComposition application.ProfileComposition
	forecastTargets     []string
	customLists         []application.CustomList
	// categories is the merged answer the backend would give. categoryEdits
	// records what each route passed through, so a test can prove the request
	// reached the application unchanged rather than summarized.
	categories      []application.CategoryDetail
	categoryEdits   []application.CategoryUpdate
	categoryRemoved []string
	// listsRemoved records the lists deleted from the library.
	listsRemoved []string
	// categoryInUse and listInUse name the profiles a deletion is refused
	// with. They are separate because the two refusals carry different words.
	categoryInUse   []application.ProfileReference
	listInUse       []application.ProfileReference
	sourceToggles   []string
	domainVerdicts  []string
	transferPayload []byte
	transferDigest  string
	transferErr     error
}

func (f *fakeBackend) ExportConfigTransfer(context.Context) ([]byte, error) {
	return []byte(`{"version":"config-transfer-v1.0"}`), nil
}
func (f *fakeBackend) PreviewConfigTransfer(payload []byte) (application.ConfigTransferPreview, error) {
	f.transferPayload = append([]byte(nil), payload...)
	if f.transferErr != nil {
		return application.ConfigTransferPreview{}, f.transferErr
	}
	return application.ConfigTransferPreview{Digest: "sha256:" + strings.Repeat("0", 64), CanApply: true}, nil
}
func (f *fakeBackend) ApplyConfigTransfer(_ context.Context, digest string, payload []byte) (application.ConfigTransferCounts, error) {
	f.transferDigest, f.transferPayload = digest, append([]byte(nil), payload...)
	if f.transferErr != nil {
		return application.ConfigTransferCounts{}, f.transferErr
	}
	return application.ConfigTransferCounts{}, nil
}

func (f *fakeBackend) Lists() []string { return []string{"example"} }
func (f *fakeBackend) DefaultPriority(context.Context) ([]string, error) {
	if f.defaultPriority == nil {
		return []string{"example"}, nil
	}
	return append([]string(nil), f.defaultPriority...), nil
}
func (f *fakeBackend) SetDefaultPriority(_ context.Context, priority []string) error {
	if f.defaultPriorityErr != nil {
		return f.defaultPriorityErr
	}
	f.defaultPriority = append([]string(nil), priority...)
	return nil
}
func (f *fakeBackend) ListDetails() []application.ListDetail {
	return []application.ListDetail{{
		ID: "example", Title: "Example", Categories: []string{"diagnostic"},
		Domains: []application.ListDomain{{Value: "example.com", IncludeSubdomains: true}},
		Sources: []application.ListSource{{ID: "dns", Type: "dns"}, {ID: "vendor", Type: "http"}}, SourceCount: 2,
	}}
}
func (f *fakeBackend) PreviewList(context.Context, string) (application.ListPreview, error) {
	return application.ListPreview{
		ListID: "example",
		Sources: []application.ListSourcePreview{
			{ID: "dns", Type: "dns", Status: "ready", Domains: []string{}, AddressCount: 2},
			{ID: "vendor", Type: "http", Status: "ready", Domains: []string{"api.example.com"}, DomainCount: 1},
		},
		Domains: []string{"api.example.com"}, DomainCount: 1, AddressCount: 2,
	}, nil
}
func (f *fakeBackend) CreateCustomList(_ context.Context, title string, domains []string) (application.CustomList, error) {
	list := application.CustomList{ID: "custom-1234567890abcdef", Title: title, Domains: domains}
	f.customLists = append(f.customLists, list)
	return list, nil
}
func (f *fakeBackend) UpdateCustomList(_ context.Context, id, title string, domains []string) (application.CustomList, error) {
	for index, list := range f.customLists {
		if list.ID == id {
			f.customLists[index] = application.CustomList{ID: id, Title: title, Domains: domains}
			return f.customLists[index], nil
		}
	}
	return application.CustomList{}, application.ErrNotFound
}
func (f *fakeBackend) ListContents(_ context.Context, listID string) (application.ListContents, error) {
	return application.ListContents{
		ListID: listID,
		Rows: []application.ListContentsRow{
			{Value: "example.com", Kind: "domain", Origin: "catalog", Enabled: true},
			{Value: "198.51.100.7", Kind: "ip", Origin: "manual", Enabled: true},
		},
		Sources: []application.ListContentsSource{
			{ID: "dns", Type: "dns", Enabled: true},
		},
		Observed: true,
	}, nil
}
func (f *fakeBackend) RefreshListByID(_ context.Context, listID string) (application.RefreshSummary, error) {
	return application.RefreshSummary{ListID: listID, SourceRuns: 1, SuccessfulRuns: 1}, nil
}
func (f *fakeBackend) SetListSourceEnabled(_ context.Context, listID, sourceID string, enabled bool) error {
	f.sourceToggles = append(f.sourceToggles, listID+"/"+sourceID+"="+strconv.FormatBool(enabled))
	return nil
}
func (f *fakeBackend) AddListSource(_ context.Context, listID, url string, format domain.FeedFormat) (application.CustomSource, error) {
	return application.CustomSource{ID: "feed-1234567890abcdef", ListID: listID, URL: url, Format: format}, nil
}
func (f *fakeBackend) RemoveListSource(_ context.Context, listID, sourceID string) error {
	f.sourceToggles = append(f.sourceToggles, listID+"/"+sourceID+"=removed")
	return nil
}
func (f *fakeBackend) SetListValues(_ context.Context, listID string, values []string, verdict application.DomainVerdict) error {
	for _, value := range values {
		if value == "not a destination" {
			return application.InvalidDestinationError{Value: value}
		}
	}
	f.domainVerdicts = append(f.domainVerdicts, listID+"/"+strings.Join(values, ",")+"="+string(verdict))
	return nil
}
func (f *fakeBackend) Categories() []application.CategoryDetail {
	if len(f.categories) > 0 {
		return f.categories
	}
	return []application.CategoryDetail{{ID: "diagnostic", Title: "Diagnostic", Lists: []string{"example"}}}
}

func (f *fakeBackend) CreateCategory(_ context.Context, title string, lists []string) (application.CategoryDetail, error) {
	if strings.TrimSpace(title) == "" {
		return application.CategoryDetail{}, errors.New("invalid category title")
	}
	category := application.CategoryDetail{ID: "custom-1234567890abcdef", Title: title, Lists: lists, Custom: true}
	if category.Lists == nil {
		category.Lists = []string{}
	}
	f.categories = append(f.categories, category)
	return category, nil
}

func (f *fakeBackend) UpdateCategory(_ context.Context, id string, update application.CategoryUpdate) (application.CategoryDetail, error) {
	f.categoryEdits = append(f.categoryEdits, update)
	switch id {
	case "diagnostic":
		if update.Title != nil {
			return application.CategoryDetail{}, application.ErrCatalogCategory
		}
		lists := []string{"example"}
		if update.Lists != nil {
			lists = *update.Lists
		}
		return application.CategoryDetail{ID: id, Title: "Diagnostic", Lists: lists}, nil
	case "custom-1234567890abcdef":
		category := application.CategoryDetail{ID: id, Title: "Мои списки", Lists: []string{}, Custom: true}
		if update.Title != nil {
			category.Title = *update.Title
		}
		if update.Lists != nil {
			category.Lists = *update.Lists
		}
		return category, nil
	default:
		return application.CategoryDetail{}, application.ErrNotFound
	}
}

func (f *fakeBackend) RemoveCategory(_ context.Context, id string, profiles application.CategoryListDisposition) error {
	switch {
	case profiles != application.CategoryListsDetach && profiles != application.CategoryListsDelete:
		return errors.New("invalid category list disposition")
	case id != "diagnostic" && id != "custom-1234567890abcdef":
		return application.ErrNotFound
	case len(f.categoryInUse) > 0:
		return application.CategoryInUseError{CategoryID: id, Profiles: f.categoryInUse}
	}
	f.categoryRemoved = append(f.categoryRemoved, id+":"+string(profiles))
	return nil
}

func (f *fakeBackend) RemoveList(_ context.Context, id string) error {
	switch {
	case id != "example" && id != "custom-1234567890abcdef":
		return application.ErrNotFound
	case len(f.listInUse) > 0:
		return application.ListInUseError{ListID: id, Profiles: f.listInUse}
	}
	f.listsRemoved = append(f.listsRemoved, id)
	return nil
}
func (f *fakeBackend) ResolvedLists(application.Profile) []string { return []string{"example"} }
func (f *fakeBackend) DefaultRefreshInterval(context.Context) (application.RefreshInterval, error) {
	return application.RefreshOff, nil
}
func (f *fakeBackend) SetDefaultRefreshInterval(_ context.Context, interval application.RefreshInterval) error {
	f.defaultInterval = interval
	return nil
}
func (f *fakeBackend) SetProfileRefreshInterval(_ context.Context, _ string, interval application.RefreshInterval) (application.Profile, error) {
	return application.Profile{ID: strings.Repeat("a", 32), RefreshInterval: interval}, nil
}
func (f *fakeBackend) ProfileSchedule(_ context.Context, profile application.Profile) (application.Schedule, error) {
	return application.ScheduleOf(profile, application.RefreshOff), nil
}

func (f *fakeBackend) MissingCategories(application.Profile) []string { return []string{} }
func (f *fakeBackend) Targets() []application.TargetOption {
	return []application.TargetOption{
		{
			ID: "keenetic", Title: "Keenetic", FormatKey: "keenetic-bat-ipv4-v1", RendererID: "keenetic-route-bat",
			FileExtension: "bat", ManualInstallationHint: "Import the file.",
		},
		{
			ID: "singbox", Title: "sing-box", FormatKey: "singbox-source-json-v1", RendererID: "singbox-ruleset-json",
			FileExtension: "json", ManualInstallationHint: "Reference the file.",
		},
	}
}
func (f *fakeBackend) ExportFormats() []application.ExportFormat {
	return []application.ExportFormat{{
		ID: "keenetic", RendererID: "keenetic-route-bat",
		FileExtension: "bat", ContentType: "application/x-bat",
	}}
}
func (f *fakeBackend) Export(_ context.Context, _ string, formatID string) (application.ExportPayload, error) {
	if formatID != "keenetic" {
		return application.ExportPayload{}, application.ErrNotFound
	}
	descriptor := domain.RendererDescriptor{
		ID: "keenetic-route-bat", Version: "keenetic-bat-ipv4-v1",
		FileExtension: "bat", ContentType: "application/x-bat",
	}
	return application.ExportPayload{
		Format: application.ExportFormat{
			ID: formatID, RendererID: descriptor.ID,
			FileExtension: descriptor.FileExtension, ContentType: descriptor.ContentType,
		},
		Descriptor: descriptor,
		Payload:    []byte("route ADD 198.51.100.8 MASK 255.255.255.255 0.0.0.0\r\n"),
	}, nil
}
func (f *fakeBackend) CreateProfile(_ context.Context, _ string, composition application.ProfileComposition) (application.Profile, error) {
	f.createdComposition = composition
	return application.Profile{ID: strings.Repeat("a", 32)}, nil
}

// ForecastComposition answers the way the application does: an unknown target
// is refused with a plain error rather than answered with silence, and a known
// one comes back with the numbers a screen refuses on.
func (f *fakeBackend) ForecastComposition(_ context.Context, composition application.ProfileComposition, targets []string) ([]application.CompositionForecast, error) {
	f.forecastComposition, f.forecastTargets = composition, targets
	known := map[string]application.CompositionForecast{
		"keenetic": {
			TargetID: "keenetic", MaximumRules: 1024, ProjectedRules: 1742, Fits: false,
			PerList: []application.ListRuleForecast{{ListID: "twitch", Rules: 1230}, {ListID: "youtube", Rules: 512}},
		},
		"singbox": {
			TargetID: "singbox", MaximumRules: 8192, ProjectedRules: 1742, Fits: true,
			PerList: []application.ListRuleForecast{{ListID: "twitch", Rules: 1230}, {ListID: "youtube", Rules: 512}},
		},
	}
	forecasts := make([]application.CompositionForecast, 0, len(targets))
	for _, id := range targets {
		forecast, ok := known[id]
		if !ok {
			return nil, errors.New("invalid forecast target")
		}
		forecast.Overlaps = application.CompositionOverlaps{Items: []application.CompositionOverlap{}}
		forecasts = append(forecasts, forecast)
	}
	return forecasts, nil
}
func (f *fakeBackend) Profile(context.Context, string) (application.Profile, error) {
	return application.Profile{ID: strings.Repeat("a", 32)}, nil
}
func (f *fakeBackend) UpdateProfile(context.Context, string, string, application.ProfileComposition) (application.Profile, error) {
	if f.archived {
		return application.Profile{}, application.ErrProfileArchived
	}
	return application.Profile{ID: strings.Repeat("a", 32)}, nil
}

// archived records the state the last archive/restore call asked for, so a
// test can prove the route reached the right verb rather than a shared one.
func (f *fakeBackend) ArchiveProfile(context.Context, string) (application.Profile, error) {
	f.archived = true
	return application.Profile{ID: strings.Repeat("a", 32), ArchivedAt: time.Unix(0, 1).UTC()}, nil
}
func (f *fakeBackend) RestoreProfile(context.Context, string) (application.Profile, error) {
	f.archived = false
	return application.Profile{ID: strings.Repeat("a", 32)}, nil
}
func (f *fakeBackend) ProfileCards(context.Context) ([]application.ProfileCard, error) {
	return []application.ProfileCard{{
		ID: strings.Repeat("a", 32), Name: "Example list", Lists: []string{"example"},
		Outputs: []application.OutputCard{{
			ID: strings.Repeat("a", 32), TargetID: "keenetic", TargetTitle: "Keenetic",
			TargetKind: "router", FileExtension: "bat",
			Latest: &application.OutputArtifact{
				ID: strings.Repeat("b", 32), SnapshotID: strings.Repeat("c", 32),
				SizeBytes: 42, ContentType: "application/octet-stream",
			},
		}},
	}}, nil
}
func (f *fakeBackend) AddOutput(context.Context, string, string) (application.CreatedOutput, error) {
	return application.CreatedOutput{Output: application.Output{ID: strings.Repeat("a", 32)}}, nil
}

func (f *fakeBackend) SetOutputDevice(_ context.Context, outputID, deviceID string) (application.Output, error) {
	return application.Output{ID: outputID, DeviceID: deviceID}, nil
}
func (f *fakeBackend) IssueSubscription(context.Context, string) (string, error) { return f.token, nil }
func (f *fakeBackend) Output(context.Context, string) (application.Output, error) {
	return application.Output{ID: strings.Repeat("a", 32)}, nil
}
func (f *fakeBackend) OutputCards(context.Context, string) ([]application.OutputCard, error) {
	return []application.OutputCard{{
		ID: strings.Repeat("a", 32), TargetID: "keenetic", TargetTitle: "Keenetic",
		TargetKind: "router", FileExtension: "bat",
		Latest: &application.OutputArtifact{
			ID: strings.Repeat("b", 32), SnapshotID: strings.Repeat("c", 32),
			SizeBytes: 42, ContentType: "application/octet-stream",
		},
	}}, nil
}
func (f *fakeBackend) Refresh(context.Context, string) ([]application.RefreshSummary, error) {
	return []application.RefreshSummary{}, nil
}
func (f *fakeBackend) Build(ctx context.Context, _ string) (application.PublishedBuild, error) {
	if f.blockBuild {
		f.started <- struct{}{}
		<-ctx.Done()
		f.canceled <- struct{}{}
		return application.PublishedBuild{}, ctx.Err()
	}
	return testPublishedBuild(), nil
}
func (f *fakeBackend) Subscription(context.Context, string) (application.ArtifactPayload, error) {
	return f.artifact, nil
}
func (f *fakeBackend) Snapshot(context.Context, string) (application.PlanSnapshotRecord, error) {
	return application.PlanSnapshotRecord{ID: strings.Repeat("a", 32), RoutingPlanJSON: []byte(`{"interface_version":"m0-spike-v1"}`)}, nil
}
func (f *fakeBackend) Artifact(context.Context, string) (application.ArtifactPayload, error) {
	return f.artifact, nil
}

func (f *fakeBackend) SecretStoreAvailable() bool { return true }
func (f *fakeBackend) DeviceCards(context.Context) ([]application.DeviceCard, error) {
	return []application.DeviceCard{{
		Device: application.Device{
			ID: strings.Repeat("d", 32), TargetID: "keenetic", Name: "Роутер",
			Address: "http://192.168.1.1", Account: "admin",
		},
		TargetTitle: "Keenetic", Deployable: true,
	}}, nil
}
func (f *fakeBackend) RegisterDevice(_ context.Context, targetID, name, address, account, interfaceName string) (application.Device, error) {
	return application.Device{ID: strings.Repeat("d", 32), TargetID: targetID, Name: name, Address: address, Account: account, Interface: interfaceName}, nil
}
func (f *fakeBackend) UpdateDevice(_ context.Context, id, name, address, account, interfaceName string) (application.Device, error) {
	return application.Device{ID: id, TargetID: "keenetic", Name: name, Address: address, Account: account, Interface: interfaceName}, nil
}
func (f *fakeBackend) ForgetDevice(context.Context, string) error { return nil }
func (f *fakeBackend) EnableAutoDelivery(_ context.Context, id, secret string) (application.Device, error) {
	f.storedCredential = secret
	return application.Device{ID: id, TargetID: "keenetic", Name: "Роутер", Address: "http://192.168.1.1", AutoDeliver: true}, nil
}
func (f *fakeBackend) DisableAutoDelivery(_ context.Context, id string) (application.Device, error) {
	f.storedCredential = ""
	return application.Device{ID: id, TargetID: "keenetic", Name: "Роутер", Address: "http://192.168.1.1"}, nil
}

func (f *fakeBackend) DeployableTargets() []application.DeployableTarget {
	return []application.DeployableTarget{{
		TargetID: "keenetic", Title: "Keenetic", DeployerID: "keenetic-route-bat",
		Requirements: application.ConnectionRequirements{
			AddressLabel: "Адрес устройства", AddressExample: "http://192.168.1.1",
			NeedsCredential: true, NeedsInterface: true, InterfaceLabel: "Интерфейс",
		},
	}}
}

func (f *fakeBackend) DeployPlan(_ context.Context, command application.DeployCommand) (application.DeployPlan, error) {
	f.deployCommands = append(f.deployCommands, command)
	if f.deployErr != nil {
		return application.DeployPlan{}, f.deployErr
	}
	return application.DeployPlan{ArtifactID: command.ArtifactID, TargetID: "keenetic", Title: "Keenetic", DeployerID: "keenetic-route-bat"}, nil
}

func (f *fakeBackend) Deploy(_ context.Context, command application.DeployCommand) (application.DeployResult, error) {
	f.deployCommands = append(f.deployCommands, command)
	if f.deployErr != nil {
		return f.deployResult, f.deployErr
	}
	return f.deployResult, nil
}

func TestLoopbackAuthorityAndMutationGuards(t *testing.T) {
	backend := testBackend()
	var logs bytes.Buffer
	server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, method, path, host, remote, contentType, csrf, origin string
		body                                                        io.Reader
		want                                                        int
	}{
		{"health", http.MethodGet, "/health", "127.0.0.1:8765", "127.0.0.1:1", "", "", "", nil, 200},
		{"host", http.MethodGet, "/health", "evil.test", "127.0.0.1:1", "", "", "", nil, 421},
		{"remote", http.MethodGet, "/health", "127.0.0.1:8765", "192.0.2.1:1", "", "", "", nil, 421},
		{"csrf", http.MethodPost, "/v1/profiles", "127.0.0.1:8765", "127.0.0.1:1", "application/json", "", "", strings.NewReader(`{"name":"list","lists":["example"]}`), 403},
		{"origin", http.MethodPost, "/v1/profiles", "127.0.0.1:8765", "127.0.0.1:1", "application/json", "1", "http://evil.test", strings.NewReader(`{"name":"list","lists":["example"]}`), 403},
		{"type", http.MethodPost, "/v1/profiles", "127.0.0.1:8765", "127.0.0.1:1", "application/json; charset=utf-8", "1", "", strings.NewReader(`{}`), 403},
		{"unknown", http.MethodPost, "/v1/profiles", "127.0.0.1:8765", "127.0.0.1:1", "application/json", "1", "", strings.NewReader(`{"name":"list","lists":["example"],"extra":true}`), 400},
		{"trailing", http.MethodPost, "/v1/profiles", "127.0.0.1:8765", "127.0.0.1:1", "application/json", "1", "", strings.NewReader(`{} {}`), 400},
		{"empty body must be object", http.MethodPost, "/v1/outputs/" + strings.Repeat("a", 32) + "/build", "127.0.0.1:8765", "127.0.0.1:1", "application/json", "1", "", strings.NewReader(`null`), 400},
		{"oversize", http.MethodPost, "/v1/profiles", "127.0.0.1:8765", "127.0.0.1:1", "application/json", "1", "", strings.NewReader(`{"name":"` + strings.Repeat("x", maxJSONBytes) + `"}`), 413},
		{"options", http.MethodOptions, "/health", "127.0.0.1:8765", "127.0.0.1:1", "", "", "", nil, 405},
		{"encoded slash", http.MethodGet, "/v1/artifacts/a%2Fb", "127.0.0.1:8765", "127.0.0.1:1", "", "", "", nil, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, "http://"+tt.host+tt.path, tt.body)
			request.Host = tt.host
			request.RemoteAddr = tt.remote
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			if tt.csrf != "" {
				request.Header.Set("X-Routevane-Request", tt.csrf)
			}
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != tt.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("CORS header emitted")
			}
		})
	}
}

func TestRequestDeadlineCancelsBackendAndReleasesConcurrencySlots(t *testing.T) {
	backend := testBackend()
	backend.blockBuild = true
	backend.started = make(chan struct{}, 32)
	backend.canceled = make(chan struct{}, 32)
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.handler.(*handler).requestTimeout = 200 * time.Millisecond
	var wg sync.WaitGroup
	wg.Add(32)
	for range 32 {
		go func() {
			defer wg.Done()
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/v1/outputs/"+strings.Repeat("a", 32)+"/build", strings.NewReader(`{}`))
			request.Host = "127.0.0.1:8765"
			request.RemoteAddr = "127.0.0.1:1"
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Routevane-Request", "1")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity {
				t.Errorf("blocked response status=%d", response.Code)
			}
		}()
	}
	for range 32 {
		select {
		case <-backend.started:
		case <-time.After(time.Second):
			t.Fatal("backend did not fill concurrency slots")
		}
	}
	overflow := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/health", nil)
	overflow.Host = "127.0.0.1:8765"
	overflow.RemoteAddr = "127.0.0.1:1"
	overflowResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(overflowResponse, overflow)
	if overflowResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("concurrency guard status=%d", overflowResponse.Code)
	}
	for range 32 {
		select {
		case <-backend.canceled:
		case <-time.After(time.Second):
			t.Fatal("backend did not observe request cancellation")
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed-out handlers leaked")
	}
	after := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/health", nil)
	after.Host = "127.0.0.1:8765"
	after.RemoteAddr = "127.0.0.1:1"
	afterResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(afterResponse, after)
	if afterResponse.Code != http.StatusOK {
		t.Fatalf("slot not released, status=%d", afterResponse.Code)
	}
}

func TestRequestDeadlinePreservesShorterCallerDeadline(t *testing.T) {
	backend := testBackend()
	backend.blockBuild = true
	backend.started = make(chan struct{}, 1)
	backend.canceled = make(chan struct{}, 1)
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.handler.(*handler).requestTimeout = time.Second
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/v1/outputs/"+strings.Repeat("a", 32)+"/build", strings.NewReader(`{}`))
	request.Host = "127.0.0.1:8765"
	request.RemoteAddr = "127.0.0.1:1"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	ctx, cancel := context.WithTimeout(request.Context(), 20*time.Millisecond)
	defer cancel()
	request = request.WithContext(ctx)
	started := time.Now()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("caller deadline was widened: %s", elapsed)
	}
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d", response.Code)
	}
	select {
	case <-backend.canceled:
	case <-time.After(time.Second):
		t.Fatal("backend did not observe caller cancellation")
	}
}

func TestSubscriptionConditionalResponsesAndSecretFreeLogs(t *testing.T) {
	backend := testBackend()
	var logs bytes.Buffer
	server, _ := New("http://127.0.0.1:8765", backend, slog.New(slog.NewJSONHandler(&logs, nil)))
	token := backend.token
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/subscriptions/"+token, nil)
	request.Host = "127.0.0.1:8765"
	request.RemoteAddr = "127.0.0.1:1"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != 200 || response.Body.String() != string(backend.artifact.Payload) {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
	etag := response.Header().Get("ETag")
	last := response.Header().Get("Last-Modified")
	if etag != `"`+backend.artifact.Artifact.ArtifactHash+`"` || last == "" {
		t.Fatalf("etag=%q last=%q", etag, last)
	}
	if response.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("artifact cache control=%q", response.Header().Get("Cache-Control"))
	}
	for _, headers := range []map[string]string{{"If-None-Match": "W/" + etag}, {"If-None-Match": "\"other\"", "If-Modified-Since": last}, {"If-Modified-Since": last}} {
		request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/subscriptions/"+token, nil)
		request.Host = "127.0.0.1:8765"
		request.RemoteAddr = "127.0.0.1:1"
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response = httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		want := http.StatusNotModified
		if headers["If-None-Match"] == "\"other\"" {
			want = http.StatusOK
		}
		if response.Code != want || want == 304 && response.Body.Len() != 0 {
			t.Fatalf("headers=%v status=%d body=%q", headers, response.Code, response.Body.String())
		}
	}
	if strings.Contains(logs.String(), token) {
		t.Fatalf("subscription secret leaked to logs: %s", logs.String())
	}
}

func TestListsAndBuildUseBoundedSafeDTOs(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), nil)
	if err != nil {
		t.Fatal(err)
	}
	lists := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/lists", nil)
	lists.Host = "127.0.0.1:8765"
	lists.RemoteAddr = "127.0.0.1:1"
	listsResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(listsResponse, lists)
	if listsResponse.Code != http.StatusOK || !strings.Contains(listsResponse.Body.String(), `"lists":["example"]`) || !strings.Contains(listsResponse.Body.String(), `"list_details":[{"id":"example","title":"Example","categories":["diagnostic"],"domains":[{"value":"example.com","include_subdomains":true}],"sources":[{"id":"dns","type":"dns"},{"id":"vendor","type":"http"}],"source_count":2}]`) {
		t.Fatalf("services response=%d %s", listsResponse.Code, listsResponse.Body.String())
	}
	preview := mutationRequest(t, "/v1/lists/example/preview", `{}`)
	previewResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(previewResponse, preview)
	previewBody := previewResponse.Body.String()
	if previewResponse.Code != http.StatusOK || !strings.Contains(previewBody, `"domains":["api.example.com"]`) || !strings.Contains(previewBody, `"address_count":2`) || strings.Contains(previewBody, "https://") {
		t.Fatalf("preview response=%d %s", previewResponse.Code, previewBody)
	}

	build := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/v1/outputs/"+strings.Repeat("a", 32)+"/build", strings.NewReader(`{}`))
	build.Host = "127.0.0.1:8765"
	build.RemoteAddr = "127.0.0.1:1"
	build.Header.Set("Content-Type", "application/json")
	build.Header.Set("X-Routevane-Request", "1")
	buildResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(buildResponse, build)
	body := buildResponse.Body.String()
	if buildResponse.Code != http.StatusOK || strings.Contains(body, `"routing_plan":`) || strings.Contains(body, "RAW-PLAN-CANARY") || !strings.Contains(body, `"partial_coverage":true`) || !strings.Contains(body, `"content_created_at"`) || !strings.Contains(body, `"validation_status":"valid"`) {
		t.Fatalf("build response=%d %s", buildResponse.Code, body)
	}
}

func TestDefaultPriorityHTTPContractIsReadWrittenAndGuarded(t *testing.T) {
	backend := testBackend()
	backend.defaultPriority = []string{"example"}
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}

	read := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/lists", nil)
	read.RemoteAddr = "127.0.0.1:1"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, read)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"default_priority":["example"]`) {
		t.Fatalf("GET services code=%d body=%s", response.Code, response.Body.String())
	}

	write := mutationRequest(t, "/v1/lists/priority", `{"default_priority":["example"]}`)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, write)
	if response.Code != http.StatusOK || response.Body.String() != `{"default_priority":["example"]}`+"\n" {
		t.Fatalf("POST priority code=%d body=%s", response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(backend.defaultPriority, []string{"example"}) {
		t.Fatalf("stored priority=%#v", backend.defaultPriority)
	}

	backend.defaultPriorityErr = errors.New("invalid default priority")
	invalid := mutationRequest(t, "/v1/lists/priority", `{"default_priority":[]}`)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, invalid)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid priority code=%d body=%s", response.Code, response.Body.String())
	}

	unguarded := mutationRequest(t, "/v1/lists/priority", `{"default_priority":["example"]}`)
	unguarded.Header.Del("X-Routevane-Request")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, unguarded)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unguarded priority code=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOneOffExportListsFormatsAndDoesNotUseAnOutputArtifact(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), nil)
	if err != nil {
		t.Fatal(err)
	}

	formats := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/export-formats", nil)
	formats.Host = "127.0.0.1:8765"
	formats.RemoteAddr = "127.0.0.1:1"
	formatsResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(formatsResponse, formats)
	if formatsResponse.Code != http.StatusOK || !strings.Contains(formatsResponse.Body.String(), `"renderer_id":"keenetic-route-bat"`) {
		t.Fatalf("formats response=%d %s", formatsResponse.Code, formatsResponse.Body.String())
	}

	profileID := strings.Repeat("a", 32)
	export := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/v1/profiles/"+profileID+"/export", strings.NewReader(`{"format_id":"keenetic"}`))
	export.Host = "127.0.0.1:8765"
	export.RemoteAddr = "127.0.0.1:1"
	export.Header.Set("Content-Type", "application/json")
	export.Header.Set("X-Routevane-Request", "1")
	exportResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(exportResponse, export)
	if exportResponse.Code != http.StatusOK || !strings.Contains(exportResponse.Body.String(), "route ADD") {
		t.Fatalf("export response=%d %q", exportResponse.Code, exportResponse.Body.String())
	}
	if got := exportResponse.Header().Get("Content-Disposition"); got != `attachment; filename="routevane-`+profileID+`-keenetic.bat"` {
		t.Fatalf("content disposition=%q", got)
	}
	if exportResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", exportResponse.Header().Get("Cache-Control"))
	}
}

func TestStaticUIHandlesSPACacheMIMEETagAndAuthority(t *testing.T) {
	assets := loadStaticAssets(fstest.MapFS{
		"index.html":             &fstest.MapFile{Data: []byte("<!doctype html><main>Routevane UI</main>")},
		"_nuxt/app.123456.js":    &fstest.MapFile{Data: []byte("export const ready = true\n")},
		"_routevane/boot.123.js": &fstest.MapFile{Data: []byte("window.__NUXT__ = {}\n")},
	})
	if !assets.available || !strings.HasPrefix(assets.digest, "sha256-") {
		t.Fatalf("assets=%#v", assets)
	}
	server, err := newServer("http://127.0.0.1:8765", testBackend(), nil, assets)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/setup/diagnostics", nil)
	request.Host = "127.0.0.1:8765"
	request.RemoteAddr = "127.0.0.1:1"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	const wantCSP = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'"
	if response.Code != http.StatusOK || response.Body.String() != "<!doctype html><main>Routevane UI</main>" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Security-Policy") != wantCSP || response.Header().Get("X-Routevane-UI-Digest") != assets.digest {
		t.Fatalf("SPA response=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}

	asset := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/_nuxt/app.123456.js", nil)
	asset.Host = "127.0.0.1:8765"
	asset.RemoteAddr = "127.0.0.1:1"
	assetResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(assetResponse, asset)
	if assetResponse.Code != http.StatusOK || assetResponse.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !strings.Contains(assetResponse.Header().Get("Content-Type"), "javascript") || assetResponse.Header().Get("ETag") == "" {
		t.Fatalf("asset response=%d headers=%v", assetResponse.Code, assetResponse.Header())
	}

	conditional := httptest.NewRequest(http.MethodHead, "http://127.0.0.1:8765/_nuxt/app.123456.js", nil)
	conditional.Host = "127.0.0.1:8765"
	conditional.RemoteAddr = "127.0.0.1:1"
	conditional.Header.Set("If-None-Match", assetResponse.Header().Get("ETag"))
	conditionalResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(conditionalResponse, conditional)
	if conditionalResponse.Code != http.StatusNotModified || conditionalResponse.Body.Len() != 0 {
		t.Fatalf("conditional response=%d body=%q", conditionalResponse.Code, conditionalResponse.Body.String())
	}

	for _, path := range []string{"/_nuxt/missing.js", "/v1/not-a-route", "/routevane-ui.marker"} {
		unknown := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
		unknown.Host = "127.0.0.1:8765"
		unknown.RemoteAddr = "127.0.0.1:1"
		unknownResponse := httptest.NewRecorder()
		server.Handler().ServeHTTP(unknownResponse, unknown)
		if unknownResponse.Code != http.StatusNotFound {
			t.Fatalf("unknown path=%q status=%d", path, unknownResponse.Code)
		}
	}

	wrongHost := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/", nil)
	wrongHost.Host = "invalid.test"
	wrongHost.RemoteAddr = "127.0.0.1:1"
	wrongHostResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(wrongHostResponse, wrongHost)
	if wrongHostResponse.Code != http.StatusMisdirectedRequest {
		t.Fatalf("wrong host status=%d", wrongHostResponse.Code)
	}
}

// A revalidated chunk must come back still cacheable. The 304 carries no body
// to re-describe, so if it dropped the caching directive the browser would keep
// asking for a file whose name already proves it cannot have changed.
func TestStaticRevalidationKeepsTheCachingContract(t *testing.T) {
	const chunk = "export const ready = true\n"
	assets := loadStaticAssets(fstest.MapFS{
		"index.html":          &fstest.MapFile{Data: []byte("<!doctype html><main>Routevane UI</main>")},
		"_nuxt/app.123456.js": &fstest.MapFile{Data: []byte(chunk)},
	})
	server, err := newServer("http://127.0.0.1:8765", testBackend(), nil, assets)
	if err != nil {
		t.Fatal(err)
	}
	get := func(method, path string, headers map[string]string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://127.0.0.1:8765"+path, nil)
		request.Host = "127.0.0.1:8765"
		request.RemoteAddr = "127.0.0.1:1"
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}

	full := get(http.MethodGet, "/_nuxt/app.123456.js", nil)
	if full.Code != http.StatusOK || full.Body.String() != chunk {
		t.Fatalf("chunk status=%d body=%q", full.Code, full.Body.String())
	}
	etag := full.Header().Get("ETag")

	revalidated := get(http.MethodGet, "/_nuxt/app.123456.js", map[string]string{"If-None-Match": etag})
	if revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0 {
		t.Fatalf("revalidated status=%d body=%q", revalidated.Code, revalidated.Body.String())
	}
	if got := revalidated.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("revalidated cache control=%q", got)
	}
	if got := revalidated.Header().Get("Content-Security-Policy"); got != uiContentSecurityPolicy {
		t.Fatalf("revalidated CSP=%q", got)
	}
	if got := revalidated.Header().Get("ETag"); got != etag {
		t.Fatalf("revalidated etag=%q, want %q", got, etag)
	}

	// The document is revalidated by the same validator but must never be
	// stored, or an operator would keep loading the page a past build shipped.
	document := get(http.MethodGet, "/setup/diagnostics", nil)
	stale := get(http.MethodGet, "/setup/diagnostics", map[string]string{"If-None-Match": document.Header().Get("ETag")})
	if stale.Code != http.StatusNotModified || stale.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("document revalidation status=%d cache control=%q", stale.Code, stale.Header().Get("Cache-Control"))
	}

	head := get(http.MethodHead, "/_nuxt/app.123456.js", nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d body=%q", head.Code, head.Body.String())
	}
	if got := head.Header().Get("Content-Length"); got != strconv.Itoa(len(chunk)) {
		t.Fatalf("HEAD content length=%q", got)
	}

	partial := get(http.MethodGet, "/_nuxt/app.123456.js", map[string]string{"Range": "bytes=0-5"})
	if partial.Code != http.StatusPartialContent || partial.Body.String() != chunk[:6] {
		t.Fatalf("range status=%d body=%q", partial.Code, partial.Body.String())
	}
}

func testPublishedBuild() application.PublishedBuild {
	createdAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	return application.PublishedBuild{
		Output:   application.Output{ID: strings.Repeat("a", 32), ProfileID: strings.Repeat("a", 32), TargetID: "keenetic"},
		Snapshot: application.PlanSnapshotRecord{ID: strings.Repeat("b", 32), OutputID: strings.Repeat("a", 32), RoutingPlanHash: strings.Repeat("c", 64), RoutingPlanJSON: []byte(`{"canary":"RAW-PLAN-CANARY"}`), PolicyVersion: "m1", CatalogRevision: strings.Repeat("d", 64), ObservationCutoff: createdAt, CreatedAt: createdAt, Status: "valid"},
		Artifact: application.ArtifactBuildRecord{ID: strings.Repeat("e", 32), OutputID: strings.Repeat("a", 32), PlanSnapshotID: strings.Repeat("b", 32), RendererID: "keenetic-route-bat", RendererVersion: "keenetic-bat-ipv4-v1", ArtifactHash: strings.Repeat("f", 64), SizeBytes: 42, ContentType: "application/x-bat", ContentCreatedAt: createdAt, ValidationStatus: "valid", Status: "published"},
		Summary:  application.PublishedBuildSummary{RuleCount: 2, PartialCoverage: true, PartialCoverageCount: 1, ContentCreatedAt: createdAt, ValidationStatus: "valid", Status: "published"},
	}
}

func testBackend() *fakeBackend {
	payload := []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n")
	return &fakeBackend{token: "rv1." + strings.Repeat("a", 32) + "." + strings.Repeat("B", 43), artifact: application.ArtifactPayload{Artifact: application.ArtifactBuildRecord{ID: strings.Repeat("c", 32), OutputID: strings.Repeat("d", 32), ArtifactHash: strings.Repeat("e", 64), SizeBytes: int64(len(payload)), ContentType: "application/x-bat", ContentCreatedAt: time.Date(2026, 8, 20, 12, 0, 0, 999, time.UTC)}, Payload: payload, Descriptor: domain.RendererDescriptor{ID: "keenetic-route-bat", Version: "keenetic-bat-ipv4-v1", ContentType: "application/x-bat", FileExtension: "bat"}}}
}

func TestArtifactDownloadIsNamedAndTypedByTheRendererDescriptor(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/artifacts/"+strings.Repeat("c", 32), nil)
	request.Host = "127.0.0.1:8765"
	request.RemoteAddr = "127.0.0.1:1"
	recorder := httptest.NewRecorder()
	server.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/x-bat" {
		t.Fatalf("content type = %q", got)
	}
	wantDisposition := `attachment; filename="routevane-` + strings.Repeat("d", 32) + `-` + strings.Repeat("e", 64) + `.bat"`
	if got := recorder.Header().Get("Content-Disposition"); got != wantDisposition {
		t.Fatalf("content disposition = %q, want %q", got, wantDisposition)
	}

	// A different format must produce a different name and type from the same
	// handler, with no per-format constant at this boundary.
	backend.artifact.Descriptor = domain.RendererDescriptor{ID: "singbox-ruleset-json", Version: "singbox-source-json-v1", ContentType: "application/json", FileExtension: "json"}
	backend.artifact.Artifact.ContentType = "application/json"
	recorder = httptest.NewRecorder()
	server.handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.HasSuffix(got, `.json"`) {
		t.Fatalf("content disposition = %q", got)
	}
}

// TestUIAvailableReportsWhetherTheControlSurfaceIsEmbedded exists because the
// failure it guards is silent: a binary built without the generated assets
// serves the API and no page, and an operator has no way to tell from the
// browser which of the two happened.
func TestUIAvailableReportsWhetherTheControlSurfaceIsEmbedded(t *testing.T) {
	embedded := loadStaticAssets(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><main>Routevane UI</main>")},
	})
	withUI, err := newServer("http://127.0.0.1:8765", testBackend(), nil, embedded)
	if err != nil {
		t.Fatal(err)
	}
	if !withUI.UIAvailable() {
		t.Fatal("embedded assets must be reported as available")
	}
	// The marker-only tree a fresh checkout carries is not a control surface.
	markerOnly := loadStaticAssets(fstest.MapFS{
		"routevane-ui.marker": &fstest.MapFile{Data: []byte("generated\n")},
	})
	withoutUI, err := newServer("http://127.0.0.1:8765", testBackend(), nil, markerOnly)
	if err != nil {
		t.Fatal(err)
	}
	if withoutUI.UIAvailable() {
		t.Fatal("a tree with no index must not be reported as a control surface")
	}
}

// mutationRequest builds a POST that satisfies the loopback mutation guards.
func mutationRequest(t *testing.T, path, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765"+path, strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:54321"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	return request
}

type repeatedByteReader byte

func (r repeatedByteReader) Read(payload []byte) (int, error) {
	for i := range payload {
		payload[i] = byte(r)
	}
	return len(payload), nil
}

func TestConfigTransferHTTPContract(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	export := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/config-transfer/export", nil)
	export.RemoteAddr = "127.0.0.1:12345"
	exported := httptest.NewRecorder()
	server.Handler().ServeHTTP(exported, export)
	if exported.Code != http.StatusOK || exported.Header().Get("Cache-Control") != "no-store" || exported.Header().Get("Content-Disposition") != `attachment; filename="routevane-config.json"` {
		t.Fatalf("export = %d headers=%v", exported.Code, exported.Header())
	}

	preview := httptest.NewRecorder()
	server.Handler().ServeHTTP(preview, mutationRequest(t, "/v1/config-transfer/preview", `{"version":"config-transfer-v1.1"}`))
	if preview.Code != http.StatusOK || string(backend.transferPayload) != `{"version":"config-transfer-v1.1"}` {
		t.Fatalf("preview = %d payload=%s", preview.Code, backend.transferPayload)
	}
	apply := httptest.NewRecorder()
	applyRequest := mutationRequest(t, "/v1/config-transfer/apply", ` {"version":"config-transfer-v1.1"} `)
	applyRequest.Header.Set(configTransferDigestHeader, "sha256:"+strings.Repeat("0", 64))
	server.Handler().ServeHTTP(apply, applyRequest)
	if apply.Code != http.StatusOK || backend.transferDigest == "" || string(backend.transferPayload) != ` {"version":"config-transfer-v1.1"} ` {
		t.Fatalf("apply = %d digest=%q payload=%s", apply.Code, backend.transferDigest, backend.transferPayload)
	}
	foreign := mutationRequest(t, "/v1/config-transfer/preview", `{}`)
	foreign.Header.Set("Origin", "http://evil.test")
	refused := httptest.NewRecorder()
	server.Handler().ServeHTTP(refused, foreign)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("foreign-origin preview = %d", refused.Code)
	}
}

func TestConfigTransferHTTPRejectsAnOversizedRawDocumentBeforeTheBackend(t *testing.T) {
	for _, path := range []string{"/v1/config-transfer/preview", "/v1/config-transfer/apply"} {
		t.Run(path, func(t *testing.T) {
			backend := testBackend()
			server, err := New("http://127.0.0.1:8765", backend, nil)
			if err != nil {
				t.Fatal(err)
			}
			body := io.LimitReader(repeatedByteReader('x'), int64(application.ConfigTransferMaxBytes)+1)
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765"+path, body)
			request.RemoteAddr = "127.0.0.1:54321"
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Routevane-Request", "1")
			request.Header.Set(configTransferDigestHeader, "sha256:"+strings.Repeat("0", 64))
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)

			if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), `"code":"config_transfer_too_large"`) {
				t.Fatalf("oversized response = %d %s", response.Code, response.Body.String())
			}
			if backend.transferPayload != nil {
				t.Fatalf("backend received %d oversized bytes", len(backend.transferPayload))
			}
		})
	}
}

func TestConfigTransferHTTPStreamsTheExactMaximumDocumentToTheBackend(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := io.LimitReader(repeatedByteReader('x'), int64(application.ConfigTransferMaxBytes))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/v1/config-transfer/preview", body)
	request.RemoteAddr = "127.0.0.1:54321"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Routevane-Request", "1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("maximum-sized response = %d %s", response.Code, response.Body.String())
	}
	if len(backend.transferPayload) != application.ConfigTransferMaxBytes {
		t.Fatalf("backend received %d bytes, want %d", len(backend.transferPayload), application.ConfigTransferMaxBytes)
	}
}

func TestConfigTransferErrorsUseVersionedCodes(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	backend.transferErr = application.NewTransferError("invalid_json", "")
	invalid := httptest.NewRecorder()
	invalidRequest := mutationRequest(t, "/v1/config-transfer/apply", `{"version":`)
	invalidRequest.Header.Set(configTransferDigestHeader, "sha256:"+strings.Repeat("0", 64))
	server.Handler().ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"code":"config_transfer_invalid_json"`) {
		t.Fatalf("invalid preview = %d %s", invalid.Code, invalid.Body.String())
	}
	backend.transferErr = application.NewTransferError("preview_required", "preview_digest")
	semantic := httptest.NewRecorder()
	server.Handler().ServeHTTP(semantic, mutationRequest(t, "/v1/config-transfer/preview", `{}`))
	if semantic.Code != http.StatusConflict || !strings.Contains(semantic.Body.String(), `"code":"config_transfer_preview_required"`) {
		t.Fatalf("semantic preview = %d %s", semantic.Code, semantic.Body.String())
	}
}

func TestCreateProfilePassesListLocalDomainOverrides(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := mutationRequest(t, "/v1/profiles", `{"name":"Example","lists":["example"],"priority":["example"],"list_domains":{"example":["custom.example"]}}`)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
	want := map[string][]string{"example": {"custom.example"}}
	if !reflect.DeepEqual(backend.createdComposition.ListDomains, want) {
		t.Fatalf("composition=%#v", backend.createdComposition)
	}
	if !reflect.DeepEqual(backend.createdComposition.Priority, []string{"example"}) {
		t.Fatalf("priority=%#v", backend.createdComposition.Priority)
	}
}

// The forecast route answers the numbers a screen refuses a device on, for a
// composition that does not exist yet. The exact document matters: the browser
// reads maximum_rules against projected_rules and shows the shortfall by
// list, so a Keenetic that cannot hold the collection is named before the
// profile is created rather than by its first failed build.
func TestCompositionForecastAnswersEveryRequestedTargetWithItsOwnNumbers(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(t, "/v1/profiles/preview",
		`{"lists":["youtube","twitch"],"categories":["video"],"exclusions":["vimeo"],"priority":["twitch","youtube"],"list_domains":{"youtube":["custom.example"]},"targets":["keenetic","singbox"]}`))
	const want = `{"targets":[` +
		`{"target_id":"keenetic","maximum_rules":1024,"projected_rules":1742,"fits":false,"per_list":[{"list_id":"twitch","rules":1230},{"list_id":"youtube","rules":512}],"overlaps":{"items":[],"truncated":false}},` +
		`{"target_id":"singbox","maximum_rules":8192,"projected_rules":1742,"fits":true,"per_list":[{"list_id":"twitch","rules":1230},{"list_id":"youtube","rules":512}],"overlaps":{"items":[],"truncated":false}}` +
		`]}` + "\n"
	if response.Code != http.StatusOK || response.Body.String() != want {
		t.Fatalf("code=%d body=%s want=%s", response.Code, response.Body.String(), want)
	}
	// The composition reaches the forecast whole. A screen that forecast only
	// the named lists would promise a number for a different profile than the
	// one it is about to create.
	wantComposition := application.ProfileComposition{
		Lists: []string{"youtube", "twitch"}, Categories: []string{"video"}, Exclusions: []string{"vimeo"},
		ListDomains: map[string][]string{"youtube": {"custom.example"}}, Priority: []string{"twitch", "youtube"},
	}
	if !reflect.DeepEqual(backend.forecastComposition, wantComposition) {
		t.Fatalf("composition=%#v", backend.forecastComposition)
	}
	if !reflect.DeepEqual(backend.forecastTargets, []string{"keenetic", "singbox"}) {
		t.Fatalf("targets=%#v", backend.forecastTargets)
	}
}

// An omitted target set asks about the whole catalog, and a target the catalog
// does not carry is refused rather than dropped from the answer: a caller that
// asked about a device and got nothing back would read the silence as a fit.
func TestCompositionForecastRefusesAnUnknownTargetAndAcceptsAnOmittedSet(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	unknown := httptest.NewRecorder()
	server.Handler().ServeHTTP(unknown, mutationRequest(t, "/v1/profiles/preview",
		`{"lists":["youtube"],"targets":["absent-device"]}`))
	if unknown.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown target code=%d body=%s", unknown.Code, unknown.Body.String())
	}
	omitted := httptest.NewRecorder()
	server.Handler().ServeHTTP(omitted, mutationRequest(t, "/v1/profiles/preview", `{"lists":["youtube"]}`))
	if omitted.Code != http.StatusOK || omitted.Body.String() != "{\"targets\":[]}\n" {
		t.Fatalf("omitted targets code=%d body=%s", omitted.Code, omitted.Body.String())
	}
	if backend.forecastTargets != nil {
		t.Fatalf("an omitted target set reached the backend as %#v rather than as absent", backend.forecastTargets)
	}
	// The forecast reads and computes; it does not deploy or re-observe, so it
	// answers on the same budget as every other screen.
	if spec, owned := routes["profiles.preview"]; !owned || spec.timeout != 0 {
		t.Fatalf("forecast route owned=%v budget=%s, want the default", owned, spec.timeout)
	}
	// GET is not this route: the composition it forecasts travels in a body.
	get := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/profiles/preview", nil)
	get.RemoteAddr = "127.0.0.1:1"
	getResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET code=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
}

// The destination route carries one operator action over a batch, because a
// pasted routes file is one decision and not one request per line. It answers
// with the refreshed contents, so the card that sent the batch reads the result
// of it without a second round trip.
func TestDestinationVerdictsTravelAsOneBatchAndAnswerWithTheContents(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(t, "/v1/lists/example/domains",
		`{"values":["example.com","198.51.100.7"],"verdict":"include"}`))
	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, `"value":"example.com","kind":"domain"`) ||
		!strings.Contains(body, `"value":"198.51.100.7","kind":"ip"`) {
		t.Fatalf("code=%d body=%s", response.Code, body)
	}
	if want := []string{"example/example.com,198.51.100.7=include"}; !reflect.DeepEqual(backend.domainVerdicts, want) {
		t.Fatalf("verdicts = %#v", backend.domainVerdicts)
	}

	// An empty batch is a malformed request, not an action with no effect: the
	// backend is never reached.
	empty := httptest.NewRecorder()
	server.Handler().ServeHTTP(empty, mutationRequest(t, "/v1/lists/example/domains", `{"values":[],"verdict":"include"}`))
	if empty.Code != http.StatusBadRequest || len(backend.domainVerdicts) != 1 {
		t.Fatalf("empty batch code=%d verdicts=%#v", empty.Code, backend.domainVerdicts)
	}

	// A refused batch names the value the operator must fix; the old single
	// "domain" field is gone, so a request carrying it is refused outright.
	refused := httptest.NewRecorder()
	server.Handler().ServeHTTP(refused, mutationRequest(t, "/v1/lists/example/domains",
		`{"values":["example.com","not a destination"],"verdict":"exclude"}`))
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), `"value":"not a destination"`) {
		t.Fatalf("refused batch code=%d body=%s", refused.Code, refused.Body.String())
	}
	legacy := httptest.NewRecorder()
	server.Handler().ServeHTTP(legacy, mutationRequest(t, "/v1/lists/example/domains", `{"domain":"example.com","verdict":"exclude"}`))
	if legacy.Code != http.StatusBadRequest || len(backend.domainVerdicts) != 1 {
		t.Fatalf("single-value payload code=%d verdicts=%#v", legacy.Code, backend.domainVerdicts)
	}
}

// TestDeployTakesTheCredentialAndNeverReturnsOrLogsIt is the assertion that
// matters most about this endpoint: the password crosses the boundary once, in
// the request body, and appears nowhere else.
func TestDeployTakesTheCredentialAndNeverReturnsOrLogsIt(t *testing.T) {
	backend := testBackend()
	backend.deployResult = application.DeployResult{
		Device:     application.DeviceInfo{DeployerID: "keenetic-route-bat", Vendor: "Keenetic", FirmwareVersion: "5.1.2", FormatKey: "keenetic-bat-ipv4-v1", Interface: "Wireguard0"},
		Backup:     application.BackupRef{ID: strings.Repeat("f", 64), Path: "backups/x", Hash: strings.Repeat("f", 64), SizeBytes: 12, CreatedAt: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)},
		ArtifactID: strings.Repeat("c", 32),
		Applied:    true,
		Events:     []application.DeployEvent{{Step: application.StepProbe, Outcome: "success"}},
	}
	var logs bytes.Buffer
	server, err := New("http://127.0.0.1:8765", backend, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "SuperSecret123"
	path := "/v1/artifacts/" + strings.Repeat("c", 32) + "/deploy"
	body := `{"device":"http://192.168.1.1","username":"admin","password":"` + secret + `","interface":"Wireguard0","confirm":true}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(t, path, body))
	if response.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
	if len(backend.deployCommands) != 1 {
		t.Fatalf("commands = %#v", backend.deployCommands)
	}
	command := backend.deployCommands[0]
	if command.Connection.Password != secret || !command.Confirm {
		t.Fatalf("the deployer did not receive the connection: %#v", command.Connection.Redacted())
	}
	if strings.Contains(response.Body.String(), secret) {
		t.Fatalf("the response echoed the credential: %s", response.Body.String())
	}
	if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), "192.168.1.1") {
		t.Fatalf("the log recorded the connection: %s", logs.String())
	}
	var decoded struct {
		Confirmed bool                     `json:"confirmed"`
		Result    application.DeployResult `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Confirmed || !decoded.Result.Applied || decoded.Result.Device.FirmwareVersion != "5.1.2" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestDeployWithoutConfirmationReportsThePlanAndChangesNothing(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/artifacts/" + strings.Repeat("c", 32) + "/deploy"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(t, path, `{"device":"http://192.168.1.1","username":"admin","password":"x","interface":"Wireguard0"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"confirmed":false`) {
		t.Fatalf("body = %s", response.Body.String())
	}
	if len(backend.deployCommands) != 1 || backend.deployCommands[0].Confirm {
		t.Fatalf("commands = %#v", backend.deployCommands)
	}
}

func TestDeployRefusesUnguardedAndMalformedRequests(t *testing.T) {
	path := "/v1/artifacts/" + strings.Repeat("c", 32) + "/deploy"
	body := `{"device":"http://192.168.1.1","username":"admin","password":"x","interface":"Wireguard0","confirm":true}`

	// A cross-origin page must not be able to change a device, and neither must
	// a request that omits the mutation marker.
	unguarded := map[string]func(*http.Request){
		"no marker":    func(r *http.Request) { r.Header.Del("X-Routevane-Request") },
		"wrong marker": func(r *http.Request) { r.Header.Set("X-Routevane-Request", "0") },
		"foreign origin": func(r *http.Request) {
			r.Header.Set("Origin", "http://evil.test")
		},
		"form content type": func(r *http.Request) {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		},
		"remote address": func(r *http.Request) { r.RemoteAddr = "203.0.113.5:9999" },
	}
	for name, mutate := range unguarded {
		t.Run(name, func(t *testing.T) {
			backend := testBackend()
			server, err := New("http://127.0.0.1:8765", backend, nil)
			if err != nil {
				t.Fatal(err)
			}
			request := mutationRequest(t, path, body)
			mutate(request)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code < 400 {
				t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
			}
			if len(backend.deployCommands) != 0 {
				t.Fatalf("a refused request reached the deployer: %#v", backend.deployCommands)
			}
		})
	}

	// The method is fixed: a device change is never a GET.
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
	get.RemoteAddr = "127.0.0.1:54321"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, get)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d", response.Code)
	}
}

func TestDeployReportsAFailureWithItsAuditTrail(t *testing.T) {
	backend := testBackend()
	backend.deployErr = application.ErrDeviceIncompatible
	backend.deployResult = application.DeployResult{
		Events: []application.DeployEvent{{Step: application.StepProbe, Outcome: "success", Detail: "4.9.9"}},
	}
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/artifacts/" + strings.Repeat("c", 32) + "/deploy"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(t, path, `{"device":"http://192.168.1.1","username":"admin","password":"x","interface":"Wireguard0","confirm":true}`))
	if response.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"error":"device_incompatible"`) || !strings.Contains(body, `"probe"`) {
		t.Fatalf("a failed deployment must return its code and its audit trail: %s", body)
	}
}

func TestProfileListingServesTheLibraryRows(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/profiles", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
	var decoded struct {
		Profiles []application.ProfileCard `json:"profiles"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Profiles) != 1 || len(decoded.Profiles[0].Outputs) != 1 || decoded.Profiles[0].Outputs[0].TargetKind != "router" || decoded.Profiles[0].Outputs[0].Latest == nil || decoded.Profiles[0].Outputs[0].Latest.SizeBytes != 42 {
		t.Fatalf("profiles = %#v", decoded.Profiles)
	}
	// The same path still refuses a verb it does not serve, naming both.
	other := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1:8765/v1/profiles", nil)
	other.RemoteAddr = "127.0.0.1:54321"
	refused := httptest.NewRecorder()
	server.Handler().ServeHTTP(refused, other)
	if refused.Code != http.StatusMethodNotAllowed || refused.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("code=%d allow=%q", refused.Code, refused.Header().Get("Allow"))
	}
}

// Archival is two verbs at two paths, not one toggle: a request that arrives
// twice must not put the profile back where it started. The refusal an
// archived profile answers an edit with is a conflict, because the request is
// well formed and the state is what rejects it.
func TestArchiveAndRestoreAreSeparateVerbsAndAnArchivedEditConflicts(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	profileID := strings.Repeat("a", 32)
	post := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765"+path, strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:54321"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Routevane-Request", "1")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}

	archived := post("/v1/profiles/"+profileID+"/archive", `{}`)
	if archived.Code != http.StatusOK || !backend.archived {
		t.Fatalf("archive code=%d archived=%v body=%s", archived.Code, backend.archived, archived.Body.String())
	}
	var decoded struct {
		Format application.Profile `json:"profile"`
	}
	if err := json.Unmarshal(archived.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Format.Archived() {
		t.Fatalf("the reply must state the resulting state: %s", archived.Body.String())
	}

	// Repeating the same verb is still the same state, and an edit is refused
	// with a conflict rather than an unprocessable body.
	if again := post("/v1/profiles/"+profileID+"/archive", `{}`); again.Code != http.StatusOK || !backend.archived {
		t.Fatalf("repeat archive code=%d archived=%v", again.Code, backend.archived)
	}
	edit := post("/v1/profiles/"+profileID+"/update", `{"name":"renamed","lists":["example"]}`)
	if edit.Code != http.StatusConflict {
		t.Fatalf("editing an archived list code=%d body=%s", edit.Code, edit.Body.String())
	}

	restored := post("/v1/profiles/"+profileID+"/restore", `{}`)
	if restored.Code != http.StatusOK || backend.archived {
		t.Fatalf("restore code=%d archived=%v", restored.Code, backend.archived)
	}
	if edit := post("/v1/profiles/"+profileID+"/update", `{"name":"renamed","lists":["example"]}`); edit.Code != http.StatusOK {
		t.Fatalf("editing a restored list code=%d body=%s", edit.Code, edit.Body.String())
	}

	// Archiving is a mutation, so the guarded verb is the only one served.
	read := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/profiles/"+profileID+"/archive", nil)
	read.RemoteAddr = "127.0.0.1:54321"
	refused := httptest.NewRecorder()
	server.Handler().ServeHTTP(refused, read)
	if refused.Code != http.StatusMethodNotAllowed || refused.Header().Get("Allow") != "POST" {
		t.Fatalf("code=%d allow=%q", refused.Code, refused.Header().Get("Allow"))
	}
}

func TestDeployableTargetsDescribeWhatEachDeployerNeeds(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/deployments/targets", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("code=%d", response.Code)
	}
	var decoded struct {
		Targets []application.DeployableTarget `json:"targets"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Targets) != 1 || decoded.Targets[0].TargetID != "keenetic" {
		t.Fatalf("targets = %#v", decoded.Targets)
	}
	if !decoded.Targets[0].Requirements.NeedsCredential || !decoded.Targets[0].Requirements.NeedsInterface {
		t.Fatalf("requirements = %#v", decoded.Targets[0].Requirements)
	}
}

// everyAPIPath is one path per route this transport answers. It is written by
// hand on purpose: it is the second entry in a double-entry ledger against the
// routes table, and the two tests below fail if either side gains an entry the
// other does not have. A route that resolved but had no owner used to answer an
// empty 200, and nothing but three switches happening to agree prevented it.
var everyAPIPath = []struct {
	path  string
	route string
}{
	{"/", "status"},
	{"/health", "health"},
	{"/v1/config-transfer/export", "config-transfer.export"},
	{"/v1/config-transfer/preview", "config-transfer.preview"},
	{"/v1/config-transfer/apply", "config-transfer.apply"},
	{"/v1/lists", "lists.collection"},
	{"/v1/lists/priority", "lists.priority"},
	{"/v1/lists/example/preview", "lists.preview"},
	{"/v1/lists/custom-1234567890abcdef/update", "lists.update"},
	{"/v1/lists/example/remove", "lists.remove"},
	{"/v1/lists/example/contents", "lists.contents"},
	{"/v1/lists/example/refresh", "lists.refresh"},
	{"/v1/lists/example/sources", "lists.sources"},
	{"/v1/lists/example/sources/feed-1234567890abcdef/update", "lists.sources.update"},
	{"/v1/lists/example/sources/feed-1234567890abcdef/remove", "lists.sources.remove"},
	{"/v1/lists/example/domains", "lists.domains"},
	{"/v1/categories", "categories.create"},
	{"/v1/categories/custom-1234567890abcdef/update", "categories.update"},
	{"/v1/categories/custom-1234567890abcdef/remove", "categories.remove"},
	{"/v1/targets", "targets"},
	{"/v1/export-formats", "export-formats"},
	{"/v1/deployments/targets", "deployments.targets"},
	{"/v1/settings", "settings.get"},
	{"/v1/settings/update", "settings.update"},
	{"/v1/profiles", "profiles.collection"},
	{"/v1/profiles/preview", "profiles.preview"},
	{"/v1/profiles/" + testID + "", "profiles.get"},
	{"/v1/profiles/" + testID + "/update", "profiles.update"},
	{"/v1/profiles/" + testID + "/refresh", "profiles.refresh"},
	{"/v1/profiles/" + testID + "/outputs", "profiles.outputs"},
	{"/v1/profiles/" + testID + "/schedule", "profiles.schedule"},
	{"/v1/profiles/" + testID + "/archive", "profiles.archive"},
	{"/v1/profiles/" + testID + "/restore", "profiles.restore"},
	{"/v1/profiles/" + testID + "/export", "profiles.export"},
	{"/v1/devices", "devices.collection"},
	{"/v1/devices/" + testID + "/update", "devices.update"},
	{"/v1/devices/" + testID + "/forget", "devices.forget"},
	{"/v1/devices/" + testID + "/auto-delivery", "devices.autodelivery"},
	{"/v1/outputs/" + testID + "", "outputs.get"},
	{"/v1/outputs/" + testID + "/build", "outputs.build"},
	{"/v1/outputs/" + testID + "/device", "outputs.device"},
	{"/v1/subscriptions/" + testID + "", "subscriptions.get"},
	{"/v1/snapshots/" + testID + "", "snapshots.get"},
	{"/v1/artifacts/" + testID + "", "artifacts.get"},
	{"/v1/artifacts/" + testID + "/deploy", "artifacts.deploy"},
}

const testID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// resolveRoute answers which route a path resolves to, through the same router
// and the same table the transport serves with. It returns an empty name for a
// path the router answered itself, which is a redirect this transport does not
// issue and must therefore never see.
func resolveRoute(path string) string {
	resolved := ""
	mux := newRouteMux(func(route string) http.Handler {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) { resolved = route })
	})
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
	mux.ServeHTTP(httptest.NewRecorder(), request)
	return resolved
}

func TestEveryResolvedRouteHasAnOwnerInTheRoutesTable(t *testing.T) {
	for _, entry := range everyAPIPath {
		route := resolveRoute(entry.path)
		if route != entry.route {
			t.Errorf("%s resolved to %q, want %q", entry.path, route, entry.route)
			continue
		}
		if routes[route].handle == nil {
			t.Errorf("%s resolves to %q, which owns no handler: it would answer an empty 200", entry.path, route)
		}
	}
}

func TestEveryRouteInTheTableIsReachable(t *testing.T) {
	reachable := map[string]bool{
		// ui is reached from the UI-path fallback rather than from a parsed API
		// path, and status becomes ui when this build carries the surface.
		"ui": true,
	}
	for _, entry := range everyAPIPath {
		reachable[entry.route] = true
	}
	for route := range routes {
		if !reachable[route] {
			t.Errorf("route %q has an owner but no path reaches it", route)
		}
	}
}

// A path form the transport does not answer is not found, never an empty
// success. /v1/devices/{id} is the concrete case: it used to resolve to a route
// name with no handler.
func TestUnhandledPathFormsAreNotFound(t *testing.T) {
	for _, path := range []string{
		"/v1/devices/" + testID,
		"/v1/categories" + "/custom-1234567890abcdef",
		"/v1/settings/" + testID,
		"/v1/artifacts/" + testID + "/install",
		"/v1/profiles/" + testID + "/publish",
	} {
		// Only the catch-all may claim these. An API route that took one would
		// answer it, and the fallback is what turns it into not found.
		if route := resolveRoute(path); route != routeUI {
			t.Errorf("%s resolves to route %q, which this transport does not answer there", path, route)
		}
		server, err := New("http://127.0.0.1:8765", testBackend(), nil)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
		request.Host = "127.0.0.1:8765"
		request.RemoteAddr = "127.0.0.1:1"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status=%d body=%q, want 404", path, response.Code, response.Body.String())
		}
	}
}

// This transport answers; it never sends a client somewhere else. A router is
// entitled to redirect a path outside canonical form, and a 3xx from a loopback
// API is a response form every client would have to learn to follow.
func TestNoRequestIsAnsweredWithARedirect(t *testing.T) {
	assets := loadStaticAssets(fstest.MapFS{
		"index.html":          &fstest.MapFile{Data: []byte("<!doctype html><main>Routevane UI</main>")},
		"_nuxt/app.123456.js": &fstest.MapFile{Data: []byte("export const ready = true\n")},
	})
	server, err := newServer("http://127.0.0.1:8765", testBackend(), nil, assets)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/", "/health", "/health/", "/v1", "/v1/", "/v1//lists", "/v1/profiles/", "//",
		"/v1/profiles/" + testID + "/", "/v1/./lists", "/_nuxt/", "/_nuxt/app.123456.js",
		"/setup/diagnostics", "/setup/diagnostics/",
	} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
		request.Host = "127.0.0.1:8765"
		request.RemoteAddr = "127.0.0.1:1"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code >= 300 && response.Code < 400 {
			t.Errorf("GET %s status=%d location=%q", path, response.Code, response.Header().Get("Location"))
		}
		if location := response.Header().Get("Location"); location != "" {
			t.Errorf("GET %s carries Location=%q", path, location)
		}
	}
}

// A read route answers GET and nothing else. HEAD is not implied by it: the
// table is the contract, and a router that widened it would answer a bodyless
// 200 where this transport has always answered 405.
func TestHeadIsNotImpliedByAReadRoute(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodHead, "http://127.0.0.1:8765/v1/targets", nil)
	request.Host = "127.0.0.1:8765"
	request.RemoteAddr = "127.0.0.1:1"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET" {
		t.Fatalf("HEAD /v1/targets status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

// The route that changes a device outlives the budget sized for answering a
// screen. Cutting it at the screen's budget is what leaves a device half
// written, so the table must give it its own.
func TestTheDeployRouteCarriesItsOwnBudget(t *testing.T) {
	spec, owned := routes["artifacts.deploy"]
	if !owned {
		t.Fatal("the deploy route has no owner")
	}
	if spec.timeout <= requestTimeout {
		t.Fatalf("deploy budget = %s, default = %s: the deployment lifecycle does not fit", spec.timeout, requestTimeout)
	}
	// The list refresh re-observes several bounded feeds in sequence, so it
	// carries its own budget too: larger than a screen answer, smaller than a
	// device deployment.
	refresh, owned := routes["lists.refresh"]
	if !owned || refresh.timeout <= requestTimeout || refresh.timeout >= spec.timeout {
		t.Fatalf("service refresh budget = %s", refresh.timeout)
	}
	for route, other := range routes {
		if route == "artifacts.deploy" || route == "lists.refresh" || other.timeout == 0 {
			continue
		}
		t.Errorf("route %q also overrides the budget (%s); every override needs its own reason", route, other.timeout)
	}
}

// The category contract is what the control surface codes against, so the
// exact documents matter: creation answers the merged category, an update
// answers the merged category, and a deletion answers nothing at all.
func TestCategoryRoutesAnswerTheirFrozenContract(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	send := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, mutationRequest(t, path, body))
		return response
	}

	created := send("/v1/categories", `{"title":"Мои списки","lists":["example"]}`)
	wantCreated := `{"category":{"id":"custom-1234567890abcdef","title":"Мои списки","lists":["example"],"custom":true}}` + "\n"
	if created.Code != http.StatusCreated || created.Body.String() != wantCreated {
		t.Fatalf("create code=%d body=%s want=%s", created.Code, created.Body.String(), wantCreated)
	}
	// lists is optional: a category may be created empty and filled later.
	if empty := send("/v1/categories", `{"title":"Пустая"}`); empty.Code != http.StatusCreated {
		t.Fatalf("create without services code=%d body=%s", empty.Code, empty.Body.String())
	}
	if invalid := send("/v1/categories", `{"title":"   "}`); invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid title code=%d body=%s", invalid.Code, invalid.Body.String())
	}

	updated := send("/v1/categories/custom-1234567890abcdef/update", `{"title":"Мои списки 2","lists":["example"]}`)
	wantUpdated := `{"category":{"id":"custom-1234567890abcdef","title":"Мои списки 2","lists":["example"],"custom":true}}` + "\n"
	if updated.Code != http.StatusOK || updated.Body.String() != wantUpdated {
		t.Fatalf("update code=%d body=%s want=%s", updated.Code, updated.Body.String(), wantUpdated)
	}
	// A shipped category takes membership and refuses a title, because the
	// catalog owns the words and would answer differently on the next load.
	members := send("/v1/categories/diagnostic/update", `{"lists":["example","other"]}`)
	wantMembers := `{"category":{"id":"diagnostic","title":"Diagnostic","lists":["example","other"],"custom":false}}` + "\n"
	if members.Code != http.StatusOK || members.Body.String() != wantMembers {
		t.Fatalf("catalog membership code=%d body=%s want=%s", members.Code, members.Body.String(), wantMembers)
	}
	if rename := send("/v1/categories/diagnostic/update", `{"title":"Диагностика"}`); rename.Code != http.StatusUnprocessableEntity {
		t.Fatalf("catalog rename code=%d body=%s", rename.Code, rename.Body.String())
	}
	if unknown := send("/v1/categories/absent/update", `{"lists":[]}`); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown update code=%d body=%s", unknown.Code, unknown.Body.String())
	}

	// An absent field and an empty one are different requests. The transport
	// must carry that difference: omitting lists leaves membership alone,
	// while sending an empty array clears it.
	membersOnly, titleOnly, cleared := backend.categoryEdits[1], backend.categoryEdits[2], backend.categoryEdits[3]
	if membersOnly.Title != nil || membersOnly.Lists == nil {
		t.Fatalf("a membership-only edit reached the backend as %#v", membersOnly)
	}
	if titleOnly.Title == nil || titleOnly.Lists != nil {
		t.Fatalf("a title-only edit reached the backend as %#v", titleOnly)
	}
	if cleared.Title != nil || cleared.Lists == nil || len(*cleared.Lists) != 0 {
		t.Fatalf("an empty membership reached the backend as %#v", cleared)
	}

	// Deletion asks one question and requires an answer. An unstated or
	// unknown disposition is refused, because "detach" and "delete" are
	// different outcomes for the operator's data (ADR 0029).
	for _, body := range []string{`{}`, `{"lists":""}`, `{"lists":"purge"}`} {
		if bad := send("/v1/categories/custom-1234567890abcdef/remove", body); bad.Code != http.StatusUnprocessableEntity {
			t.Fatalf("disposition %s code=%d body=%s", body, bad.Code, bad.Body.String())
		}
	}
	// The retired name is now an unknown field, which is what a request written
	// against the previous vocabulary looks like.
	if unknownField := send("/v1/categories/custom-1234567890abcdef/remove", `{"lists":"detach","services":[]}`); unknownField.Code != http.StatusBadRequest {
		t.Fatalf("unknown field code=%d body=%s", unknownField.Code, unknownField.Body.String())
	}
	if unknown := send("/v1/categories/absent/remove", `{"lists":"detach"}`); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown deletion code=%d body=%s", unknown.Code, unknown.Body.String())
	}
	backend.categoryInUse = []application.ProfileReference{
		{ID: strings.Repeat("a", 32), Title: "Дом"},
		{ID: strings.Repeat("b", 32), Title: "Офис"},
	}
	inUse := send("/v1/categories/custom-1234567890abcdef/remove", `{"lists":"delete"}`)
	wantInUse := `{"error":"category in use","profiles":[` +
		`{"id":"` + strings.Repeat("a", 32) + `","title":"Дом"},` +
		`{"id":"` + strings.Repeat("b", 32) + `","title":"Офис"}]}` + "\n"
	if inUse.Code != http.StatusConflict || inUse.Body.String() != wantInUse {
		t.Fatalf("in-use deletion code=%d body=%s want=%s", inUse.Code, inUse.Body.String(), wantInUse)
	}

	backend.categoryInUse = nil
	removed := send("/v1/categories/custom-1234567890abcdef/remove", `{"lists":"detach"}`)
	if removed.Code != http.StatusNoContent || removed.Body.Len() != 0 {
		t.Fatalf("deletion code=%d body=%q", removed.Code, removed.Body.String())
	}
	// A shipped category is deleted the same way: the operator owns the
	// library, and the removal is recorded rather than written to the catalog.
	shipped := send("/v1/categories/diagnostic/remove", `{"lists":"delete"}`)
	if shipped.Code != http.StatusNoContent || shipped.Body.Len() != 0 {
		t.Fatalf("catalog deletion code=%d body=%q", shipped.Code, shipped.Body.String())
	}
	// The disposition reaches the application as the operator stated it; a
	// transport that defaulted it would delete data no one asked to delete.
	if !reflect.DeepEqual(backend.categoryRemoved, []string{"custom-1234567890abcdef:detach", "diagnostic:delete"}) {
		t.Fatalf("removed = %#v", backend.categoryRemoved)
	}

	// GET is not one of these routes: every one of them carries a body.
	get := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/categories", nil)
	get.RemoteAddr = "127.0.0.1:1"
	getResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET code=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
}

// Deleting a list is its own route with its own refusal. The control surface
// codes against the exact document, so the refusal says "list in use" and
// keys the profiles that hold it under "profiles".
func TestRemoveListRouteAnswersItsFrozenContract(t *testing.T) {
	backend := testBackend()
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	send := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, mutationRequest(t, path, body))
		return response
	}

	if unknown := send("/v1/lists/absent/remove", `{}`); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown deletion code=%d body=%s", unknown.Code, unknown.Body.String())
	}
	// The route asks nothing, so it carries nothing: a body with content is a
	// request this route does not answer.
	if extra := send("/v1/lists/example/remove", `{"lists":"delete"}`); extra.Code != http.StatusBadRequest {
		t.Fatalf("non-empty body code=%d body=%s", extra.Code, extra.Body.String())
	}
	backend.listInUse = []application.ProfileReference{
		{ID: strings.Repeat("a", 32), Title: "Дом"},
		{ID: strings.Repeat("b", 32), Title: "Офис"},
	}
	inUse := send("/v1/lists/example/remove", `{}`)
	wantInUse := `{"error":"list in use","profiles":[` +
		`{"id":"` + strings.Repeat("a", 32) + `","title":"Дом"},` +
		`{"id":"` + strings.Repeat("b", 32) + `","title":"Офис"}]}` + "\n"
	if inUse.Code != http.StatusConflict || inUse.Body.String() != wantInUse {
		t.Fatalf("in-use deletion code=%d body=%s want=%s", inUse.Code, inUse.Body.String(), wantInUse)
	}

	backend.listInUse = nil
	removed := send("/v1/lists/example/remove", `{}`)
	if removed.Code != http.StatusNoContent || removed.Body.Len() != 0 {
		t.Fatalf("deletion code=%d body=%q", removed.Code, removed.Body.String())
	}
	if !reflect.DeepEqual(backend.listsRemoved, []string{"example"}) {
		t.Fatalf("removed = %#v", backend.listsRemoved)
	}

	// GET is not one of these routes: they are mutations and answer POST only.
	get := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/lists/example/remove", nil)
	get.RemoteAddr = "127.0.0.1:1"
	getResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET code=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
}

// Both library deletions pass the same guard as every other mutation: a page
// on another origin must not be able to delete what the operator collected.
func TestLibraryDeletionsRefuseUnguardedRequests(t *testing.T) {
	unguarded := map[string]func(*http.Request){
		"no marker":         func(r *http.Request) { r.Header.Del("X-Routevane-Request") },
		"wrong marker":      func(r *http.Request) { r.Header.Set("X-Routevane-Request", "0") },
		"foreign origin":    func(r *http.Request) { r.Header.Set("Origin", "http://evil.test") },
		"form content type": func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") },
	}
	deletions := map[string]string{
		"/v1/lists/example/remove":                      `{}`,
		"/v1/categories/custom-1234567890abcdef/remove": `{"lists":"delete"}`,
	}
	for path, body := range deletions {
		for name, mutate := range unguarded {
			t.Run(path+"/"+name, func(t *testing.T) {
				backend := testBackend()
				server, err := New("http://127.0.0.1:8765", backend, nil)
				if err != nil {
					t.Fatal(err)
				}
				request := mutationRequest(t, path, body)
				mutate(request)
				response := httptest.NewRecorder()
				server.Handler().ServeHTTP(response, request)
				if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "request rejected") {
					t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
				}
				if len(backend.listsRemoved) != 0 || len(backend.categoryRemoved) != 0 {
					t.Fatalf("a refused request deleted from the library: %#v %#v", backend.listsRemoved, backend.categoryRemoved)
				}
			})
		}
	}
}

// The catalog listing carries the merged categories, and custom is present on
// every one of them: an absent field would leave the surface guessing which
// categories it may rename.
func TestListListingCarriesMergedCategoriesWithTheirOwnership(t *testing.T) {
	backend := testBackend()
	backend.categories = []application.CategoryDetail{
		{ID: "custom-1234567890abcdef", Title: "Мои списки", Lists: []string{"example"}, Custom: true},
		{ID: "diagnostic", Title: "Diagnostic", Lists: []string{"example"}},
	}
	server, err := New("http://127.0.0.1:8765", backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/lists", nil)
	request.RemoteAddr = "127.0.0.1:1"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
	var decoded struct {
		Categories []application.CategoryDetail `json:"categories"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Categories, backend.categories) {
		t.Fatalf("categories = %#v", decoded.Categories)
	}
	if !strings.Contains(response.Body.String(), `"id":"diagnostic","title":"Diagnostic","lists":["example"],"custom":false`) {
		t.Fatalf("a shipped category omitted its ownership: %s", response.Body.String())
	}
}
