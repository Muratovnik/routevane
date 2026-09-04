package application

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/domain"
)

const transferCodePrefix = "config_transfer_"

const (
	ConfigTransferVersion         = "config-transfer-v1.2"
	configTransferOmissionVersion = "config-transfer-v1.1"
	configTransferLegacyVersion   = "config-transfer-v1.0"
	// ConfigTransferMaxBytes covers the current bounded product maximum with
	// headroom: 200 routes can each carry at most 512 253-byte local domains,
	// while the other largest collections are 16,384 membership/verdict rows,
	// 128 custom lists with 64 domains each, 200 devices, and 800 outputs.
	// Preview, apply, export, and the browser all use this one limit.
	ConfigTransferMaxBytes = 64 << 20
	maxTransferRoutes      = 200
	maxTransferDevices     = 200
	maxTransferOutputs     = 800
	maxTransferRows        = 16_384 // memberships plus include/exclude verdict rows
)

// TransferError is safe to expose at the local HTTP boundary. Detail is
// deliberately bounded and never includes the imported document.
type TransferError struct {
	Code string
	Path string
}

func (e TransferError) Error() string {
	if e.Path == "" {
		return "configuration transfer: " + e.Code
	}
	return "configuration transfer: " + e.Code + " at " + e.Path
}

// NewTransferError keeps the HTTP and persistence boundaries on the one
// versioned public error namespace. Callers pass the stable suffix so internal
// decisions stay readable without risking one bare code escaping the API.
func NewTransferError(code, path string) TransferError {
	if !strings.HasPrefix(code, transferCodePrefix) {
		code = transferCodePrefix + code
	}
	if len(path) > 160 {
		path = path[:160]
	}
	return TransferError{Code: code, Path: path}
}

func transferError(code, path string) error {
	return NewTransferError(code, path)
}

type ConfigTransferCounts struct {
	CustomLists      uint `json:"custom_lists"`
	CustomCategories uint `json:"custom_categories"`
	CustomSources    uint `json:"custom_sources"`
	Routes           uint `json:"routes"`
	Devices          uint `json:"devices"`
	Outputs          uint `json:"outputs"`
}

type ConfigTransferWarning struct {
	Code string `json:"code"`
}

type ConfigTransferPreview struct {
	Digest   string                  `json:"digest"`
	CanApply bool                    `json:"can_apply"`
	Counts   ConfigTransferCounts    `json:"counts"`
	Warnings []ConfigTransferWarning `json:"warnings"`
}

type TransferSettings struct {
	RefreshInterval RefreshInterval `json:"refresh_interval"`
}
type TransferCustomService struct {
	Ref, Title string
	Domains    []string
}

func (v TransferCustomService) MarshalJSON() ([]byte, error) {
	type wire struct {
		Ref     string   `json:"ref"`
		Title   string   `json:"title"`
		Domains []string `json:"domains"`
	}
	return json.Marshal(wire(v))
}
func (v *TransferCustomService) UnmarshalJSON(b []byte) error {
	type wire struct {
		Ref     string   `json:"ref"`
		Title   string   `json:"title"`
		Domains []string `json:"domains"`
	}
	var w wire
	if err := strictUnmarshal(b, &w); err != nil {
		return err
	}
	*v = TransferCustomService(w)
	return nil
}

type TransferCustomCategory struct{ Ref, Title string }

func (v TransferCustomCategory) MarshalJSON() ([]byte, error) {
	type w struct {
		Ref   string `json:"ref"`
		Title string `json:"title"`
	}
	return json.Marshal(w(v))
}
func (v *TransferCustomCategory) UnmarshalJSON(b []byte) error {
	type w struct {
		Ref   string `json:"ref"`
		Title string `json:"title"`
	}
	var x w
	if err := strictUnmarshal(b, &x); err != nil {
		return err
	}
	*v = TransferCustomCategory(x)
	return nil
}

type TransferMembership struct {
	CategoryRef, ServiceRef string
	State                   MembershipState
}

func (v TransferMembership) MarshalJSON() ([]byte, error) {
	type w struct {
		CategoryRef string          `json:"category_ref"`
		ServiceRef  string          `json:"service_ref"`
		State       MembershipState `json:"state"`
	}
	return json.Marshal(w(v))
}
func (v *TransferMembership) UnmarshalJSON(b []byte) error {
	type w struct {
		CategoryRef string          `json:"category_ref"`
		ServiceRef  string          `json:"service_ref"`
		State       MembershipState `json:"state"`
	}
	var x w
	if err := strictUnmarshal(b, &x); err != nil {
		return err
	}
	*v = TransferMembership(x)
	return nil
}

type TransferRemoval struct {
	Kind RemovalKind
	ID   string
}

func (v TransferRemoval) MarshalJSON() ([]byte, error) {
	type w struct {
		Kind RemovalKind `json:"kind"`
		ID   string      `json:"id"`
	}
	return json.Marshal(w(v))
}
func (v *TransferRemoval) UnmarshalJSON(b []byte) error {
	type w struct {
		Kind RemovalKind `json:"kind"`
		ID   string      `json:"id"`
	}
	var x w
	if err := strictUnmarshal(b, &x); err != nil {
		return err
	}
	*v = TransferRemoval(x)
	return nil
}

type TransferCustomSource struct {
	Ref, URL string
	Format   domain.FeedFormat
}

func (v TransferCustomSource) MarshalJSON() ([]byte, error) {
	type w struct {
		Ref    string            `json:"ref"`
		URL    string            `json:"url"`
		Format domain.FeedFormat `json:"format"`
	}
	return json.Marshal(w(v))
}
func (v *TransferCustomSource) UnmarshalJSON(b []byte) error {
	type w struct {
		Ref    string            `json:"ref"`
		URL    string            `json:"url"`
		Format domain.FeedFormat `json:"format"`
	}
	var x w
	if err := strictUnmarshal(b, &x); err != nil {
		return err
	}
	*v = TransferCustomSource(x)
	return nil
}

