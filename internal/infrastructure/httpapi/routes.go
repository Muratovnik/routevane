// Routing lives in its own file because it is one contract with three parts —
// which path resolves to which name, which methods that name answers, and which
// handler owns it — and those parts are only correct while they stay together.
package httpapi

import (
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

// writeDeadlineSlack is the margin between a route's own budget and the write
// deadline, so the handler's context expires first and answers with its own
// error instead of having the connection cut underneath it.
const writeDeadlineSlack = 10 * time.Second

// routeUI and routeStatus are the two routes no request path names directly:
// the control surface answers whatever the API does not, and the root belongs
// to whichever of the two this build carries.
const (
	routeUI     = "ui"
	routeStatus = "status"
)

// routeSpec is everything the transport knows about one route: which path it
// answers on, which methods it answers, how long it may take, and which handler
// owns it.
//
// One table replaces three parallel switches over the same route names. That is
// not tidiness: a name that the path parser could produce but no handler owned
// used to answer an empty 200, and the only thing preventing it was that three
// places happened to agree.
type routeSpec struct {
	// path is the pattern net/http's router matches this route by. Identifiers
	// are named wildcards, so the handler reads them back by name instead of
	// counting segments.
	path string
	// methods are the methods this route answers, in the order the Allow header
	// should list them.
	methods []string
	// timeout overrides the default request budget. Zero means the default.
	timeout time.Duration
	handle  func(h *handler, w http.ResponseWriter, r *http.Request)
}

func (s routeSpec) allows(method string) bool {
	return slices.Contains(s.methods, method)
}

func (s routeSpec) allowHeader() string { return strings.Join(s.methods, ", ") }

var (
	readMethods  = []string{http.MethodGet}
	writeMethods = []string{http.MethodPost}
	// collectionMethods belong to a path that reads and creates through one
	// address: GET lists what is there, POST adds to it.
	collectionMethods = []string{http.MethodGet, http.MethodPost}
	uiMethods         = []string{http.MethodGet, http.MethodHead}
)

// routes owns every route this transport answers. A path that no entry here
// claims is a 404, so adding a path without an owner cannot ship as a silent
// success.
var routes = map[string]routeSpec{
	"config-transfer.export": {path: "/v1/config-transfer/export", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.exportConfigTransfer(w, r)
	}},
	"config-transfer.preview": {path: "/v1/config-transfer/preview", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.previewConfigTransfer(w, r)
	}},
	"config-transfer.apply": {path: "/v1/config-transfer/apply", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.applyConfigTransfer(w, r)
	}},
	// The control surface answers every path the API does not claim, which is
	// why its pattern is the catch-all. Whether a given path is one of its
	// pages is decided before the route is entered.
	routeUI: {path: "/", methods: uiMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.serveUI(w, r)
	}},
	routeStatus: {path: "/{$}", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.status(w, r)
	}},
	"health": {path: "/health", methods: readMethods, handle: func(_ *handler, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}},
	"services.collection": {path: "/v1/services", methods: collectionMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.createCustomService(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"services": h.backend.Services(), "service_details": h.backend.ServiceDetails(), "categories": h.backend.Categories()})
	}},
	"services.preview": {path: "/v1/services/{id}/preview", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.previewService(w, r, r.PathValue("id"))
	}},
	"services.update": {path: "/v1/services/{id}/update", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.updateCustomService(w, r, r.PathValue("id"))
	}},
	"services.remove": {path: "/v1/services/{id}/remove", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.removeService(w, r, r.PathValue("id"))
	}},
	"services.contents": {path: "/v1/services/{id}/contents", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.serviceContents(w, r, r.PathValue("id"))
	}},
	"services.refresh": {path: "/v1/services/{id}/refresh", methods: writeMethods, timeout: refreshRequestTimeout, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.refreshService(w, r, r.PathValue("id"))
	}},
	"services.sources": {path: "/v1/services/{id}/sources", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.addServiceSource(w, r, r.PathValue("id"))
	}},
	"services.sources.update": {path: "/v1/services/{id}/sources/{source}/update", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.updateServiceSource(w, r, r.PathValue("id"), r.PathValue("source"))
	}},
	"services.sources.remove": {path: "/v1/services/{id}/sources/{source}/remove", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.removeServiceSource(w, r, r.PathValue("id"), r.PathValue("source"))
	}},
	"services.domains": {path: "/v1/services/{id}/domains", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.setServiceValues(w, r, r.PathValue("id"))
	}},
	// Categories have no listing of their own: GET /v1/services already answers
	// with the merged categories beside the services they carry, and a second
	// address for the same document would be a second thing to keep in step.
	"categories.create": {path: "/v1/categories", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.createCategory(w, r)
	}},
	"categories.update": {path: "/v1/categories/{id}/update", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.updateCategory(w, r, r.PathValue("id"))
	}},
	"categories.remove": {path: "/v1/categories/{id}/remove", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.removeCategory(w, r, r.PathValue("id"))
	}},
	"targets": {path: "/v1/targets", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"targets": h.backend.Targets()})
	}},
	"export-formats": {path: "/v1/export-formats", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"formats": h.backend.ExportFormats()})
	}},
	"deployments.targets": {path: "/v1/deployments/targets", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"targets": h.backend.DeployableTargets()})
	}},
	"artifacts.deploy": {path: "/v1/artifacts/{id}/deploy", methods: writeMethods, timeout: deployRequestTimeout, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.deploy(w, r, r.PathValue("id"))
	}},
	"artifacts.get": {path: "/v1/artifacts/{id}", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.artifact(w, r, r.PathValue("id"))
	}},
	"devices.collection": {path: "/v1/devices", methods: collectionMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.createDevice(w, r)
			return
		}
		h.listDevices(w, r)
	}},
	"devices.update": {path: "/v1/devices/{id}/update", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.updateDevice(w, r, r.PathValue("id"))
	}},
	"devices.forget": {path: "/v1/devices/{id}/forget", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.forgetDevice(w, r, r.PathValue("id"))
	}},
	"devices.autodelivery": {path: "/v1/devices/{id}/auto-delivery", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.autoDelivery(w, r, r.PathValue("id"))
	}},
	"settings.get": {path: "/v1/settings", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.settings(w, r)
	}},
	"settings.update": {path: "/v1/settings/update", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.updateSettings(w, r)
	}},
	"lists.collection": {path: "/v1/lists", methods: collectionMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.createList(w, r)
			return
		}
		h.listLists(w, r)
	}},
	// The forecast is a POST because it carries a composition in its body, not
	// because it changes anything: it creates no list, output, attempt, or
	// artifact, and it answers on the default budget like every other screen.
	//
	// Its literal path is more specific than the identifier form below, so no
	// stored list can shadow it and "preview" is never read as a list id.
	"lists.preview": {path: "/v1/lists/preview", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.previewComposition(w, r)
	}},
	"lists.get": {path: "/v1/lists/{id}", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.getList(w, r, r.PathValue("id"))
	}},
	"lists.update": {path: "/v1/lists/{id}/update", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.updateList(w, r, r.PathValue("id"))
	}},
	"lists.archive": {path: "/v1/lists/{id}/archive", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.setListArchived(w, r, r.PathValue("id"), true)
	}},
	"lists.restore": {path: "/v1/lists/{id}/restore", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.setListArchived(w, r, r.PathValue("id"), false)
	}},
	"lists.schedule": {path: "/v1/lists/{id}/schedule", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.updateListSchedule(w, r, r.PathValue("id"))
	}},
	"lists.outputs": {path: "/v1/lists/{id}/outputs", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.addOutput(w, r, r.PathValue("id"))
	}},
	"lists.refresh": {path: "/v1/lists/{id}/refresh", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.refresh(w, r, r.PathValue("id"))
	}},
	"lists.export": {path: "/v1/lists/{id}/export", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.exportList(w, r, r.PathValue("id"))
	}},
	"outputs.get": {path: "/v1/outputs/{id}", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.getOutput(w, r, r.PathValue("id"))
	}},
	"outputs.build": {path: "/v1/outputs/{id}/build", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.build(w, r, r.PathValue("id"))
	}},
	"outputs.device": {path: "/v1/outputs/{id}/device", methods: writeMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.setOutputDevice(w, r, r.PathValue("id"))
	}},
	"subscriptions.get": {path: "/v1/subscriptions/{id}", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.subscription(w, r, r.PathValue("id"))
	}},
	"snapshots.get": {path: "/v1/snapshots/{id}", methods: readMethods, handle: func(h *handler, w http.ResponseWriter, r *http.Request) {
		h.snapshot(w, r, r.PathValue("id"))
	}},
}

// newRouteMux registers every route in the table on one net/http router.
// Matching, precedence between a literal and an identifier, and reading an
// identifier back out of a path all belong to the standard library; the table
// keeps only what the transport adds to them.
//
// The route name is the seam: production enters the shared request pipeline
// with it, and the ledger test resolves paths through this same router.
func newRouteMux(enter func(route string) http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	for route, spec := range routes {
		mux.Handle(spec.path, enter(route))
	}
	return mux
}

// canonicalPath is net/http's own path canonicalization. A path that differs
// from it is one the router would answer with a redirect, and this transport
// issues none.
func canonicalPath(requestPath string) string {
	cleaned := path.Clean(requestPath)
	if strings.HasSuffix(requestPath, "/") && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}
