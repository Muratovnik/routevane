// Package httpapi owns Routevane's loopback-only transport.
package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	maxJSONBytes   = 64 << 10
	requestTimeout = 20 * time.Second
	// deployRequestTimeout is the budget for the one route that changes a
	// device. The default request budget is sized for answering a screen; a
	// deployment probes, backs up, writes and verifies over a home router's
	// own connection, and every one of those steps has its own budget. Cutting
	// the request at the screen's budget is what leaves a device half-written,
	// so this route is given a budget that fits its lifecycle instead.
	deployRequestTimeout = 3 * time.Minute
	// refreshRequestTimeout is the budget for re-observing one list's
	// automatic sources on demand. Each source carries its own bounded fetch,
	// but a list may hold several slow feeds in sequence, and cutting the
	// cycle at the screen's budget would persist a half-observed list.
	refreshRequestTimeout = time.Minute
	// profileRefreshRequestTimeout is the budget for re-observing every list
	// of one profile. The lists are observed one after another, each source
	// inside its own bounded fetch, so a profile of many lists outgrows the
	// one-list budget; cutting it there leaves the later lists unobserved
	// while the earlier ones already moved on. It stays well inside the
	// desktop transport's ten-minute abort, write slack included, so the
	// operator gets this route's own answer rather than a transport failure.
	profileRefreshRequestTimeout = 5 * time.Minute
	// credentialRequestTimeout is the budget for a route that may save a
	// device password. Saving signs in to the device first, read-only, inside
	// the check's own budget (ADR 0059); the screen's budget is added on top so
	// waiting for the delivery gate and writing the store keep their time after
	// a slow device.
	credentialRequestTimeout = requestTimeout + application.CredentialCheckBudget
	// cleanupRequestTimeout is the budget for retiring a format or a profile.
	// Without the opt-in a retirement answers at once; with it, the route first
	// runs a send's device lifecycle once for every surface the formats
	// delivered to, one after another, so it outgrows a single send. A
	// removal cut by this budget is rolled back on its own detached budget
	// (ADR 0020) and nothing is retired. It stays inside the desktop
	// transport's ten-minute abort, write slack included.
	cleanupRequestTimeout = 5 * time.Minute
)