type TransferTuning struct {
	ServiceRef         string
	DisabledSources    []string
	CustomSources      []TransferCustomSource
	Includes, Excludes []string
}

func (v TransferTuning) MarshalJSON() ([]byte, error) {
	type w struct {
		ServiceRef      string                 `json:"service_ref"`
		DisabledSources []string               `json:"disabled_sources"`
		CustomSources   []TransferCustomSource `json:"custom_sources"`
		Includes        []string               `json:"includes"`
		Excludes        []string               `json:"excludes"`
	}
	return json.Marshal(w(v))
}
func (v *TransferTuning) UnmarshalJSON(b []byte) error {
	type w struct {
		ServiceRef      string                 `json:"service_ref"`
		DisabledSources []string               `json:"disabled_sources"`
		CustomSources   []TransferCustomSource `json:"custom_sources"`
		Includes        []string               `json:"includes"`
		Excludes        []string               `json:"excludes"`
	}
	var x w
	if err := strictUnmarshal(b, &x); err != nil {
		return err
	}
	*v = TransferTuning(x)
	return nil
}

type TransferRoute struct {
	Ref, Name                        string
	Services, Categories, Exclusions []string
	Priority                         []string
	ServiceDomains                   map[string][]string
	RefreshInterval                  RefreshInterval
	Archived                         bool
}

func (v TransferRoute) MarshalJSON() ([]byte, error) {
	type w struct {
		Ref             string              `json:"ref"`
		Name            string              `json:"name"`
		Services        []string            `json:"services"`
		Categories      []string            `json:"categories"`
		Exclusions      []string            `json:"exclusions"`
		Priority        []string            `json:"priority"`
		ServiceDomains  map[string][]string `json:"service_domains"`
		RefreshInterval RefreshInterval     `json:"refresh_interval"`
		Archived        bool                `json:"archived"`
	}
	if v.ServiceDomains == nil {
		v.ServiceDomains = map[string][]string{}
	}
	return json.Marshal(w(v))
}
func (v *TransferRoute) UnmarshalJSON(b []byte) error {
	type w struct {
		Ref             string              `json:"ref"`
		Name            string              `json:"name"`
		Services        []string            `json:"services"`
		Categories      []string            `json:"categories"`
		Exclusions      []string            `json:"exclusions"`
		Priority        []string            `json:"priority"`
		ServiceDomains  map[string][]string `json:"service_domains"`
		RefreshInterval RefreshInterval     `json:"refresh_interval"`
		Archived        bool                `json:"archived"`
	}
	var x w
	if err := strictUnmarshal(b, &x); err != nil {
		return err
	}
	*v = TransferRoute(x)
	return nil
}

type TransferDevice struct{ Ref, TargetID, Name, Address, Account, Interface string }

func (v TransferDevice) MarshalJSON() ([]byte, error) {
	type w struct {
		Ref       string `json:"ref"`
		TargetID  string `json:"target_id"`
		Name      string `json:"name"`
		Address   string `json:"address"`
		Account   string `json:"account"`
		Interface string `json:"interface"`
	}
	return json.Marshal(w(v))
}
func (v *TransferDevice) UnmarshalJSON(b []byte) error {
	type w struct {
		Ref       string `json:"ref"`
		TargetID  string `json:"target_id"`
		Name      string `json:"name"`
		Address   string `json:"address"`
		Account   string `json:"account"`
		Interface string `json:"interface"`
	}
	var x w
	if err := strictUnmarshal(b, &x); err != nil {
		return err
	}
	*v = TransferDevice(x)
	return nil
}

type TransferOutput struct{ Ref, RouteRef, TargetID, DeviceRef string }

func (v TransferOutput) MarshalJSON() ([]byte, error) {
	type w struct {
		Ref       string `json:"ref"`
		RouteRef  string `json:"route_ref"`
		TargetID  string `json:"target_id"`
		DeviceRef string `json:"device_ref,omitempty"`
	}
	return json.Marshal(w(v))
}
func (v *TransferOutput) UnmarshalJSON(b []byte) error {
	type w struct {
		Ref       string `json:"ref"`
		RouteRef  string `json:"route_ref"`
		TargetID  string `json:"target_id"`
		DeviceRef string `json:"device_ref,omitempty"`
	}
	var x w
	if err := strictUnmarshal(b, &x); err != nil {
		return err
	}
	*v = TransferOutput(x)
	return nil
}

type ConfigTransferDocument struct {
	Version              string                   `json:"version"`
	Settings             TransferSettings         `json:"settings"`
	CustomServices       []TransferCustomService  `json:"custom_services"`
	CustomCategories     []TransferCustomCategory `json:"custom_categories"`
	Memberships          []TransferMembership     `json:"memberships"`
	Removals             []TransferRemoval        `json:"removals"`
	Tunings              []TransferTuning         `json:"tunings"`
	Routes               []TransferRoute          `json:"routes"`
	Devices              []TransferDevice         `json:"devices"`
	Outputs              []TransferOutput         `json:"outputs"`
	OmittedCustomSources uint                     `json:"omitted_custom_sources"`
}

type ConfigTransferRepository interface {
	ExportConfigTransfer(context.Context) (ConfigTransferDocument, error)
	ApplyConfigTransfer(context.Context, ConfigTransferApply) error
}

type ConfigTransferApply struct {
	Document                                                                             ConfigTransferDocument
	AppliedAt                                                                            time.Time
	CustomServiceIDs, CustomCategoryIDs, CustomSourceIDs, RouteIDs, DeviceIDs, OutputIDs map[string]string
	Outputs                                                                              map[string]Output
}

