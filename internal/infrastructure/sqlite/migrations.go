package sqlite

const CurrentSchemaVersion = 15

type migration struct {
	version int
	sql     string
}

// Version one is the adopted list/output baseline. Every following change is
// forward-only: published plans, artifacts and subscriptions stay immutable.
var migrations = []migration{{version: 1, sql: `
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at_ns INTEGER NOT NULL
) STRICT;

CREATE TABLE resources (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('domain','ip','prefix')),
    normalized_value TEXT NOT NULL,
    ip_version INTEGER CHECK (ip_version IS NULL OR ip_version IN (4,6)),
    created_at_ns INTEGER NOT NULL,
    UNIQUE (kind, normalized_value)
) STRICT;

CREATE TABLE sightings (
    id INTEGER PRIMARY KEY,
    service_id TEXT NOT NULL,
    component_id TEXT NOT NULL,
    resource_id INTEGER NOT NULL REFERENCES resources(id),
    source_id TEXT NOT NULL,
    source_class TEXT NOT NULL CHECK (source_class IN ('manual','official','observed','community','metadata')),
    source_revision TEXT NOT NULL,
    first_seen_ns INTEGER NOT NULL,
    last_seen_ns INTEGER NOT NULL,
    valid_until_ns INTEGER NOT NULL,
    ttl_seconds INTEGER,
    observation_count INTEGER NOT NULL CHECK (observation_count > 0),
    metadata_json TEXT NOT NULL DEFAULT '',
    invalid INTEGER NOT NULL DEFAULT 0 CHECK (invalid IN (0,1)),
    UNIQUE (service_id, component_id, resource_id, source_id, source_revision)
) STRICT;

CREATE INDEX sightings_service_source_idx
ON sightings(service_id, source_id, source_revision);

CREATE TABLE relations (
    id INTEGER PRIMARY KEY,
    source_resource_id INTEGER NOT NULL REFERENCES resources(id),
    relation_type TEXT NOT NULL CHECK (relation_type IN ('cname_to','loaded_by','redirects_to','observed_in_session')),
    target_resource_id INTEGER NOT NULL REFERENCES resources(id),
    service_id TEXT NOT NULL,
    component_id TEXT NOT NULL,
    first_seen_ns INTEGER NOT NULL,
    last_seen_ns INTEGER NOT NULL,
    valid_until_ns INTEGER NOT NULL,
    source_id TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    invalid INTEGER NOT NULL DEFAULT 0 CHECK (invalid IN (0,1)),
    UNIQUE (source_resource_id, relation_type, target_resource_id, service_id, component_id, source_id, source_revision)
) STRICT;

CREATE INDEX relations_service_source_idx
ON relations(service_id, source_id, source_revision);

CREATE TABLE source_runs (
    id INTEGER PRIMARY KEY,
    service_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    started_at_ns INTEGER NOT NULL,
    completed_at_ns INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('success','failed')),
    sighting_count INTEGER NOT NULL DEFAULT 0 CHECK (sighting_count >= 0),
    relation_count INTEGER NOT NULL DEFAULT 0 CHECK (relation_count >= 0),
    error_code TEXT
) STRICT;

CREATE INDEX source_runs_service_source_idx
ON source_runs(service_id, source_id, completed_at_ns);

-- The effective target profile of one service, which is not the product's
-- profile of anything. The name separates the two deliberately.
CREATE TABLE effective_profiles (
    profile_key TEXT NOT NULL,
    service_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    renderer_id TEXT NOT NULL,
    catalog_revision TEXT NOT NULL,
    config_json TEXT NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (profile_key, service_id)
) WITHOUT ROWID, STRICT;

-- Service-wide preferences the process itself acts on, as opposed to the ones
-- a browser keeps for its own view. The scheduler reads this table, so it
-- cannot live in the operator's local storage.
CREATE TABLE settings (
    key TEXT PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 64),
    value TEXT NOT NULL CHECK (length(value) <= 256),
    updated_at_ns INTEGER NOT NULL
) WITHOUT ROWID, STRICT;

-- A list is the unit of storage and of output (ADR 0013, ADR 0016). Its name
-- and composition are mutable; identity and creation time are not.
--
-- refresh_interval is the list's own rule; the empty string means it follows
-- the service-wide default, which is what a list has until someone says
-- otherwise. last_refresh_failed records that the most recent scheduled run
-- did not finish, so the surface can say so and the scheduler waits for the
-- next tick instead of retrying inside this one.
--
-- archived_at_ns is when the list left the shelf, zero while it is on it. It
-- is a moment rather than a flag because "since when" is the question asked of
-- an archived list, and a flag would answer it with nothing. Archiving stops
-- the list from changing; it never touches what the list already published,
-- which is why the artifact and subscription tables know nothing about it.
CREATE TABLE lists (
    id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    refresh_interval TEXT NOT NULL DEFAULT '' CHECK (refresh_interval IN ('', 'off', 'daily', 'weekly')),
    last_refreshed_at_ns INTEGER NOT NULL DEFAULT 0 CHECK (last_refreshed_at_ns >= 0),
    last_refresh_failed INTEGER NOT NULL DEFAULT 0 CHECK (last_refresh_failed IN (0, 1)),
    archived_at_ns INTEGER NOT NULL DEFAULT 0 CHECK (archived_at_ns >= 0),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

-- The services the list names itself.
CREATE TABLE list_services (
    list_id TEXT NOT NULL REFERENCES lists(id),
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    PRIMARY KEY (list_id, service_id)
) WITHOUT ROWID, STRICT;

-- The catalog categories the list references. A reference points only at
-- catalog content, which cannot reference a user list back, so a cycle is
-- impossible by construction (ADR 0016). There is no foreign key because a
-- category lives in the catalog, exactly as a service does.
CREATE TABLE list_categories (
    list_id TEXT NOT NULL REFERENCES lists(id),
    category_id TEXT NOT NULL CHECK (length(category_id) BETWEEN 1 AND 64),
    PRIMARY KEY (list_id, category_id)
) WITHOUT ROWID, STRICT;

-- What the operator removed from what a reference brought in. The exclusion is
-- kept rather than applied destructively so the list can show it and put it
-- back, and so a category that regains the service does not silently return it.
CREATE TABLE list_exclusions (
    list_id TEXT NOT NULL REFERENCES lists(id),
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    PRIMARY KEY (list_id, service_id)
) WITHOUT ROWID, STRICT;

-- A device is a registered instance of a catalog target: what the operator
-- calls it and where it is. No credential is stored here. A password belongs
-- in the operating system's secret store or in one attempt and nowhere else,
-- and a column for it would be the first place anyone looked (ADR 0014).
--
-- auto_deliver records that the operator explicitly asked for unattended
-- delivery, which is the only condition under which a credential outlives one
-- attempt.
CREATE TABLE devices (
    id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    target_id TEXT NOT NULL CHECK (length(target_id) BETWEEN 1 AND 64),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    address TEXT NOT NULL CHECK (length(address) BETWEEN 1 AND 512),
    account TEXT NOT NULL CHECK (length(account) <= 120),
    auto_deliver INTEGER NOT NULL DEFAULT 0 CHECK (auto_deliver IN (0, 1)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

-- An output binds one list to one renderer format and optionally to one device.
-- It owns the subscription and the artifact history. The list expresses the
-- selection, so an output never needs to name more than one.
CREATE TABLE outputs (
    id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    list_id TEXT NOT NULL REFERENCES lists(id),
    target_id TEXT NOT NULL,
    profile_key TEXT NOT NULL,
    renderer_id TEXT NOT NULL,
    renderer_version TEXT NOT NULL,
    target_revision TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    latest_artifact_id TEXT REFERENCES artifact_builds(id),
    previous_artifact_id TEXT REFERENCES artifact_builds(id),
    CHECK (latest_artifact_id IS NULL OR latest_artifact_id <> previous_artifact_id)
) STRICT;

-- One output per format per list: the output is the binding, so a second one
-- for the same target would be the same object twice.
CREATE UNIQUE INDEX outputs_list_target_idx ON outputs(list_id, target_id);

CREATE TABLE plan_snapshots (
    id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    output_id TEXT NOT NULL REFERENCES outputs(id),
    routing_plan_hash TEXT NOT NULL CHECK (length(routing_plan_hash) = 64 AND routing_plan_hash NOT GLOB '*[^0-9a-f]*'),
    routing_plan_json BLOB NOT NULL CHECK (length(routing_plan_json) BETWEEN 1 AND 4194304),
    policy_version TEXT NOT NULL,
    catalog_revision TEXT NOT NULL,
    observation_cutoff_ns INTEGER NOT NULL,
    created_at_ns INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status = 'valid'),
    UNIQUE (output_id, routing_plan_json)
) STRICT;
CREATE INDEX plan_snapshots_output_hash_idx ON plan_snapshots(output_id, routing_plan_hash);

CREATE TABLE artifact_builds (
    id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    output_id TEXT NOT NULL REFERENCES outputs(id),
    plan_snapshot_id TEXT NOT NULL REFERENCES plan_snapshots(id),
    renderer_id TEXT NOT NULL,
    renderer_version TEXT NOT NULL,
    artifact_hash TEXT NOT NULL CHECK (length(artifact_hash) = 64 AND artifact_hash NOT GLOB '*[^0-9a-f]*'),
    artifact_path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 4194304),
    content_type TEXT NOT NULL,
    content_created_at_ns INTEGER NOT NULL,
    validation_status TEXT NOT NULL CHECK (validation_status = 'valid'),
    status TEXT NOT NULL CHECK (status = 'published'),
    UNIQUE (plan_snapshot_id, renderer_id, renderer_version, artifact_hash)
) STRICT;
CREATE INDEX artifact_builds_output_hash_idx ON artifact_builds(output_id, artifact_hash);

CREATE TABLE subscriptions (
    output_id TEXT PRIMARY KEY REFERENCES outputs(id),
    token_id TEXT NOT NULL UNIQUE CHECK (length(token_id) = 32 AND token_id NOT GLOB '*[^0-9a-f]*'),
    token_hash BLOB NOT NULL CHECK (length(token_hash) = 32),
    created_at_ns INTEGER NOT NULL
) WITHOUT ROWID, STRICT;

CREATE TRIGGER plan_snapshots_no_update BEFORE UPDATE ON plan_snapshots BEGIN
    SELECT RAISE(ABORT, 'immutable plan snapshot');
END;
CREATE TRIGGER plan_snapshots_no_delete BEFORE DELETE ON plan_snapshots BEGIN
    SELECT RAISE(ABORT, 'immutable plan snapshot');
END;
CREATE TRIGGER artifact_builds_no_update BEFORE UPDATE ON artifact_builds BEGIN
    SELECT RAISE(ABORT, 'immutable artifact build');
END;
CREATE TRIGGER artifact_builds_no_delete BEFORE DELETE ON artifact_builds BEGIN
    SELECT RAISE(ABORT, 'immutable artifact build');
END;
CREATE TRIGGER subscriptions_no_update BEFORE UPDATE ON subscriptions BEGIN
    SELECT RAISE(ABORT, 'immutable subscription');
END;
CREATE TRIGGER subscriptions_no_delete BEFORE DELETE ON subscriptions BEGIN
    SELECT RAISE(ABORT, 'immutable subscription');
END;
CREATE TRIGGER outputs_identity_immutable BEFORE UPDATE OF id,list_id,target_id,profile_key,renderer_id,renderer_version,target_revision,created_at_ns ON outputs BEGIN
    SELECT RAISE(ABORT, 'immutable output identity');
END;
CREATE TRIGGER outputs_no_delete BEFORE DELETE ON outputs BEGIN
    SELECT RAISE(ABORT, 'immutable output');
END;
CREATE TRIGGER outputs_pointer_integrity_insert BEFORE INSERT ON outputs
WHEN NEW.latest_artifact_id IS NOT NULL OR NEW.previous_artifact_id IS NOT NULL BEGIN
    SELECT CASE WHEN NEW.latest_artifact_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM artifact_builds WHERE id=NEW.latest_artifact_id AND output_id=NEW.id
    ) THEN RAISE(ABORT, 'invalid latest artifact pointer') END;
    SELECT CASE WHEN NEW.previous_artifact_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM artifact_builds WHERE id=NEW.previous_artifact_id AND output_id=NEW.id
    ) THEN RAISE(ABORT, 'invalid previous artifact pointer') END;
END;
CREATE TRIGGER outputs_pointer_integrity_update BEFORE UPDATE OF latest_artifact_id, previous_artifact_id ON outputs BEGIN
    SELECT CASE WHEN NEW.latest_artifact_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM artifact_builds WHERE id=NEW.latest_artifact_id AND output_id=NEW.id
    ) THEN RAISE(ABORT, 'invalid latest artifact pointer') END;
    SELECT CASE WHEN NEW.previous_artifact_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM artifact_builds WHERE id=NEW.previous_artifact_id AND output_id=NEW.id
    ) THEN RAISE(ABORT, 'invalid previous artifact pointer') END;
END;
CREATE TRIGGER artifact_output_integrity BEFORE INSERT ON artifact_builds BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM plan_snapshots WHERE id=NEW.plan_snapshot_id AND output_id=NEW.output_id
    ) THEN RAISE(ABORT, 'artifact snapshot output mismatch') END;
END;
CREATE TRIGGER lists_identity_immutable BEFORE UPDATE OF id, created_at_ns ON lists BEGIN
    SELECT RAISE(ABORT, 'immutable list identity');
END;
-- Deleting a list would leave its outputs serving subscriptions for something
-- the library no longer lists, which is the one thing the surface must never
-- do (ADR 0004).
CREATE TRIGGER lists_no_delete BEFORE DELETE ON lists BEGIN
    SELECT RAISE(ABORT, 'immutable list');
END;
-- Naming a service and excluding it in the same list is not a preference, it is
-- a contradiction the resolver would have to break arbitrarily. It is refused
-- where it cannot be bypassed instead of being resolved by convention.
CREATE TRIGGER list_services_not_excluded BEFORE INSERT ON list_services BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM list_exclusions WHERE list_id=NEW.list_id AND service_id=NEW.service_id
    ) THEN RAISE(ABORT, 'service is both named and excluded') END;
END;
CREATE TRIGGER list_exclusions_not_named BEFORE INSERT ON list_exclusions BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM list_services WHERE list_id=NEW.list_id AND service_id=NEW.service_id
    ) THEN RAISE(ABORT, 'service is both named and excluded') END;
END;
`}, {version: 2, sql: `
-- One immutable, bounded result per publication attempt. Keeping attempts in
-- their own table preserves output identity and publication history while a
-- failed candidate leaves the last valid artifact untouched.
CREATE TABLE output_attempts (
    id INTEGER PRIMARY KEY,
    output_id TEXT NOT NULL REFERENCES outputs(id),
    status TEXT NOT NULL CHECK (status IN ('success','failed')),
    code TEXT NOT NULL DEFAULT '' CHECK (length(code) <= 64),
    projected_rules INTEGER NOT NULL DEFAULT 0 CHECK (projected_rules >= 0),
    maximum_rules INTEGER NOT NULL DEFAULT 0 CHECK (maximum_rules >= 0),
    artifact_id TEXT REFERENCES artifact_builds(id),
    completed_at_ns INTEGER NOT NULL,
    CHECK ((status = 'success' AND code = '' AND artifact_id IS NOT NULL) OR
           (status = 'failed' AND code <> '' AND artifact_id IS NULL))
) STRICT;
CREATE INDEX output_attempts_latest_idx
ON output_attempts(output_id, completed_at_ns DESC, id DESC);
CREATE TRIGGER output_attempts_no_update BEFORE UPDATE ON output_attempts BEGIN
    SELECT RAISE(ABORT, 'immutable output attempt');
END;
CREATE TRIGGER output_attempts_no_delete BEFORE DELETE ON output_attempts BEGIN
    SELECT RAISE(ABORT, 'immutable output attempt');
END;
`}, {version: 3, sql: `
-- A list may replace the catalog domain seeds of one of its services without
-- changing that service for any other list. An empty JSON array is distinct
-- from no row: it intentionally removes the static domains while observations
-- and source updates continue to contribute rules.
CREATE TABLE list_service_domains (
    list_id TEXT NOT NULL REFERENCES lists(id),
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    domains_json TEXT NOT NULL CHECK (length(domains_json) BETWEEN 2 AND 16384),
    PRIMARY KEY (list_id, service_id)
) WITHOUT ROWID, STRICT;
`}, {version: 4, sql: `
-- An operator-defined catalog entry: a title and the domain suffixes it stands
-- for. The reserved id prefix keeps it apart from every shipped service. Title
-- and domains are mutable; identity and creation time are not, and there is no
-- delete: a list referencing a vanished service would silently publish less
-- than it says (the same reasoning as ADR 0004 for lists).
CREATE TABLE custom_services (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 63 AND id GLOB 'custom-*'),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    domains_json TEXT NOT NULL CHECK (length(domains_json) BETWEEN 2 AND 16384),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;
CREATE TRIGGER custom_services_identity_immutable BEFORE UPDATE OF id, created_at_ns ON custom_services BEGIN
    SELECT RAISE(ABORT, 'immutable custom service identity');
END;
CREATE TRIGGER custom_services_no_delete BEFORE DELETE ON custom_services BEGIN
    SELECT RAISE(ABORT, 'immutable custom service');
END;
`}, {version: 5, sql: `
-- The operator's standing corrections to one service's automatic sources: a
-- catalog source switched off for this installation. Enabling again removes
-- the row, so the absence of a row is the catalog's own default.
CREATE TABLE service_disabled_sources (
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    source_id TEXT NOT NULL CHECK (length(source_id) BETWEEN 1 AND 64),
    PRIMARY KEY (service_id, source_id)
) WITHOUT ROWID, STRICT;

-- An operator-added HTTP feed of one service. It is configuration rather than
-- a publication: removing it stops future observations while everything the
-- service already published stays untouched, so deletion is allowed here.
CREATE TABLE custom_sources (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 63 AND id GLOB 'feed-*'),
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    url TEXT NOT NULL CHECK (length(url) BETWEEN 12 AND 2048),
    format TEXT NOT NULL CHECK (format IN ('text','json','domain-list')),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;
CREATE INDEX custom_sources_service_idx ON custom_sources(service_id);
CREATE TRIGGER custom_sources_identity_immutable BEFORE UPDATE OF id, service_id, created_at_ns ON custom_sources BEGIN
    SELECT RAISE(ABORT, 'immutable custom source identity');
END;

-- The operator's verdict on one destination of one service: include adds a
-- destination the automatic material does not carry, exclude switches off a
-- destination that catalog seeds or source observations keep offering.
-- Resetting the verdict removes the row: no row means the automatic material
-- speaks for itself.
--
-- The domain column keeps its name because the shape is unchanged and a rename
-- would be a migration with nothing to migrate. It carries the canonical form
-- of any destination the operator can state: a domain name, an IP address, or
-- a network prefix, all of which fit the existing length bound. The kind is
-- derived from the value where the rule kinds live, above this schema.
CREATE TABLE service_domain_verdicts (
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    domain TEXT NOT NULL CHECK (length(domain) BETWEEN 1 AND 253),
    verdict TEXT NOT NULL CHECK (verdict IN ('include','exclude')),
    PRIMARY KEY (service_id, domain)
) WITHOUT ROWID, STRICT;
`}, {version: 6, sql: `
-- A category the operator created. The shipped catalog still supplies its own
-- categories and cannot use the reserved prefix, so the two never collide and
-- a later catalog import can never claim a stored identity (ADR 0028).
-- Unlike a custom service, this row may be deleted: a category is a grouping,
-- and the routes that name one are found before the deletion is allowed.
CREATE TABLE custom_categories (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 63 AND id GLOB 'custom-*'),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;
CREATE TRIGGER custom_categories_identity_immutable BEFORE UPDATE OF id, created_at_ns ON custom_categories BEGIN
    SELECT RAISE(ABORT, 'immutable custom category identity');
END;

-- The operator's membership overlay over the shipped catalog: 'added' puts a
-- service into a category the catalog does not put it in, 'removed' takes a
-- catalog member out. Absence of a row is the catalog's own answer, so the
-- table stores only where the operator disagrees with it.
--
-- There is no foreign key on either column: a category may be a catalog one,
-- which has no row here or anywhere else, and a service may be shipped or
-- operator-defined. The reserved prefix carries the one constraint that is
-- expressible — a category the operator created has no catalog membership to
-- remove, so 'removed' is meaningless for it and refused by the schema.
CREATE TABLE category_memberships (
    category_id TEXT NOT NULL CHECK (length(category_id) BETWEEN 1 AND 64),
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    state TEXT NOT NULL CHECK (state IN ('added','removed')),
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (category_id, service_id),
    CHECK (state = 'added' OR category_id NOT GLOB 'custom-*')
) WITHOUT ROWID, STRICT;
`}, {version: 7, sql: `
-- What the operator deleted from what the catalog ships (ADR 0029). The
-- operator owns the library, including the objects the shipped files put in
-- it, and a deletion must survive a catalog update: the file is never edited,
-- so the subtraction is recorded here and every catalog read applies it before
-- merging the overlay.
--
-- The reserved prefix is refused rather than recorded. Deleting an object the
-- operator created deletes that object's own rows, so a record for one would
-- subtract something already gone and would then outlive whatever later took
-- the identity.
CREATE TABLE catalog_removals (
    kind TEXT NOT NULL CHECK (kind IN ('category','service')),
    id TEXT NOT NULL CHECK (length(id) BETWEEN 1 AND 63 AND id NOT GLOB 'custom-*'),
    removed_at TEXT NOT NULL CHECK (length(removed_at) BETWEEN 1 AND 64),
    PRIMARY KEY (kind, id)
) WITHOUT ROWID, STRICT;

-- A custom list may now be deleted. Version four refused it because a route
-- referencing a vanished list would silently publish less than it says; that
-- reason is answered by the refusal above the store, which names the routes
-- holding a direct reference and lets the deletion through only when none
-- does. The trigger that expressed the old answer would refuse the new one.
DROP TRIGGER custom_services_no_delete;
`}, {version: 8, sql: `
-- Scheduled delivery needs the same non-secret connection metadata as a
-- manual delivery. The interface belongs to the registered device; an empty
-- value is valid for deployers that do not attach routes to an interface.
ALTER TABLE devices ADD COLUMN interface TEXT NOT NULL DEFAULT '' CHECK (length(interface) <= 120);

-- An output may name one explicit device (ADR 0013). ON DELETE SET NULL keeps
-- the published file and subscription when the operator forgets a device.
ALTER TABLE outputs ADD COLUMN device_id TEXT REFERENCES devices(id) ON DELETE SET NULL;
`}, {version: 9, sql: `
-- Exact static-route ownership is scoped to a stable endpoint, target, and
-- interface. Retired scopes are retained so changing or forgetting a device
-- cannot make stale claims authorize deletion if an address is reused.
CREATE TABLE managed_route_scopes (
    id INTEGER PRIMARY KEY,
    endpoint TEXT NOT NULL CHECK (length(endpoint) BETWEEN 1 AND 512),
    target_id TEXT NOT NULL CHECK (length(target_id) BETWEEN 1 AND 64),
    interface TEXT NOT NULL CHECK (length(interface) BETWEEN 1 AND 120),
    retired_at_ns INTEGER NOT NULL DEFAULT 0 CHECK (retired_at_ns >= 0),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX managed_route_scopes_active_idx
ON managed_route_scopes(endpoint,target_id,interface) WHERE retired_at_ns=0;

CREATE TABLE managed_routes (
    scope_id INTEGER NOT NULL REFERENCES managed_route_scopes(id),
    prefix TEXT NOT NULL CHECK (length(prefix) BETWEEN 3 AND 64),
    created_by_routevane INTEGER NOT NULL CHECK (created_by_routevane IN (0,1)),
    PRIMARY KEY (scope_id,prefix)
) WITHOUT ROWID, STRICT;

CREATE TABLE managed_route_claims (
    scope_id INTEGER NOT NULL,
    output_id TEXT NOT NULL REFERENCES outputs(id) CHECK (length(output_id)=32 AND output_id NOT GLOB '*[^0-9a-f]*'),
    prefix TEXT NOT NULL CHECK (length(prefix) BETWEEN 3 AND 64),
    PRIMARY KEY (scope_id,output_id,prefix),
    FOREIGN KEY (scope_id,prefix) REFERENCES managed_routes(scope_id,prefix)
) WITHOUT ROWID, STRICT;
`}, {version: 10, sql: `
-- Route descriptions are part of exact ownership. Legacy rows migrate with
-- empty descriptions and labels, which keeps them safe and usable without
-- inventing provenance that version nine never stored.
ALTER TABLE managed_routes ADD COLUMN description TEXT NOT NULL DEFAULT ''
    CHECK (length(CAST(description AS BLOB)) <= 96);
ALTER TABLE managed_route_claims ADD COLUMN description TEXT NOT NULL DEFAULT ''
    CHECK (length(CAST(description AS BLOB)) <= 96);
-- The complete provenance can contain every shipped and custom category. Keep
-- the same conservative byte ceiling as an immutable plan snapshot.
ALTER TABLE managed_route_claims ADD COLUMN labels_json TEXT NOT NULL DEFAULT '[]'
    CHECK (length(CAST(labels_json AS BLOB)) BETWEEN 2 AND 4194304 AND json_valid(labels_json) AND json_type(labels_json)='array');
`}, {version: 11, sql: `
-- A route's resolved services have an operator-owned priority independent of
-- whether each service was named directly or arrived through a live category.
-- Existing routes have no rows and therefore retain their canonical service-id
-- order until the operator chooses another one.
CREATE TABLE list_service_priorities (
    list_id TEXT NOT NULL REFERENCES lists(id),
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    position INTEGER NOT NULL CHECK (position BETWEEN 0 AND 127),
    PRIMARY KEY (list_id, service_id),
    UNIQUE (list_id, position)
) WITHOUT ROWID, STRICT;
`}, {version: 12, sql: `
-- The library-wide default order is distinct from route-local list priority.
-- It is a sparse preference: stale service ids may remain stored, while reads
-- filter them and append currently available catalog ids canonically.
CREATE TABLE library_service_priorities (
    service_id TEXT NOT NULL CHECK (length(service_id) BETWEEN 1 AND 64),
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (service_id),
    UNIQUE (position)
) WITHOUT ROWID, STRICT;
`}, {version: 13, sql: `
-- ADR 0039 brings the schema to the vocabulary the rest of the product already
-- uses: one entry is a rule, a set of rules is a list, a group of lists is a
-- category, and the composition published to a device is a profile. Migrations
-- one to twelve keep the words they were written with, because an applied
-- migration is history; this is the one place where the two vocabularies meet.
--
--   effective_profiles, profile_key  ->  effective_formats, format_key
--   lists and its list_* children    ->  profiles and its profile_* children
--   custom_services, service_*       ->  custom_lists, list_*
--
-- The order is not free. The word lists is both a name being retired and a
-- name being claimed, so every rename of a list runs before every rename of
-- a service. In the other order the first step walks into names the second
-- step still needs and the two meanings merge with nothing left to tell them
-- apart.
--
-- SQLite rewrites a foreign key, a trigger body and an index definition to
-- follow a renamed table or column, but it never renames a trigger or an index
-- itself. Both are therefore dropped first and created again at the end, which
-- is also the only way their own names can follow the rename.
DROP TRIGGER lists_identity_immutable;
DROP TRIGGER lists_no_delete;
DROP TRIGGER list_services_not_excluded;
DROP TRIGGER list_exclusions_not_named;
DROP TRIGGER custom_services_identity_immutable;
DROP TRIGGER custom_sources_identity_immutable;
DROP TRIGGER outputs_identity_immutable;
DROP INDEX outputs_list_target_idx;
DROP INDEX sightings_service_source_idx;
DROP INDEX relations_service_source_idx;
DROP INDEX source_runs_service_source_idx;
DROP INDEX custom_sources_service_idx;

-- The rendering profile of a target is a format, which is what frees the word
-- for the composition below. Version one already had to say in a comment that
-- the two were not the same thing.
ALTER TABLE effective_profiles RENAME TO effective_formats;
ALTER TABLE effective_formats RENAME COLUMN profile_key TO format_key;
ALTER TABLE outputs RENAME COLUMN profile_key TO format_key;

ALTER TABLE lists RENAME TO profiles;
ALTER TABLE list_services RENAME TO profile_lists;
ALTER TABLE list_categories RENAME TO profile_categories;
ALTER TABLE list_exclusions RENAME TO profile_exclusions;
ALTER TABLE list_service_domains RENAME TO profile_list_domains;
ALTER TABLE list_service_priorities RENAME TO profile_list_priorities;
ALTER TABLE profile_lists RENAME COLUMN list_id TO profile_id;
ALTER TABLE profile_categories RENAME COLUMN list_id TO profile_id;
ALTER TABLE profile_exclusions RENAME COLUMN list_id TO profile_id;
ALTER TABLE profile_list_domains RENAME COLUMN list_id TO profile_id;
ALTER TABLE profile_list_priorities RENAME COLUMN list_id TO profile_id;
ALTER TABLE outputs RENAME COLUMN list_id TO profile_id;

ALTER TABLE custom_services RENAME TO custom_lists;
ALTER TABLE service_disabled_sources RENAME TO list_disabled_sources;
ALTER TABLE service_domain_verdicts RENAME TO list_domain_verdicts;
ALTER TABLE library_service_priorities RENAME TO library_list_priorities;
ALTER TABLE sightings RENAME COLUMN service_id TO list_id;
ALTER TABLE relations RENAME COLUMN service_id TO list_id;
ALTER TABLE source_runs RENAME COLUMN service_id TO list_id;
ALTER TABLE effective_formats RENAME COLUMN service_id TO list_id;
ALTER TABLE category_memberships RENAME COLUMN service_id TO list_id;
ALTER TABLE custom_sources RENAME COLUMN service_id TO list_id;
ALTER TABLE profile_lists RENAME COLUMN service_id TO list_id;
ALTER TABLE profile_exclusions RENAME COLUMN service_id TO list_id;
ALTER TABLE profile_list_domains RENAME COLUMN service_id TO list_id;
ALTER TABLE profile_list_priorities RENAME COLUMN service_id TO list_id;
ALTER TABLE list_disabled_sources RENAME COLUMN service_id TO list_id;
ALTER TABLE list_domain_verdicts RENAME COLUMN service_id TO list_id;
ALTER TABLE library_list_priorities RENAME COLUMN service_id TO list_id;

-- The only place the retired word is a stored value rather than a name. A
-- CHECK constraint cannot be altered, so the table is rebuilt around the new
-- one and every recorded deletion is carried across under the current word.
CREATE TABLE catalog_removals_v13 (
    kind TEXT NOT NULL CHECK (kind IN ('category','list')),
    id TEXT NOT NULL CHECK (length(id) BETWEEN 1 AND 63 AND id NOT GLOB 'custom-*'),
    removed_at TEXT NOT NULL CHECK (length(removed_at) BETWEEN 1 AND 64),
    PRIMARY KEY (kind, id)
) WITHOUT ROWID, STRICT;
INSERT INTO catalog_removals_v13(kind,id,removed_at)
SELECT CASE kind WHEN 'service' THEN 'list' ELSE kind END, id, removed_at FROM catalog_removals;
DROP TABLE catalog_removals;
ALTER TABLE catalog_removals_v13 RENAME TO catalog_removals;

CREATE UNIQUE INDEX outputs_profile_target_idx ON outputs(profile_id, target_id);
CREATE INDEX sightings_list_source_idx ON sightings(list_id, source_id, source_revision);
CREATE INDEX relations_list_source_idx ON relations(list_id, source_id, source_revision);
CREATE INDEX source_runs_list_source_idx ON source_runs(list_id, source_id, completed_at_ns);
CREATE INDEX custom_sources_list_idx ON custom_sources(list_id);

CREATE TRIGGER profiles_identity_immutable BEFORE UPDATE OF id, created_at_ns ON profiles BEGIN
    SELECT RAISE(ABORT, 'immutable profile identity');
END;
CREATE TRIGGER profiles_no_delete BEFORE DELETE ON profiles BEGIN
    SELECT RAISE(ABORT, 'immutable profile');
END;
CREATE TRIGGER profile_lists_not_excluded BEFORE INSERT ON profile_lists BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM profile_exclusions WHERE profile_id=NEW.profile_id AND list_id=NEW.list_id
    ) THEN RAISE(ABORT, 'list is both named and excluded') END;
END;
CREATE TRIGGER profile_exclusions_not_named BEFORE INSERT ON profile_exclusions BEGIN
    SELECT CASE WHEN EXISTS (
        SELECT 1 FROM profile_lists WHERE profile_id=NEW.profile_id AND list_id=NEW.list_id
    ) THEN RAISE(ABORT, 'list is both named and excluded') END;
END;
CREATE TRIGGER custom_lists_identity_immutable BEFORE UPDATE OF id, created_at_ns ON custom_lists BEGIN
    SELECT RAISE(ABORT, 'immutable custom list identity');
END;
CREATE TRIGGER custom_sources_identity_immutable BEFORE UPDATE OF id, list_id, created_at_ns ON custom_sources BEGIN
    SELECT RAISE(ABORT, 'immutable custom source identity');
END;
CREATE TRIGGER outputs_identity_immutable BEFORE UPDATE OF id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns ON outputs BEGIN
    SELECT RAISE(ABORT, 'immutable output identity');
END;
`}, {version: 14, sql: `
ALTER TABLE outputs ADD COLUMN fqdn_group_prefix TEXT NOT NULL DEFAULT '';
CREATE TABLE managed_fqdn_groups (
 endpoint TEXT NOT NULL,
 output_id TEXT NOT NULL REFERENCES outputs(id),
 name TEXT NOT NULL,
 interface TEXT NOT NULL,
 state_json TEXT NOT NULL,
 PRIMARY KEY(endpoint,name)
) STRICT;
`}, {version: 15, sql: `
CREATE TABLE deployment_attempts (
 id TEXT PRIMARY KEY,
 artifact_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','succeeded','failed')),
 started_at_ns INTEGER NOT NULL,
 completed_at_ns INTEGER,
 result_json TEXT NOT NULL,
 error_code TEXT NOT NULL,
 CHECK((status='pending' AND completed_at_ns IS NULL AND result_json='' AND error_code='') OR
       (status='succeeded' AND completed_at_ns IS NOT NULL AND result_json<>'' AND error_code='') OR
       (status='failed' AND completed_at_ns IS NOT NULL AND result_json<>'' AND error_code<>''))
) STRICT;
CREATE TRIGGER deployment_attempts_identity_immutable BEFORE UPDATE OF id,artifact_id,request_hash,started_at_ns ON deployment_attempts BEGIN
 SELECT RAISE(ABORT, 'immutable deployment attempt identity');
END;
CREATE TRIGGER deployment_attempts_terminal_immutable BEFORE UPDATE OF status,completed_at_ns,result_json,error_code ON deployment_attempts WHEN OLD.status<>'pending' OR NEW.status='pending' BEGIN
 SELECT RAISE(ABORT, 'immutable deployment attempt outcome');
END;
CREATE TRIGGER deployment_attempts_no_delete BEFORE DELETE ON deployment_attempts BEGIN
 SELECT RAISE(ABORT, 'immutable deployment attempt');
END;
`}}