type Backend interface {
	ExportConfigTransfer(context.Context) ([]byte, error)
	PreviewConfigTransfer([]byte) (application.ConfigTransferPreview, error)
	ApplyConfigTransfer(context.Context, string, []byte) (application.ConfigTransferCounts, error)
	Lists() []string
	DefaultPriority(context.Context) ([]string, error)
	SetDefaultPriority(context.Context, []string) error
	// ListDetailsWithFreshness is the library listing; each list carries its
	// latest read, read for every list at once.
	ListDetailsWithFreshness(context.Context) ([]application.ListDetail, error)
	PreviewList(context.Context, string) (application.ListPreview, error)
	// Custom lists are the operator-defined part of the catalog: created
	// and edited here, planned exactly like a shipped list everywhere else.
	CreateCustomList(ctx context.Context, title string, domains []string) (application.CustomList, error)
	UpdateCustomList(ctx context.Context, id, title string, domains []string) (application.CustomList, error)
	// RemoveList deletes one list from the library, whoever created it
	// (ADR 0029). A profile naming it directly refuses the deletion; a profile
	// reaching it through a category simply carries less on its next build.
	RemoveList(ctx context.Context, id string) error
	// List tuning is the operator's standing correction to one list's
	// automatic material: sources switched on and off, added feeds, and
	// verdicts on destinations — domains, addresses, networks. Contents is the
	// one table the list card renders.
	ListContents(ctx context.Context, listID string) (application.ListContents, error)
	RefreshListByID(ctx context.Context, listID string) (application.ListRefresh, error)
	SetListSourceEnabled(ctx context.Context, listID, sourceID string, enabled bool) error
	AddListSource(ctx context.Context, listID, url string, format domain.FeedFormat) (application.CustomSource, error)
	RemoveListSource(ctx context.Context, listID, sourceID string) error
	// SetListValues carries one operator action about a batch of
	// destinations: a pasted or imported file is one request, not one per line.
	SetListValues(ctx context.Context, listID string, values []string, verdict application.DomainVerdict) error
	Categories() []application.CategoryDetail
	// A category is operator-owned over the catalog seed (ADR 0028). Creation,
	// rename and deletion live here; every reader of membership sees the merge
	// the server computed, never a client's own.
	CreateCategory(ctx context.Context, title string, lists []string) (application.CategoryDetail, error)
	UpdateCategory(ctx context.Context, id string, update application.CategoryUpdate) (application.CategoryDetail, error)
	// RemoveCategory carries the disposition of the lists the category held,
	// because deleting a category is two outcomes rather than one and the
	// operator chooses which (ADR 0029).
	RemoveCategory(ctx context.Context, id string, profiles application.CategoryListDisposition) error
	Targets() []application.TargetOption
	ExportFormats() []application.ExportFormat
	Export(context.Context, string, string) (application.ExportPayload, error)
	CreateProfile(context.Context, string, application.ProfileComposition) (application.Profile, error)
	// ForecastCompositionKeeping answers what a composition would cost on each named
	// target before the profile exists. It reads and computes; it stores nothing,
	// which is what lets a screen ask before the operator has committed to
	// anything. The last argument names the targets whose format keeps
	// addresses beside domains (ADR 0057).
	ForecastCompositionKeeping(context.Context, application.ProfileComposition, []string, []string) ([]application.CompositionForecast, error)
	Profile(context.Context, string) (application.Profile, error)
	UpdateProfile(context.Context, string, string, application.ProfileComposition) (application.Profile, error)
	// Archival is its own pair of verbs rather than a field of an update: it is
	// the one edit an archived profile still accepts, so it cannot travel inside
	// the edit it would have to refuse.
	ArchiveProfile(context.Context, string) (application.Profile, error)
	RestoreProfile(context.Context, string) (application.Profile, error)
	// Retirement is the step after archival and the only removal of a profile
	// or a format (ADR 0051). Each answers the moment it took effect.
	DeleteProfile(context.Context, string) (time.Time, error)
	RemoveOutput(context.Context, string) (time.Time, error)
	// The device forms of retirement first remove from every device what the
	// retired formats delivered there, and retire only after every removal
	// verified (ADR 0060). The reads list those objects by surface.
	DeleteProfileCleaningDevices(context.Context, string) (application.Retirement, error)
	RemoveOutputCleaningDevices(context.Context, string) (application.Retirement, error)
	ProfileDeviceObjects(context.Context, string) ([]application.DeviceObjects, error)
	OutputDeviceObjects(context.Context, string) ([]application.DeviceObjects, error)
	// ReissueSubscription returns the complete new token exactly once.
	ReissueSubscription(context.Context, string) (string, error)
	// ResolvedLists and MissingCategories keep resolution on the server. A
	// screen that resolved references itself would answer differently from the
	// build whenever its catalog copy was a moment stale.
	ResolvedLists(application.Profile) []string
	MissingCategories(application.Profile) []string
	DefaultRefreshInterval(context.Context) (application.RefreshInterval, error)
	SetDefaultRefreshInterval(context.Context, application.RefreshInterval) error
	SetProfileRefreshInterval(context.Context, string, application.RefreshInterval) (application.Profile, error)
	ProfileSchedule(context.Context, application.Profile) (application.Schedule, error)
	ProfileCardsPage(context.Context, application.ProfileShelf, string) (application.ProfileCardPage, error)
	AddOutput(context.Context, string, string) (application.CreatedOutput, error)
	SetOutputDevice(context.Context, string, string) (application.Output, error)
	SetOutputFQDNPrefix(context.Context, string, string) (application.Output, error)
	SetOutputKeepAddresses(context.Context, string, bool) (application.Output, error)
	IssueSubscription(context.Context, string) (string, error)
	Output(context.Context, string) (application.Output, error)
	OutputCards(context.Context, string) ([]application.OutputCard, error)
	Refresh(context.Context, string) ([]application.RefreshSummary, error)
	Build(context.Context, string) (application.PublishedBuild, error)
	Subscription(context.Context, string) (application.ArtifactPayload, error)
	Snapshot(context.Context, string) (application.PlanSnapshotRecord, error)
	Artifact(context.Context, string) (application.ArtifactPayload, error)
	// DeployableTargets and Deploy are the deployment surface. They are separate
	// from publication because what can be installed is decided by the format's
	// deployer, not by the catalog.
	// The device registry is its own surface: what an operator installed is a
	// fact about their network, not about the catalog.
	SecretStoreAvailable() bool
	DeviceCards(context.Context) ([]application.DeviceCard, error)
	RegisterDevice(ctx context.Context, targetID, name, address, account, interfaceName string) (application.Device, error)
	UpdateDevice(ctx context.Context, id, name, address, account, interfaceName string) (application.Device, error)
	ForgetDevice(context.Context, string) error
	// SaveDeviceCredential checks the password with a read-only login before
	// keeping it (ADR 0059) and answers what the check found.
	SaveDeviceCredential(ctx context.Context, id, secret string) (application.Device, application.CredentialCheck, error)
	RemoveDeviceCredential(context.Context, string) (application.Device, error)
	EnableAutoDelivery(ctx context.Context, id, secret string) (application.Device, error)
	DisableAutoDelivery(context.Context, string) (application.Device, error)
	DeployableTargets() []application.DeployableTarget
	DeployPlan(context.Context, application.DeployCommand) (application.DeployPlan, error)
	DeployAttempt(context.Context, string, application.DeployCommand) (application.DeploymentAttempt, error)
	DeploymentAttempt(context.Context, string) (application.DeploymentAttempt, error)
}