func (s *PublicationService) ExportConfigTransfer(ctx context.Context) ([]byte, error) {
	repo, ok := s.config.Store.(ConfigTransferRepository)
	if !ok {
		return nil, transferError("storage_failed", "")
	}
	doc, err := repo.ExportConfigTransfer(ctx)
	if err != nil {
		return nil, fmt.Errorf("export configuration: %w", err)
	}
	doc.Version = ConfigTransferVersion
	omitCustomSources(&doc)
	canonicalizeTransferReferences(&doc)
	if err := s.validateTransferShape(&doc, nil); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(doc)
	if err != nil {
		return nil, transferError("invalid_shape", "")
	}
	if err := validateConfigTransferSize(len(payload)); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *PublicationService) PreviewConfigTransfer(payload []byte, validateDevice func(TransferDevice) error) (ConfigTransferPreview, ConfigTransferDocument, error) {
	doc, err := s.validateTransfer(payload, validateDevice)
	if err != nil {
		return ConfigTransferPreview{}, ConfigTransferDocument{}, err
	}
	hash := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	return transferPreview(doc, digest), doc, nil
}

func (s *PublicationService) ApplyConfigTransfer(ctx context.Context, previewDigest string, payload []byte, validateDevice func(TransferDevice) error) (ConfigTransferCounts, error) {
	if previewDigest == "" {
		return ConfigTransferCounts{}, transferError("preview_required", "preview_digest")
	}
	doc, err := s.validateTransfer(payload, validateDevice)
	if err != nil {
		return ConfigTransferCounts{}, err
	}
	hash := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	if !validTransferDigest(previewDigest) || digest != previewDigest {
		return ConfigTransferCounts{}, transferError("preview_mismatch", "preview_digest")
	}
	repo, ok := s.config.Store.(ConfigTransferRepository)
	if !ok {
		return ConfigTransferCounts{}, transferError("storage_failed", "")
	}
	apply, err := s.prepareApply(doc)
	if err != nil {
		return ConfigTransferCounts{}, err
	}
	registries, err := s.prepareTransferRegistries(apply)
	if err != nil {
		return ConfigTransferCounts{}, err
	}
	if err := repo.ApplyConfigTransfer(ctx, apply); err != nil {
		var te TransferError
		if errors.As(err, &te) {
			return ConfigTransferCounts{}, err
		}
		return ConfigTransferCounts{}, fmt.Errorf("%w: apply configuration", transferError("storage_failed", ""))
	}
	// Registry state was prepared from the already-validated document before
	// the SQLite transaction. This swap cannot fail after commit, so a caller
	// never receives a failure for a configuration that became durable.
	s.installTransferRegistries(registries)
	return transferCounts(doc), nil
}

type transferRegistries struct {
	custom     map[string]CustomService
	tuning     map[string]ServiceTuning
	categories map[string]CustomCategory
	membership map[string]map[string]MembershipState
	removed    map[RemovalKind]map[string]struct{}
}

