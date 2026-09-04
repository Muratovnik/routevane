package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// ExportConfigTransfer reads all portable operator-authored rows through one
// SQLite read transaction. It never joins publication/history tables, so a
// credential, token, observation, artifact, attempt, or audit cannot enter the
// document accidentally.
func (s *Store) ExportConfigTransfer(ctx context.Context) (application.ConfigTransferDocument, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ConfigTransferDocument{}, fmt.Errorf("begin transfer snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	d := application.ConfigTransferDocument{Version: application.ConfigTransferVersion, Settings: application.TransferSettings{RefreshInterval: application.RefreshOff}}
	var interval string
	err = tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=?", application.SettingRefreshInterval).Scan(&interval)
	if err == nil {
		d.Settings.RefreshInterval = application.RefreshInterval(interval)
	} else if err != sql.ErrNoRows {
		return d, fmt.Errorf("read transfer settings: %w", err)
	}
	if err := exportCustomServices(ctx, tx, &d); err != nil {
		return d, err
	}
	if err := exportCategories(ctx, tx, &d); err != nil {
		return d, err
	}
	if err := exportTunings(ctx, tx, &d); err != nil {
		return d, err
	}
	if err := exportRoutes(ctx, tx, &d); err != nil {
		return d, err
	}
	if err := exportDevices(ctx, tx, &d); err != nil {
		return d, err
	}
	if err := exportOutputs(ctx, tx, &d); err != nil {
		return d, err
	}
	if err := tx.Commit(); err != nil {
		return d, fmt.Errorf("commit transfer snapshot: %w", err)
	}
	return d, nil
}

func exportCustomServices(ctx context.Context, q *sql.Tx, d *application.ConfigTransferDocument) error {
	rows, err := q.QueryContext(ctx, "SELECT id,title,domains_json FROM custom_services ORDER BY id")
	if err != nil {
		return fmt.Errorf("read custom services: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v application.TransferCustomService
		var raw string
		if err := rows.Scan(&v.Ref, &v.Title, &raw); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &v.Domains); err != nil {
			return fmt.Errorf("decode custom service: %w", err)
		}
		d.CustomServices = append(d.CustomServices, v)
	}
	return rows.Err()
}

func exportCategories(ctx context.Context, q *sql.Tx, d *application.ConfigTransferDocument) error {
	rows, err := q.QueryContext(ctx, "SELECT id,title FROM custom_categories ORDER BY id")
	if err != nil {
		return fmt.Errorf("read custom categories: %w", err)
	}
	for rows.Next() {
		var v application.TransferCustomCategory
		if err := rows.Scan(&v.Ref, &v.Title); err != nil {
			_ = rows.Close()
			return err
		}
		d.CustomCategories = append(d.CustomCategories, v)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, "SELECT category_id,service_id,state FROM category_memberships ORDER BY category_id,service_id")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v application.TransferMembership
		if err := rows.Scan(&v.CategoryRef, &v.ServiceRef, &v.State); err != nil {
			_ = rows.Close()
			return err
		}
		d.Memberships = append(d.Memberships, v)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, "SELECT kind,id FROM catalog_removals ORDER BY kind,id")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v application.TransferRemoval
		if err := rows.Scan(&v.Kind, &v.ID); err != nil {
			_ = rows.Close()
			return err
		}
		d.Removals = append(d.Removals, v)
	}
	return rows.Close()
}