type Server struct {
	server      *http.Server
	handler     http.Handler
	origin      string
	uiAvailable bool
}

func New(origin string, backend Backend, logger *slog.Logger) (*Server, error) {
	return newServer(origin, backend, logger, embeddedStaticAssets())
}

func newServer(origin string, backend Backend, logger *slog.Logger, assets staticAssets) (*Server, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid HTTP origin")
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid HTTP origin")
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("origin is not IPv4 loopback")
	}
	if backend == nil {
		return nil, fmt.Errorf("HTTP backend is nil")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	h := &handler{origin: origin, host: parsed.Host, backend: backend, logger: logger, slots: make(chan struct{}, 32), requestTimeout: requestTimeout, assets: assets, version: "dev"}
	h.mux = newRouteMux(func(route string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.enter(route, w, r) })
	})
	server := &http.Server{Addr: parsed.Host, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	return &Server{server: server, handler: h, origin: origin, uiAvailable: assets.available}, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

// SetVersion supplies the running binary's build identity. Call before Serve.
func (s *Server) SetVersion(version string) {
	s.handler.(*handler).version = version
}

// RequireDesktopToken confines UI/API access to the owning desktop process.
// Call before Serve. Subscription URLs retain their separate bearer contract.
func (s *Server) RequireDesktopToken(token string) {
	s.handler.(*handler).desktopToken = token
}

// CancelRequestsWith binds requests to the owning process's lifetime.
// Device recovery already detaches from request cancellation with its own budget.
func (s *Server) CancelRequestsWith(ctx context.Context) {
	s.server.BaseContext = func(net.Listener) context.Context { return ctx }
}
func (s *Server) Origin() string { return s.origin }

// UIAvailable reports whether this build carries the generated control surface.
// A binary built without it still serves the API, so the caller is the one who
// says so instead of leaving an operator to discover it in a browser.
func (s *Server) UIAvailable() bool { return s.uiAvailable }
func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return fmt.Errorf("listener is nil")
	}
	tcp, ok := listener.Addr().(*net.TCPAddr)
	if !ok || tcp.IP.To4() == nil || !tcp.IP.IsLoopback() {
		return fmt.Errorf("listener is not IPv4 loopback")
	}
	err := s.server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func (s *Server) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }

type handler struct {
	origin, host   string
	backend        Backend
	logger         *slog.Logger
	slots          chan struct{}
	requestTimeout time.Duration
	assets         staticAssets
	mux            *http.ServeMux
	desktopToken   string
	version        string
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w, h.assets.digest)
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		writeError(w, http.StatusServiceUnavailable, "busy")
		return
	}
	if !loopbackRemote(r.RemoteAddr) || r.Host != h.host {
		writeError(w, http.StatusMisdirectedRequest, "misdirected request")
		return
	}
	if r.URL.RawQuery != "" || r.URL.Fragment != "" || unsafeEscapedPath(r.URL.EscapedPath()) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// A path outside canonical form is one the router would answer with a
	// redirect, and an asterisk-form request carries no path to route at all.
	// Neither has ever named a route here, so both go to the fallback.
	if requestPath := r.URL.Path; !strings.HasPrefix(requestPath, "/") || canonicalPath(requestPath) != requestPath {
		h.serveFallback(w, r)
		return
	}
	h.mux.ServeHTTP(w, r)
}

// enter resolves the two routes a matched path does not name on its own, then
// hands the request to the shared pipeline.
func (h *handler) enter(route string, w http.ResponseWriter, r *http.Request) {
	switch route {
	case routeUI:
		h.serveFallback(w, r)
		return
	case routeStatus:
		// A build that carries the control surface answers its own root with
		// it rather than with the placeholder page.
		if h.assets.available {
			route = routeUI
		}
	}
	h.dispatch(route, w, r)
}

// serveFallback answers a path the API does not claim. The control surface
// takes the ones that could name one of its pages; the rest are not found and
// must never be answered by a page.
func (h *handler) serveFallback(w http.ResponseWriter, r *http.Request) {
	if !h.isUIPath(r.URL.Path) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	h.dispatch(routeUI, w, r)
}