func (s *PublicationService) prepareTransferRegistries(a ConfigTransferApply) (transferRegistries, error) {
	now := a.AppliedAt.UTC()
	state := transferRegistries{
		custom: map[string]CustomService{}, tuning: map[string]ServiceTuning{}, categories: map[string]CustomCategory{},
		membership: map[string]map[string]MembershipState{}, removed: emptyRemovalIndex(),
	}
	serviceID := func(ref string) string {
		if id := a.CustomServiceIDs[ref]; id != "" {
			return id
		}
		return ref
	}
	categoryID := func(ref string) string {
		if id := a.CustomCategoryIDs[ref]; id != "" {
			return id
		}
		return ref
	}
	for _, item := range a.Document.CustomServices {
		value, err := normalizedCustomService(CustomService{ID: a.CustomServiceIDs[item.Ref], Title: item.Title, Domains: item.Domains, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return state, transferError("invalid_shape", "custom_services")
		}
		state.custom[value.ID] = value
	}
	for _, item := range a.Document.CustomCategories {
		value, err := normalizedCustomCategory(CustomCategory{ID: a.CustomCategoryIDs[item.Ref], Title: item.Title, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return state, transferError("invalid_shape", "custom_categories")
		}
		state.categories[value.ID] = value
	}
	for _, item := range a.Document.Memberships {
		category, service := categoryID(item.CategoryRef), serviceID(item.ServiceRef)
		if state.membership[category] == nil {
			state.membership[category] = map[string]MembershipState{}
		}
		state.membership[category][service] = item.State
	}
	for _, item := range a.Document.Removals {
		state.removed[item.Kind][item.ID] = struct{}{}
	}
	for _, item := range a.Document.Tunings {
		service := serviceID(item.ServiceRef)
		tuning := ServiceTuning{DisabledSources: append([]string(nil), item.DisabledSources...), Includes: append([]string(nil), item.Includes...), Excludes: append([]string(nil), item.Excludes...)}
		for _, source := range item.CustomSources {
			tuning.CustomSources = append(tuning.CustomSources, CustomSource{ID: a.CustomSourceIDs[source.Ref], ServiceID: service, URL: source.URL, Format: source.Format, CreatedAt: now, UpdatedAt: now})
		}
		value, err := normalizedServiceTuning(service, tuning)
		if err != nil {
			return state, transferError("invalid_shape", "tunings")
		}
		state.tuning[service] = value
	}
	return state, nil
}

func (s *PublicationService) installTransferRegistries(state transferRegistries) {
	s.registryMu.Lock()
	defer s.registryMu.Unlock()
	s.custom.mu.Lock()
	s.tuning.mu.Lock()
	s.overlay.mu.Lock()
	s.custom.services = state.custom
	s.tuning.byID = state.tuning
	s.overlay.custom, s.overlay.membership, s.overlay.removed = state.categories, state.membership, state.removed
	s.overlay.mu.Unlock()
	s.tuning.mu.Unlock()
	s.custom.mu.Unlock()
}

func validTransferDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (s *PublicationService) validateTransfer(payload []byte, validateDevice func(TransferDevice) error) (ConfigTransferDocument, error) {
	if err := validateConfigTransferSize(len(payload)); err != nil {
		return ConfigTransferDocument{}, err
	}
	if !utf8.Valid(payload) {
		return ConfigTransferDocument{}, transferError("invalid_json", "")
	}
	if err := RejectDuplicateJSONKeys(payload); err != nil {
		return ConfigTransferDocument{}, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(payload, &top); err != nil || top == nil {
		return ConfigTransferDocument{}, transferError("invalid_json", "")
	}
	var version string
	if err := json.Unmarshal(top["version"], &version); err != nil {
		return ConfigTransferDocument{}, transferError("invalid_json", "version")
	}
	_, hasOmissionMetadata := top["omitted_custom_sources"]
	if version != ConfigTransferVersion && version != configTransferOmissionVersion && version != configTransferLegacyVersion {
		return ConfigTransferDocument{}, transferError("unsupported_version", "version")
	}
	if version == configTransferLegacyVersion && hasOmissionMetadata {
		return ConfigTransferDocument{}, transferError("unsupported_version", "version")
	}
	if version != configTransferLegacyVersion && !hasOmissionMetadata {
		return ConfigTransferDocument{}, transferError("invalid_shape", "omitted_custom_sources")
	}
	if version != configTransferLegacyVersion && strings.TrimSpace(string(top["omitted_custom_sources"])) == "null" {
		return ConfigTransferDocument{}, transferError("invalid_shape", "omitted_custom_sources")
	}
	var doc ConfigTransferDocument
	if err := strictUnmarshal(payload, &doc); err != nil {
		return ConfigTransferDocument{}, transferError("invalid_json", "")
	}
	canonicalizeTransfer(&doc)
	if err := s.validateTransferShape(&doc, validateDevice); err != nil {
		return ConfigTransferDocument{}, err
	}
	return doc, nil
}

func validateConfigTransferSize(size int) error {
	if size > ConfigTransferMaxBytes {
		return transferError("too_large", "")
	}
	return nil
}

// omitCustomSources applies the portable-file secrecy boundary independently
// of a repository implementation. An arbitrary feed URL may carry a credential
// in its host, path, or query, so no URL heuristic can make one exportable.
// References that only disable an omitted source are omitted with it; catalog
// source choices remain intact.
func omitCustomSources(d *ConfigTransferDocument) {
	omitted := uint(0)
	for i := range d.Tunings {
		t := &d.Tunings[i]
		custom := make(map[string]struct{}, len(t.CustomSources))
		for _, source := range t.CustomSources {
			custom[source.Ref] = struct{}{}
		}
		omitted += uint(len(t.CustomSources))
		t.CustomSources = nil
		kept := t.DisabledSources[:0]
		for _, sourceID := range t.DisabledSources {
			if _, isCustom := custom[sourceID]; !isCustom {
				kept = append(kept, sourceID)
			}
		}
		t.DisabledSources = kept
	}
	d.OmittedCustomSources += omitted
}

func strictUnmarshal(payload []byte, destination any) error {
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

// RejectDuplicateJSONKeys protects the transfer document at the application
// boundary, so HTTP and non-HTTP callers get the same strict semantics.
func RejectDuplicateJSONKeys(payload []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	var walk func(string) error
	walk = func(path string) error {
		tok, err := dec.Token()
		if err != nil {
			return transferError("invalid_json", path)
		}
		switch d := tok.(type) {
		case json.Delim:
			switch d {
			case '{':
				seen := map[string]struct{}{}
				for dec.More() {
					k, err := dec.Token()
					if err != nil {
						return transferError("invalid_json", path)
					}
					key, ok := k.(string)
					if !ok {
						return transferError("invalid_json", path)
					}
					if _, ok := seen[key]; ok {
						return transferError("duplicate_key", path+"/"+key)
					}
					seen[key] = struct{}{}
					if err := walk(path + "/" + key); err != nil {
						return err
					}
				}
				_, err = dec.Token()
				return err
			case '[':
				i := 0
				for dec.More() {
					if err := walk(fmt.Sprintf("%s/%d", path, i)); err != nil {
						return err
					}
					i++
				}
				_, err = dec.Token()
				return err
			default:
				return transferError("invalid_json", path)
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return err
	}
	if dec.More() {
		return transferError("invalid_json", "")
	}
	return nil
}

func canonicalizeTransfer(d *ConfigTransferDocument) {
	nilSlices(d)
	slices.SortFunc(d.CustomServices, func(a, b TransferCustomService) int { return cmp.Compare(a.Ref, b.Ref) })
	for i := range d.CustomServices {
		d.CustomServices[i].Domains = domain.StableStrings(d.CustomServices[i].Domains)
	}
	slices.SortFunc(d.CustomCategories, func(a, b TransferCustomCategory) int { return cmp.Compare(a.Ref, b.Ref) })
	slices.SortFunc(d.Memberships, func(a, b TransferMembership) int {
		return cmp.Compare(a.CategoryRef+"\x00"+a.ServiceRef, b.CategoryRef+"\x00"+b.ServiceRef)
	})
	slices.SortFunc(d.Removals, func(a, b TransferRemoval) int {
		return cmp.Compare(string(a.Kind)+"\x00"+a.ID, string(b.Kind)+"\x00"+b.ID)
	})
	for i := range d.Tunings {
		t := &d.Tunings[i]
		t.DisabledSources = domain.StableStrings(t.DisabledSources)
		t.Includes = domain.StableStrings(t.Includes)
		t.Excludes = domain.StableStrings(t.Excludes)
		slices.SortFunc(t.CustomSources, func(a, b TransferCustomSource) int { return cmp.Compare(a.Ref, b.Ref) })
	}
	slices.SortFunc(d.Tunings, func(a, b TransferTuning) int { return cmp.Compare(a.ServiceRef, b.ServiceRef) })
	for i := range d.Routes {
		r := &d.Routes[i]
		r.Services = domain.StableStrings(r.Services)
		r.Categories = domain.StableStrings(r.Categories)
		r.Exclusions = domain.StableStrings(r.Exclusions)
		if r.Priority == nil {
			r.Priority = []string{}
		}
		if r.ServiceDomains == nil {
			r.ServiceDomains = map[string][]string{}
		}
		for k, v := range r.ServiceDomains {
			r.ServiceDomains[k] = domain.StableStrings(v)
		}
	}
	slices.SortFunc(d.Routes, func(a, b TransferRoute) int { return cmp.Compare(a.Ref, b.Ref) })
	slices.SortFunc(d.Devices, func(a, b TransferDevice) int { return cmp.Compare(a.Ref, b.Ref) })
	slices.SortFunc(d.Outputs, func(a, b TransferOutput) int { return cmp.Compare(a.Ref, b.Ref) })
}

// canonicalizeTransferReferences removes source-local persistent identities
// from an exported document. The stable references it assigns are only edges
// inside this document; the apply path replaces all of them with fresh random
// SQLite identities. Catalog references intentionally stay unchanged.
func canonicalizeTransferReferences(d *ConfigTransferDocument) {
	canonicalizeTransfer(d)
	serviceRefs := make(map[string]string, len(d.CustomServices))
	for i := range d.CustomServices {
		old := d.CustomServices[i].Ref
		ref := fmt.Sprintf("custom-service-%d", i+1)
		serviceRefs[old] = ref
		d.CustomServices[i].Ref = ref
	}
	categoryRefs := make(map[string]string, len(d.CustomCategories))
	for i := range d.CustomCategories {
		old := d.CustomCategories[i].Ref
		ref := fmt.Sprintf("custom-category-%d", i+1)
		categoryRefs[old] = ref
		d.CustomCategories[i].Ref = ref
	}
	n := 0
	for i := range d.Tunings {
		for j := range d.Tunings[i].CustomSources {
			n++
			d.Tunings[i].CustomSources[j].Ref = fmt.Sprintf("custom-source-%d", n)
		}
	}
	routeRefs := make(map[string]string, len(d.Routes))
	for i := range d.Routes {
		old := d.Routes[i].Ref
		ref := fmt.Sprintf("route-%d", i+1)
		routeRefs[old] = ref
		d.Routes[i].Ref = ref
	}
	deviceRefs := make(map[string]string, len(d.Devices))
	for i := range d.Devices {
		old := d.Devices[i].Ref
		ref := fmt.Sprintf("device-%d", i+1)
		deviceRefs[old] = ref
		d.Devices[i].Ref = ref
	}
	for i := range d.Outputs {
		d.Outputs[i].Ref = fmt.Sprintf("output-%d", i+1)
	}
	service := func(id string) string {
		if ref := serviceRefs[id]; ref != "" {
			return ref
		}
		return id
	}
	category := func(id string) string {
		if ref := categoryRefs[id]; ref != "" {
			return ref
		}
		return id
	}
	for i := range d.Memberships {
		d.Memberships[i].CategoryRef = category(d.Memberships[i].CategoryRef)
		d.Memberships[i].ServiceRef = service(d.Memberships[i].ServiceRef)
	}
	for i := range d.Tunings {
		d.Tunings[i].ServiceRef = service(d.Tunings[i].ServiceRef)
	}
	for i := range d.Routes {
		route := &d.Routes[i]
		for j := range route.Services {
			route.Services[j] = service(route.Services[j])
		}
		for j := range route.Categories {
			route.Categories[j] = category(route.Categories[j])
		}
		for j := range route.Exclusions {
			route.Exclusions[j] = service(route.Exclusions[j])
		}
		for j := range route.Priority {
			route.Priority[j] = service(route.Priority[j])
		}
		domains := make(map[string][]string, len(route.ServiceDomains))
		for id, values := range route.ServiceDomains {
			domains[service(id)] = values
		}
		route.ServiceDomains = domains
	}
	for i := range d.Outputs {
		output := &d.Outputs[i]
		output.RouteRef = routeRefs[output.RouteRef]
		if output.DeviceRef != "" {
			output.DeviceRef = deviceRefs[output.DeviceRef]
		}
	}
	canonicalizeTransfer(d)
}

func nilSlices(d *ConfigTransferDocument) {
	if d.CustomServices == nil {
		d.CustomServices = []TransferCustomService{}
	}
	if d.CustomCategories == nil {
		d.CustomCategories = []TransferCustomCategory{}
	}
	if d.Memberships == nil {
		d.Memberships = []TransferMembership{}
	}
	if d.Removals == nil {
		d.Removals = []TransferRemoval{}
	}
	if d.Tunings == nil {
		d.Tunings = []TransferTuning{}
	}
	if d.Routes == nil {
		d.Routes = []TransferRoute{}
	}
	if d.Devices == nil {
		d.Devices = []TransferDevice{}
	}
	if d.Outputs == nil {
		d.Outputs = []TransferOutput{}
	}
	for i := range d.Tunings {
		t := &d.Tunings[i]
		if t.DisabledSources == nil {
			t.DisabledSources = []string{}
		}
		if t.CustomSources == nil {
			t.CustomSources = []TransferCustomSource{}
		}
		if t.Includes == nil {
			t.Includes = []string{}
		}
		if t.Excludes == nil {
			t.Excludes = []string{}
		}
	}
}

func transferCounts(d ConfigTransferDocument) ConfigTransferCounts {
	n := uint(0)
	for _, t := range d.Tunings {
		n += uint(len(t.CustomSources))
	}
	return ConfigTransferCounts{uint(len(d.CustomServices)), uint(len(d.CustomCategories)), n, uint(len(d.Routes)), uint(len(d.Devices)), uint(len(d.Outputs))}
}
func transferPreview(d ConfigTransferDocument, digest string) ConfigTransferPreview {
	warnings := []ConfigTransferWarning{}
	if d.OmittedCustomSources > 0 {
		warnings = append(warnings, ConfigTransferWarning{"custom_sources_require_recreation"})
	}
	if len(d.Devices) > 0 {
		warnings = append(warnings, ConfigTransferWarning{"devices_require_credentials"}, ConfigTransferWarning{"automatic_delivery_disabled"})
	}
	if len(d.Outputs) > 0 {
		warnings = append(warnings, ConfigTransferWarning{"outputs_require_publication"})
	}
	return ConfigTransferPreview{digest, true, transferCounts(d), warnings}
}

func (s *PublicationService) validateTransferShape(d *ConfigTransferDocument, validateDevice func(TransferDevice) error) error {
	if len(d.CustomServices) > maxCustomServices || len(d.CustomCategories) > maxCustomCategories || len(d.Routes) > maxTransferRoutes || len(d.Devices) > maxTransferDevices || len(d.Outputs) > maxTransferOutputs {
		return transferError("limit_exceeded", "")
	}
	if d.OmittedCustomSources > maxCustomSourcesTotal {
		return transferError("limit_exceeded", "omitted_custom_sources")
	}
	if !d.Settings.RefreshInterval.validDefault() {
		return transferError("invalid_shape", "settings/refresh_interval")
	}
	services := map[string]bool{}
	for id := range s.config.Definitions {
		if _, local := s.config.LocalServiceIDs[id]; !local {
			services[id] = true
		}
	}
	categories := map[string]bool{}
	localCategories := map[string]bool{}
	for id, category := range s.config.Categories {
		portable := true
		for _, serviceID := range category.Services {
			if _, local := s.config.LocalServiceIDs[serviceID]; local {
				portable = false
				break
			}
		}
		if portable {
			categories[id] = true
		} else {
			localCategories[id] = true
		}
	}
	seen := map[string]bool{}
	for i := range d.CustomServices {
		v := &d.CustomServices[i]
		p := fmt.Sprintf("custom_services/%d", i)
		if seen[v.Ref] {
			return transferError("duplicate_key", p+"/ref")
		}
		seen[v.Ref] = true
		if !strings.HasPrefix(v.Ref, customServiceIDPrefix) {
			return transferError("invalid_shape", p+"/ref")
		}
		title, domains, err := validCustomServiceInput(v.Title, v.Domains)
		if err != nil {
			return transferError("invalid_shape", p)
		}
		v.Title, v.Domains = title, domains
		services[v.Ref] = true
	}
	seen = map[string]bool{}
	if len(d.Memberships) > maxTransferRows {
		return transferError("limit_exceeded", "memberships")
	}
	for i := range d.CustomCategories {
		v := &d.CustomCategories[i]
		p := fmt.Sprintf("custom_categories/%d", i)
		if seen[v.Ref] {
			return transferError("duplicate_key", p+"/ref")
		}
		seen[v.Ref] = true
		title, ok := validCategoryTitle(v.Title)
		if !strings.HasPrefix(v.Ref, CustomCategoryIDPrefix) || !ok {
			return transferError("invalid_shape", p)
		}
		v.Title = title
		categories[v.Ref] = true
	}
	seenRemovals := map[string]bool{}
	for i, v := range d.Removals {
		key := string(v.Kind) + "\x00" + v.ID
		if seenRemovals[key] {
			return transferError("duplicate_key", fmt.Sprintf("removals/%d", i))
		}
		seenRemovals[key] = true
		if _, local := s.config.LocalServiceIDs[v.ID]; local || localCategories[v.ID] {
			return transferError("local_catalog_dependency", fmt.Sprintf("removals/%d/id", i))
		}
		if v.Kind != RemovalCategory && v.Kind != RemovalService {
			return transferError("invalid_shape", fmt.Sprintf("removals/%d/kind", i))
		}
		known := services[v.ID]
		if v.Kind == RemovalCategory {
			known = categories[v.ID]
		}
		if !known || strings.HasPrefix(v.ID, "custom-") {
			return transferError("catalog_reference_missing", fmt.Sprintf("removals/%d/id", i))
		}
	}
	seen = map[string]bool{}
	for i, v := range d.Memberships {
		p := fmt.Sprintf("memberships/%d", i)
		k := v.CategoryRef + "\x00" + v.ServiceRef
		if seen[k] {
			return transferError("duplicate_key", p)
		}
		seen[k] = true
		if _, local := s.config.LocalServiceIDs[v.ServiceRef]; local || localCategories[v.CategoryRef] {
			return transferError("local_catalog_dependency", p)
		}
		if !categories[v.CategoryRef] || !services[v.ServiceRef] {
			return transferError("invalid_reference", p)
		}
		if v.State != MembershipAdded && v.State != MembershipRemoved {
			return transferError("invalid_shape", p+"/state")
		}
		if strings.HasPrefix(v.CategoryRef, "custom-") && v.State == MembershipRemoved {
			return transferError("invalid_shape", p+"/state")
		}
	}
	seen = map[string]bool{}
	verdictTotal := 0
	for i, t := range d.Tunings {
		p := fmt.Sprintf("tunings/%d", i)
		if seen[t.ServiceRef] {
			return transferError("duplicate_key", p+"/service_ref")
		}
		seen[t.ServiceRef] = true
		if _, local := s.config.LocalServiceIDs[t.ServiceRef]; local {
			return transferError("local_catalog_dependency", p+"/service_ref")
		}
		if !services[t.ServiceRef] {
			return transferError("catalog_reference_missing", p+"/service_ref")
		}
		if len(t.CustomSources) > 0 {
			return transferError("secret_material", p+"/custom_sources")
		}
		verdictTotal += len(t.Includes) + len(t.Excludes)
		if len(t.DisabledSources) > maxCustomSourcesTotal || len(t.Includes)+len(t.Excludes) > maxServiceVerdicts || len(d.Memberships)+verdictTotal > maxTransferRows {
			return transferError("limit_exceeded", p)
		}
		definition, _ := s.baseDefinition(t.ServiceRef)
		available := map[string]bool{}
		for _, src := range definition.Sources {
			available[src.ID] = true
		}
		for _, id := range t.DisabledSources {
			if !available[id] {
				return transferError("source_missing", p+"/disabled_sources")
			}
		}
		if _, err := normalizedServiceTuning(t.ServiceRef, ServiceTuning{DisabledSources: t.DisabledSources, Includes: t.Includes, Excludes: t.Excludes}); err != nil {
			return transferError("invalid_shape", p)
		}
	}
	effectiveServices, effectiveCategories, members := s.transferCompositionCatalog(*d, services, categories)
	seen = map[string]bool{}
	for i := range d.Routes {
		r := &d.Routes[i]
		p := fmt.Sprintf("routes/%d", i)
		if seen[r.Ref] {
			return transferError("duplicate_key", p+"/ref")
		}
		seen[r.Ref] = true
		if !validTransferReference(r.Ref, "route") {
			return transferError("invalid_shape", p+"/ref")
		}
		name, ok := validListName(r.Name)
		if !ok {
			return transferError("invalid_shape", p+"/name")
		}
		r.Name = name
		if !r.RefreshInterval.valid() {
			return transferError("invalid_shape", p+"/refresh_interval")
		}
		serviceReferences := append(append([]string{}, r.Services...), r.Exclusions...)
		serviceReferences = append(serviceReferences, r.Priority...)
		for _, id := range serviceReferences {
			if _, local := s.config.LocalServiceIDs[id]; local {
				return transferError("local_catalog_dependency", p)
			}
			if !services[id] {
				return transferError("catalog_reference_missing", p)
			}
		}
		for _, id := range r.Categories {
			if localCategories[id] {
				return transferError("local_catalog_dependency", p)
			}
			if !categories[id] {
				return transferError("catalog_reference_missing", p)
			}
		}
		composition := ListComposition{
			Services: r.Services, Categories: r.Categories,
			Exclusions: r.Exclusions, ServiceDomains: r.ServiceDomains,
			Priority: r.Priority,
		}
		validated, err := validTransferComposition(composition, effectiveServices, effectiveCategories, members)
		if err != nil {
			return transferError("invalid_reference", p)
		}
		r.Services, r.Categories, r.Exclusions, r.ServiceDomains, r.Priority = validated.Services, validated.Categories, validated.Exclusions, validated.ServiceDomains, validated.Priority
	}
	seen = map[string]bool{}
	for i, v := range d.Devices {
		p := fmt.Sprintf("devices/%d", i)
		if seen[v.Ref] {
			return transferError("duplicate_key", p+"/ref")
		}
		seen[v.Ref] = true
		if !validTransferReference(v.Ref, "device") {
			return transferError("invalid_shape", p+"/ref")
		}
		if _, ok := s.config.Targets[v.TargetID]; !ok {
			return transferError("target_missing", p+"/target_id")
		}
		if validateDevice != nil && validateDevice(v) != nil {
			return transferError("connection_invalid", p)
		}
	}
	seen = map[string]bool{}
	pairs := map[string]bool{}
	for i, v := range d.Outputs {
		p := fmt.Sprintf("outputs/%d", i)
		if seen[v.Ref] {
			return transferError("duplicate_key", p+"/ref")
		}
		seen[v.Ref] = true
		if !validTransferReference(v.Ref, "output") {
			return transferError("invalid_shape", p+"/ref")
		}
		if !validTransferReference(v.RouteRef, "route") {
			return transferError("invalid_shape", p+"/route_ref")
		}
		if !containsRoute(d.Routes, v.RouteRef) {
			return transferError("invalid_reference", p+"/route_ref")
		}
		target, ok := s.config.Targets[v.TargetID]
		if !ok {
			return transferError("target_missing", p+"/target_id")
		}
		_ = target
		if v.DeviceRef != "" {
			if !validTransferReference(v.DeviceRef, "device") {
				return transferError("invalid_shape", p+"/device_ref")
			}
			dev, ok := findDevice(d.Devices, v.DeviceRef)
			if !ok || dev.TargetID != v.TargetID {
				return transferError("invalid_reference", p+"/device_ref")
			}
		}
		pair := v.RouteRef + "\x00" + v.TargetID
		if pairs[pair] {
			return transferError("duplicate_key", p)
		}
		pairs[pair] = true
	}
	return nil
}

// validTransferReference accepts only the document-local identifiers emitted
// by this format version. Keeping these out of the persistent-id grammar
// makes it impossible to bind an imported output to a destination row by
// accident, and makes an absent output binding unambiguous.
func validTransferReference(value, prefix string) bool {
	index, ok := strings.CutPrefix(value, prefix+"-")
	if !ok || index == "" || index[0] == '0' {
		return false
	}
	for _, r := range index {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// transferCompositionCatalog reproduces the post-apply catalog view while the
// transfer document is still in memory. The SQLite transaction replaces the
// overlay wholesale, so composition validation must use that prospective
// overlay rather than the destination's current registries.
func (s *PublicationService) transferCompositionCatalog(d ConfigTransferDocument, services, categories map[string]bool) (map[string]bool, map[string]bool, map[string]map[string]MembershipState) {
	effectiveServices := make(map[string]bool, len(services))
	for id := range services {
		effectiveServices[id] = true
	}
	effectiveCategories := make(map[string]bool, len(categories))
	for id := range categories {
		effectiveCategories[id] = true
	}
	for _, removal := range d.Removals {
		if removal.Kind == RemovalService {
			delete(effectiveServices, removal.ID)
		} else if removal.Kind == RemovalCategory {
			delete(effectiveCategories, removal.ID)
		}
	}
	members := make(map[string]map[string]MembershipState, len(effectiveCategories))
	for categoryID := range effectiveCategories {
		members[categoryID] = map[string]MembershipState{}
		if base, ok := s.config.Categories[categoryID]; ok {
			for _, serviceID := range base.Services {
				members[categoryID][serviceID] = MembershipAdded
			}
		}
	}
	for _, membership := range d.Memberships {
		if members[membership.CategoryRef] != nil {
			members[membership.CategoryRef][membership.ServiceRef] = membership.State
		}
	}
	return effectiveServices, effectiveCategories, members
}

// validTransferComposition applies the same rules as validComposition to the
// catalog that the transfer will install. In particular, a category is not a
// substitute for a service unless its effective memberships resolve to one.
func validTransferComposition(c ListComposition, services, categories map[string]bool, members map[string]map[string]MembershipState) (ListComposition, error) {
	serviceIDs := domain.StableStrings(c.Services)
	categoryIDs := domain.StableStrings(c.Categories)
	exclusions := domain.StableStrings(c.Exclusions)
	if len(serviceIDs) > maxCompositionItems || len(categoryIDs) > maxCompositionItems || len(exclusions) > maxCompositionItems {
		return ListComposition{}, errors.New("limit")
	}
	for _, id := range serviceIDs {
		if domain.ValidateSlug(id) != nil || !services[id] {
			return ListComposition{}, errors.New("service")
		}
	}
	for _, id := range categoryIDs {
		if domain.ValidateSlug(id) != nil || !categories[id] {
			return ListComposition{}, errors.New("category")
		}
	}
	named := make(map[string]struct{}, len(serviceIDs))
	for _, id := range serviceIDs {
		named[id] = struct{}{}
	}
	for _, id := range exclusions {
		if domain.ValidateSlug(id) != nil || !services[id] {
			return ListComposition{}, errors.New("exclusion")
		}
		if _, both := named[id]; both {
			return ListComposition{}, errors.New("contradiction")
		}
	}
	resolved := append([]string(nil), serviceIDs...)
	for _, categoryID := range categoryIDs {
		for serviceID, state := range members[categoryID] {
			if state == MembershipAdded && services[serviceID] {
				resolved = append(resolved, serviceID)
			}
		}
	}
	excluded := make(map[string]struct{}, len(exclusions))
	for _, id := range exclusions {
		excluded[id] = struct{}{}
	}
	withoutExcluded := resolved[:0]
	for _, id := range resolved {
		if _, dropped := excluded[id]; !dropped {
			withoutExcluded = append(withoutExcluded, id)
		}
	}
	resolved = domain.StableStrings(withoutExcluded)
	if len(resolved) == 0 {
		return ListComposition{}, errors.New("empty")
	}
	serviceDomains, err := normalizeServiceDomains(c.ServiceDomains, resolved)
	if err != nil {
		return ListComposition{}, err
	}
	priority, err := normalizePriority(c.Priority, resolved)
	if err != nil {
		return ListComposition{}, err
	}
	return ListComposition{Services: serviceIDs, Categories: categoryIDs, Exclusions: exclusions, ServiceDomains: serviceDomains, Priority: priority}, nil
}
func containsRoute(v []TransferRoute, ref string) bool {
	for _, x := range v {
		if x.Ref == ref {
			return true
		}
	}
	return false
}
func findDevice(v []TransferDevice, ref string) (TransferDevice, bool) {
	for _, x := range v {
		if x.Ref == ref {
			return x, true
		}
	}
	return TransferDevice{}, false
}

func (s *PublicationService) prepareApply(d ConfigTransferDocument) (ConfigTransferApply, error) {
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return ConfigTransferApply{}, transferError("storage_failed", "clock")
	}
	a := ConfigTransferApply{Document: d, AppliedAt: now, CustomServiceIDs: map[string]string{}, CustomCategoryIDs: map[string]string{}, CustomSourceIDs: map[string]string{}, RouteIDs: map[string]string{}, DeviceIDs: map[string]string{}, OutputIDs: map[string]string{}, Outputs: map[string]Output{}}
	used := map[string]bool{}
	gen := func(prefix string) (string, error) {
		for i := 0; i < 8; i++ {
			n := 16
			if prefix == "" {
				n = 32
			}
			id, err := randomHex(s.config.Entropy, n/2)
			if err != nil {
				return "", transferError("identity_exhausted", "")
			}
			id = prefix + id
			if !used[id] {
				used[id] = true
				return id, nil
			}
		}
		return "", transferError("identity_exhausted", "")
	}
	for _, v := range d.CustomServices {
		id, e := gen("custom-")
		if e != nil {
			return a, e
		}
		a.CustomServiceIDs[v.Ref] = id
	}
	for _, v := range d.CustomCategories {
		id, e := gen("custom-")
		if e != nil {
			return a, e
		}
		a.CustomCategoryIDs[v.Ref] = id
	}
	for _, t := range d.Tunings {
		for _, v := range t.CustomSources {
			id, e := gen("feed-")
			if e != nil {
				return a, e
			}
			a.CustomSourceIDs[v.Ref] = id
		}
	}
	for _, v := range d.Routes {
		id, e := gen("")
		if e != nil {
			return a, e
		}
		a.RouteIDs[v.Ref] = id
	}
	for _, v := range d.Devices {
		id, e := gen("")
		if e != nil {
			return a, e
		}
		a.DeviceIDs[v.Ref] = id
	}
	for _, v := range d.Outputs {
		id, e := gen("")
		if e != nil {
			return a, e
		}
		a.OutputIDs[v.Ref] = id
		target := s.config.Targets[v.TargetID]
		renderer, err := s.config.Renderers.For(target)
		if err != nil {
			return a, transferError("target_missing", "outputs")
		}
		a.Outputs[v.Ref] = Output{ID: id, ListID: a.RouteIDs[v.RouteRef], TargetID: v.TargetID, DeviceID: a.DeviceIDs[v.DeviceRef], ProfileKey: target.ProfileKey, RendererID: target.RendererID, RendererVersion: renderer.Version(), TargetRevision: s.config.TargetRevision, CreatedAt: now}
	}
	return a, nil
}