func exportTunings(ctx context.Context, q *sql.Tx, d *application.ConfigTransferDocument) error {
	by := map[string]*application.TransferTuning{}
	get := func(id string) *application.TransferTuning {
		if v := by[id]; v != nil {
			return v
		}
		v := &application.TransferTuning{ServiceRef: id}
		by[id] = v
		return v
	}
	// A custom feed URL may carry credentials in any component. Read only its
	// opaque identity so the export can omit both the source and its disabled
	// reference without ever loading the URL into the transfer snapshot.
	customSources := map[string]struct{}{}
	rows, err := q.QueryContext(ctx, "SELECT id FROM custom_sources ORDER BY id")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		customSources[id] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	d.OmittedCustomSources = uint(len(customSources))

	rows, err = q.QueryContext(ctx, "SELECT service_id,source_id FROM service_disabled_sources ORDER BY service_id,source_id")
	if err != nil {
		return err
	}
	for rows.Next() {
		var sid, id string
		if err := rows.Scan(&sid, &id); err != nil {
			_ = rows.Close()
			return err
		}
		if _, omitted := customSources[id]; omitted {
			continue
		}
		v := get(sid)
		v.DisabledSources = append(v.DisabledSources, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, "SELECT service_id,domain,verdict FROM service_domain_verdicts ORDER BY service_id,domain")
	if err != nil {
		return err
	}
	for rows.Next() {
		var sid, value, verdict string
		if err := rows.Scan(&sid, &value, &verdict); err != nil {
			_ = rows.Close()
			return err
		}
		v := get(sid)
		if verdict == string(application.DomainVerdictInclude) {
			v.Includes = append(v.Includes, value)
		} else {
			v.Excludes = append(v.Excludes, value)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, v := range by {
		d.Tunings = append(d.Tunings, *v)
	}
	return nil
}

func exportRoutes(ctx context.Context, q *sql.Tx, d *application.ConfigTransferDocument) error {
	rows, err := q.QueryContext(ctx, "SELECT id,name,refresh_interval,archived_at_ns FROM lists ORDER BY id")
	if err != nil {
		return err
	}
	by := map[string]*application.TransferRoute{}
	for rows.Next() {
		var v application.TransferRoute
		var archived int64
		if err := rows.Scan(&v.Ref, &v.Name, &v.RefreshInterval, &archived); err != nil {
			_ = rows.Close()
			return err
		}
		v.Archived = archived != 0
		v.ServiceDomains = map[string][]string{}
		d.Routes = append(d.Routes, v)
		by[v.Ref] = &d.Routes[len(d.Routes)-1]
	}
	if err := rows.Close(); err != nil {
		return err
	}
	// Appends above can move the slice, so rebuild pointers once its size is final.
	by = map[string]*application.TransferRoute{}
	for i := range d.Routes {
		by[d.Routes[i].Ref] = &d.Routes[i]
	}
	for _, spec := range []struct {
		query string
		add   func(*application.TransferRoute, string)
	}{{"SELECT list_id,service_id FROM list_services ORDER BY list_id,service_id", func(r *application.TransferRoute, v string) { r.Services = append(r.Services, v) }}, {"SELECT list_id,category_id FROM list_categories ORDER BY list_id,category_id", func(r *application.TransferRoute, v string) { r.Categories = append(r.Categories, v) }}, {"SELECT list_id,service_id FROM list_exclusions ORDER BY list_id,service_id", func(r *application.TransferRoute, v string) { r.Exclusions = append(r.Exclusions, v) }}} {
		rows, err = q.QueryContext(ctx, spec.query)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, v string
			if err := rows.Scan(&id, &v); err != nil {
				_ = rows.Close()
				return err
			}
			spec.add(by[id], v)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	rows, err = q.QueryContext(ctx, "SELECT list_id,service_id,domains_json FROM list_service_domains ORDER BY list_id,service_id")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, sid, raw string
		if err := rows.Scan(&id, &sid, &raw); err != nil {
			_ = rows.Close()
			return err
		}
		var values []string
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			_ = rows.Close()
			return err
		}
		by[id].ServiceDomains[sid] = values
	}
	return rows.Close()
}

func exportDevices(ctx context.Context, q *sql.Tx, d *application.ConfigTransferDocument) error {
	rows, err := q.QueryContext(ctx, "SELECT id,target_id,name,address,account,interface FROM devices ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v application.TransferDevice
		if err := rows.Scan(&v.Ref, &v.TargetID, &v.Name, &v.Address, &v.Account, &v.Interface); err != nil {
			return err
		}
		d.Devices = append(d.Devices, v)
	}
	return rows.Err()
}
func exportOutputs(ctx context.Context, q *sql.Tx, d *application.ConfigTransferDocument) error {
	rows, err := q.QueryContext(ctx, "SELECT id,list_id,target_id,COALESCE(device_id,'') FROM outputs ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v application.TransferOutput
		if err := rows.Scan(&v.Ref, &v.RouteRef, &v.TargetID, &v.DeviceRef); err != nil {
			return err
		}
		d.Outputs = append(d.Outputs, v)
	}
	return rows.Err()
}

// ApplyConfigTransfer refuses every database that is not a just-created empty
// destination, then inserts the complete remapped state in one transaction.
func (s *Store) ApplyConfigTransfer(ctx context.Context, a application.ConfigTransferApply) error {
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range requiredTables {
		if table == "schema_migrations" {
			continue
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return application.NewTransferError("destination_not_empty", "")
		}
	}
	now := a.AppliedAt.UTC().UnixNano()
	d := a.Document
	if _, err := tx.ExecContext(ctx, "INSERT INTO settings(key,value,updated_at_ns) VALUES(?,?,?)", application.SettingRefreshInterval, d.Settings.RefreshInterval, now); err != nil {
		return err
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
	for _, v := range d.CustomServices {
		raw, _ := json.Marshal(v.Domains)
		if _, err := tx.ExecContext(ctx, "INSERT INTO custom_services(id,title,domains_json,created_at_ns,updated_at_ns) VALUES(?,?,?,?,?)", a.CustomServiceIDs[v.Ref], v.Title, string(raw), now, now); err != nil {
			return err
		}
	}
	for _, v := range d.CustomCategories {
		if _, err := tx.ExecContext(ctx, "INSERT INTO custom_categories(id,title,created_at_ns,updated_at_ns) VALUES(?,?,?,?)", a.CustomCategoryIDs[v.Ref], v.Title, now, now); err != nil {
			return err
		}
	}
	for _, v := range d.Memberships {
		if _, err := tx.ExecContext(ctx, "INSERT INTO category_memberships(category_id,service_id,state,updated_at_ns) VALUES(?,?,?,?)", categoryID(v.CategoryRef), serviceID(v.ServiceRef), v.State, now); err != nil {
			return err
		}
	}
	for _, v := range d.Removals {
		if _, err := tx.ExecContext(ctx, "INSERT INTO catalog_removals(kind,id,removed_at) VALUES(?,?,?)", v.Kind, v.ID, a.AppliedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	for _, t := range d.Tunings {
		sid := serviceID(t.ServiceRef)
		for _, id := range t.DisabledSources {
			if _, err := tx.ExecContext(ctx, "INSERT INTO service_disabled_sources(service_id,source_id) VALUES(?,?)", sid, id); err != nil {
				return err
			}
		}
		for _, v := range t.CustomSources {
			if _, err := tx.ExecContext(ctx, "INSERT INTO custom_sources(id,service_id,url,format,created_at_ns,updated_at_ns) VALUES(?,?,?,?,?,?)", a.CustomSourceIDs[v.Ref], sid, v.URL, v.Format, now, now); err != nil {
				return err
			}
		}
		for _, v := range t.Includes {
			if _, err := tx.ExecContext(ctx, "INSERT INTO service_domain_verdicts(service_id,domain,verdict) VALUES(?,?,?)", sid, v, application.DomainVerdictInclude); err != nil {
				return err
			}
		}
		for _, v := range t.Excludes {
			if _, err := tx.ExecContext(ctx, "INSERT INTO service_domain_verdicts(service_id,domain,verdict) VALUES(?,?,?)", sid, v, application.DomainVerdictExclude); err != nil {
				return err
			}
		}
	}
	for _, v := range d.Routes {
		id := a.RouteIDs[v.Ref]
		archived := int64(0)
		if v.Archived {
			archived = now
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO lists(id,name,refresh_interval,last_refreshed_at_ns,last_refresh_failed,archived_at_ns,created_at_ns,updated_at_ns) VALUES(?,?,?,0,0,?,?,?)", id, v.Name, v.RefreshInterval, archived, now, now); err != nil {
			return err
		}
		for _, x := range v.Services {
			if _, err := tx.ExecContext(ctx, "INSERT INTO list_services(list_id,service_id) VALUES(?,?)", id, serviceID(x)); err != nil {
				return err
			}
		}
		for _, x := range v.Categories {
			if _, err := tx.ExecContext(ctx, "INSERT INTO list_categories(list_id,category_id) VALUES(?,?)", id, categoryID(x)); err != nil {
				return err
			}
		}
		for _, x := range v.Exclusions {
			if _, err := tx.ExecContext(ctx, "INSERT INTO list_exclusions(list_id,service_id) VALUES(?,?)", id, serviceID(x)); err != nil {
				return err
			}
		}
		for sid, values := range v.ServiceDomains {
			raw, _ := json.Marshal(values)
			if _, err := tx.ExecContext(ctx, "INSERT INTO list_service_domains(list_id,service_id,domains_json) VALUES(?,?,?)", id, serviceID(sid), string(raw)); err != nil {
				return err
			}
		}
	}
	for _, v := range d.Devices {
		if _, err := tx.ExecContext(ctx, "INSERT INTO devices(id,target_id,name,address,account,auto_deliver,created_at_ns,updated_at_ns,interface) VALUES(?,?,?,?,?,0,?,?,?)", a.DeviceIDs[v.Ref], v.TargetID, v.Name, v.Address, v.Account, now, now, v.Interface); err != nil {
			return err
		}
	}
	for _, v := range d.Outputs {
		o := a.Outputs[v.Ref]
		var device any = nil
		if o.DeviceID != "" {
			device = o.DeviceID
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO outputs(id,list_id,target_id,profile_key,renderer_id,renderer_version,target_revision,created_at_ns,device_id) VALUES(?,?,?,?,?,?,?,?,?)", o.ID, o.ListID, o.TargetID, o.ProfileKey, o.RendererID, o.RendererVersion, o.TargetRevision, now, device); err != nil {
			return err
		}
	}
	if s.configTransferPreflight != nil {
		if err := s.configTransferPreflight(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