var requiredTables = []string{
	"deployment_attempts",
	"managed_fqdn_groups",
	"settings",
	"devices",
	"catalog_removals",
	"custom_categories",
	"custom_lists",
	"custom_sources",
	"list_disabled_sources",
	"list_domain_verdicts",
	"category_memberships",
	"library_list_priorities",
	"profiles",
	"profile_lists",
	"profile_categories",
	"profile_exclusions",
	"profile_list_domains",
	"profile_list_priorities",
	"managed_route_claims",
	"managed_route_scopes",
	"managed_routes",
	"outputs",
	"output_attempts",
	"effective_formats",
	"plan_snapshots",
	"artifact_builds",
	"subscriptions",
	"relations",
	"resources",
	"schema_migrations",
	"sightings",
	"source_runs",
}

var requiredColumns = map[string][]string{
	"deployment_attempts":     {"id", "artifact_id", "request_hash", "status", "started_at_ns", "completed_at_ns", "result_json", "error_code"},
	"managed_fqdn_groups":     {"endpoint", "output_id", "name", "interface", "state_json"},
	"schema_migrations":       {"version", "applied_at_ns"},
	"resources":               {"id", "kind", "normalized_value", "ip_version", "created_at_ns"},
	"sightings":               {"id", "list_id", "component_id", "resource_id", "source_id", "source_class", "source_revision", "first_seen_ns", "last_seen_ns", "valid_until_ns", "ttl_seconds", "observation_count", "metadata_json", "invalid"},
	"relations":               {"id", "source_resource_id", "relation_type", "target_resource_id", "list_id", "component_id", "first_seen_ns", "last_seen_ns", "valid_until_ns", "source_id", "source_revision", "invalid"},
	"source_runs":             {"id", "list_id", "source_id", "source_revision", "started_at_ns", "completed_at_ns", "status", "sighting_count", "relation_count", "error_code"},
	"effective_formats":       {"format_key", "list_id", "target_id", "renderer_id", "catalog_revision", "config_json", "updated_at_ns"},
	"settings":                {"key", "value", "updated_at_ns"},
	"profiles":                {"id", "name", "refresh_interval", "last_refreshed_at_ns", "last_refresh_failed", "archived_at_ns", "created_at_ns", "updated_at_ns"},
	"devices":                 {"id", "target_id", "name", "address", "account", "auto_deliver", "created_at_ns", "updated_at_ns", "interface"},
	"catalog_removals":        {"kind", "id", "removed_at"},
	"custom_categories":       {"id", "title", "created_at_ns", "updated_at_ns"},
	"category_memberships":    {"category_id", "list_id", "state", "updated_at_ns"},
	"custom_lists":            {"id", "title", "domains_json", "created_at_ns", "updated_at_ns"},
	"custom_sources":          {"id", "list_id", "url", "format", "created_at_ns", "updated_at_ns"},
	"list_disabled_sources":   {"list_id", "source_id"},
	"list_domain_verdicts":    {"list_id", "domain", "verdict"},
	"profile_lists":           {"profile_id", "list_id"},
	"profile_categories":      {"profile_id", "category_id"},
	"profile_exclusions":      {"profile_id", "list_id"},
	"profile_list_domains":    {"profile_id", "list_id", "domains_json"},
	"profile_list_priorities": {"profile_id", "list_id", "position"},
	"library_list_priorities": {"list_id", "position"},
	"managed_route_scopes":    {"id", "endpoint", "target_id", "interface", "retired_at_ns", "created_at_ns", "updated_at_ns"},
	"managed_routes":          {"scope_id", "prefix", "created_by_routevane", "description"},
	"managed_route_claims":    {"scope_id", "output_id", "prefix", "description", "labels_json"},
	"outputs":                 {"id", "profile_id", "target_id", "format_key", "renderer_id", "renderer_version", "target_revision", "created_at_ns", "latest_artifact_id", "previous_artifact_id", "device_id", "fqdn_group_prefix"},
	"output_attempts":         {"id", "output_id", "status", "code", "projected_rules", "maximum_rules", "artifact_id", "completed_at_ns"},
	"plan_snapshots":          {"id", "output_id", "routing_plan_hash", "routing_plan_json", "policy_version", "catalog_revision", "observation_cutoff_ns", "created_at_ns", "status"},
	"artifact_builds":         {"id", "output_id", "plan_snapshot_id", "renderer_id", "renderer_version", "artifact_hash", "artifact_path", "size_bytes", "content_type", "content_created_at_ns", "validation_status", "status"},
	"subscriptions":           {"output_id", "token_id", "token_hash", "created_at_ns"},
}