// dispatch is everything the routes share: the methods a route answers, the
// guard every mutation passes, the budget it may spend, and the record of
// which route answered.
func (h *handler) dispatch(route string, w http.ResponseWriter, r *http.Request) {
	if h.desktopToken != "" && route != "subscriptions.get" &&
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Routevane-Desktop")), []byte(h.desktopToken)) != 1 {
		writeError(w, http.StatusForbidden, "request rejected")
		return
	}
	spec := routes[route]
	if spec.handle == nil {
		// A route name no table entry owns is not found. It must never fall
		// through to an empty success: that is exactly how a route table split
		// across several switches answers 200 with nothing.
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !spec.allows(r.Method) {
		w.Header().Set("Allow", spec.allowHeader())
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !h.validMutation(r) {
		writeError(w, http.StatusForbidden, "request rejected")
		return
	}
	budget := h.requestTimeout
	if spec.timeout > 0 {
		budget = spec.timeout
		// The server's write deadline is one value for every route, so a route
		// whose budget exceeds it would have its connection cut mid-answer no
		// matter how long its context lived. The deadline is extended for this
		// response only, alongside the budget it belongs to.
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(budget + writeDeadlineSlack)); err != nil {
			h.logger.Warn("write deadline not extended", "route", route, "error", err)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	r = r.WithContext(ctx)
	h.logger.Info("http request", "route", route, "method", r.Method)
	spec.handle(h, w, r)
}

func (h *handler) isUIPath(requestPath string) bool {
	if requestPath == "/health" || requestPath == "/v1" || strings.HasPrefix(requestPath, "/v1/") {
		return false
	}
	return h.assets.available || requestPath == "/"
}

func (h *handler) validMutation(r *http.Request) bool {
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 || contentTypes[0] != "application/json" {
		return false
	}
	markers := r.Header.Values("X-Routevane-Request")
	if len(markers) != 1 || markers[0] != "1" {
		return false
	}
	origins := r.Header.Values("Origin")
	return len(origins) == 0 || len(origins) == 1 && origins[0] == h.origin
}

// settings answers with the preferences the process itself acts on. A browser
// keeps its own view preferences locally; these are not those.
func (h *handler) settings(w http.ResponseWriter, r *http.Request) {
	interval, err := h.backend.DefaultRefreshInterval(r.Context())
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refresh_interval": interval})
}

func (h *handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshInterval application.RefreshInterval `json:"refresh_interval"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.backend.SetDefaultRefreshInterval(r.Context(), request.RefreshInterval); err != nil {
		h.backendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refresh_interval": request.RefreshInterval})
}

func (h *handler) subscription(w http.ResponseWriter, r *http.Request, token string) {
	result, err := h.backend.Subscription(r.Context(), token)
	if err != nil {
		h.backendError(w, err)
		return
	}
	if result.Fallback {
		w.Header().Set("X-Routevane-Fallback", "previous")
	}
	writeArtifact(w, r, result)
}
func (h *handler) artifact(w http.ResponseWriter, r *http.Request, id string) {
	result, err := h.backend.Artifact(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	writeArtifact(w, r, result)
}
func (h *handler) snapshot(w http.ResponseWriter, r *http.Request, id string) {
	snapshot, err := h.backend.Snapshot(r.Context(), id)
	if err != nil {
		h.backendError(w, err)
		return
	}
	payload := struct {
		ID                string          `json:"id"`
		OutputID          string          `json:"output_id"`
		RoutingPlanHash   string          `json:"routing_plan_hash"`
		RoutingPlan       json.RawMessage `json:"routing_plan"`
		PolicyVersion     string          `json:"policy_version"`
		CatalogRevision   string          `json:"catalog_revision"`
		ObservationCutoff time.Time       `json:"observation_cutoff"`
		CreatedAt         time.Time       `json:"created_at"`
		Status            string          `json:"status"`
	}{snapshot.ID, snapshot.OutputID, snapshot.RoutingPlanHash, json.RawMessage(snapshot.RoutingPlanJSON), snapshot.PolicyVersion, snapshot.CatalogRevision, snapshot.ObservationCutoff, snapshot.CreatedAt, snapshot.Status}
	writeJSON(w, http.StatusOK, payload)
}

var statusTemplate = template.Must(template.New("status").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Routevane</title></head><body><main><h1>Routevane</h1><p>Local routing publication service is running.</p></main></body></html>`))

func (h *handler) status(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'none'; img-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = statusTemplate.Execute(w, nil)
}

func writeArtifact(w http.ResponseWriter, r *http.Request, result application.ArtifactPayload) {
	a := result.Artifact
	w.Header().Set("ETag", `"`+a.ArtifactHash+`"`)
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="routevane-`+a.OutputID+`-`+a.ArtifactHash+`.`+result.Descriptor.FileExtension+`"`)
	w.Header().Set("Cache-Control", "no-cache")
	// A published artifact is one immutable byte string, so the conditional
	// request, the validators and the modification time it is revalidated by
	// belong to the standard library rather than to a second parser here.
	http.ServeContent(w, r, "", a.ContentCreatedAt, bytes.NewReader(result.Payload))
}

func decodeEmpty(w http.ResponseWriter, r *http.Request) bool {
	var value map[string]json.RawMessage
	if !decodeJSON(w, r, &value) {
		return false
	}
	if value == nil || len(value) != 0 {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}
func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	return decodeJSONLimit(w, r, destination, maxJSONBytes)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, destination any, maximum int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maximum)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

// backendError maps an application error to a bounded response and records the
// reason locally. A loopback service that answers 422 with no local explanation
// is not operable, and the log stays on the operator's own machine.
func (h *handler) backendError(w http.ResponseWriter, err error) {
	h.logger.Warn("request failed", "error", err.Error())
	writeBackendError(w, err)
}

func writeBackendError(w http.ResponseWriter, err error) {
	// A batch of destinations is refused as a whole, so the refusal names the
	// one value that caused it. The value is the caller's own bounded text and
	// nothing of this process, which is why it may be repeated back.
	// A category or a list a profile still names cannot be deleted, and the
	// refusal names the profiles: they are the operator's own titles, and
	// finding them again would otherwise cost a second request against every
	// stored profile.
	var categoryInUse application.CategoryInUseError
	var listInUse application.ListInUseError
	var invalidDestination application.InvalidDestinationError
	switch {
	case errors.As(err, &categoryInUse):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "category in use",
			"profiles": categoryInUse.Profiles,
		})
	case errors.As(err, &listInUse):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "list in use",
			"profiles": listInUse.Profiles,
		})
	case errors.As(err, &invalidDestination):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid destination",
			"value": invalidDestination.Value,
		})
	case errors.Is(err, application.ErrSubscriptionRevoked):
		// A genuine link that was withdrawn is gone, not unknown: a subscriber
		// can stop asking rather than retry.
		writeCodedError(w, http.StatusGone, "subscription revoked", "subscription_revoked")
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, application.ErrCredentialRequired):
		// Automatic delivery for a deployer that takes a password needs one saved
		// first (ADR 0050). A stable code, because a screen acts on it.
		writeError(w, http.StatusConflict, "credential_required")
	case errors.Is(err, application.ErrAddressRejected):
		// The address of a connection broke the deployer's destination policy.
		// The field travels with the code so a form can say it beside the
		// field; the address itself is never repeated (ADR 0050).
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "address_rejected", "field": "address"})
	case errors.Is(err, application.ErrConnectionInvalid):
		// Another part of the connection does not suit the deployer, the same
		// meaning the deploy routes give this code.
		writeError(w, http.StatusUnprocessableEntity, "connection_invalid")
	case errors.Is(err, application.ErrDeviceAuthRejected):
		// The device refused the password offered for saving (ADR 0059). The
		// code is the one a refused send carries, with the same field.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "device_auth_rejected", "field": "credential"})
	case errors.Is(err, application.ErrCredentialInvalid):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "credential_invalid", "field": "credential"})
	case errors.Is(err, application.ErrSecretStoreUnavailable):
		// The platform keeps no credential store, which no retry changes
		// (ADR 0014).
		writeError(w, http.StatusServiceUnavailable, "secret_store_unavailable")
	case errors.Is(err, application.ErrArtifactUnavailable):
		writeError(w, http.StatusServiceUnavailable, "artifact unavailable")
	case errors.Is(err, application.ErrTargetChanged):
		writeError(w, http.StatusConflict, "output target changed")
	case errors.Is(err, application.ErrProfileArchived):
		writeCodedError(w, http.StatusConflict, "profile is archived", "profile_archived")
	case errors.Is(err, application.ErrProfileNotArchived):
		writeCodedError(w, http.StatusConflict, "profile is not archived", "profile_not_archived")
	case errors.Is(err, application.ErrSubscriptionMissing):
		writeCodedError(w, http.StatusConflict, "subscription not issued", "subscription_not_issued")
	case errors.Is(err, application.ErrRuleLimit), errors.Is(err, application.ErrPartialCoverage), errors.Is(err, application.ErrFormatMismatch), errors.Is(err, application.ErrPreflight), errors.Is(err, application.ErrSourceFailed), errors.Is(err, application.ErrSourceDegraded):
		details := application.ClassifyBuildFailure(err)
		body := map[string]any{
			"error":           "operation failed",
			"code":            details.Code,
			"projected_rules": details.ProjectedRules,
			"maximum_rules":   details.MaximumRules,
		}
		// Only a refusal for incomplete coverage names lists, and it names every
		// one, so the screen can send the operator to each.
		if len(details.UncoveredLists) > 0 {
			body["uncovered_lists"] = details.UncoveredLists
		}
		writeJSON(w, http.StatusUnprocessableEntity, body)
	default:
		writeError(w, http.StatusUnprocessableEntity, "operation failed")
	}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeNoContent answers a mutation that has nothing to say. The body is
// omitted rather than empty, which is what 204 means.
func writeNoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeCodedError adds the stable machine code a client branches on beside the
// message, in the same envelope a build refusal uses.
func writeCodedError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}
func securityHeaders(w http.ResponseWriter, digest string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	if digest == "" {
		digest = "unavailable"
	}
	w.Header().Set("X-Routevane-UI-Digest", digest)
}

func loopbackRemote(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil && ip.IsLoopback()
}
func unsafeEscapedPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(path, "%") || strings.Contains(path, "\\") || strings.Contains(path, "..")
}