// requiredForeignKeys names every parent relation the publication and delivery
// chains depend on. A schema that lost one would still answer every read and
// accept an orphan or retain a forgotten device on the next write.
var requiredForeignKeys = map[string][]string{
	"plan_snapshots":          {"outputs"},
	"artifact_builds":         {"outputs"},
	"subscriptions":           {"outputs"},
	"profile_lists":           {"profiles"},
	"profile_categories":      {"profiles"},
	"profile_exclusions":      {"profiles"},
	"profile_list_domains":    {"profiles"},
	"profile_list_priorities": {"profiles"},
	"outputs":                 {"profiles", "devices"},
	"output_attempts":         {"outputs"},
	"managed_routes":          {"managed_route_scopes"},
	"managed_route_claims":    {"managed_routes", "outputs"},
	"managed_fqdn_groups":     {"outputs"},
}

var requiredTriggers = []string{
	"artifact_builds_no_delete",
	"artifact_builds_no_update",
	"artifact_output_integrity",
	"custom_categories_identity_immutable",
	"custom_lists_identity_immutable",
	"custom_sources_identity_immutable",
	"deployment_attempts_identity_immutable",
	"deployment_attempts_no_delete",
	"deployment_attempts_terminal_immutable",
	"outputs_identity_immutable",
	"outputs_no_delete",
	"outputs_pointer_integrity_insert",
	"outputs_pointer_integrity_update",
	"output_attempts_no_delete",
	"output_attempts_no_update",
	"plan_snapshots_no_delete",
	"plan_snapshots_no_update",
	"profiles_identity_immutable",
	"profiles_no_delete",
	"profile_exclusions_not_named",
	"profile_lists_not_excluded",
	"subscriptions_no_delete",
	"subscriptions_no_update",
}
