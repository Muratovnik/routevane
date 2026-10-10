-- Fixed schema checkpoint 15, derived from the unchanged historical migrations.
-- Later schema versions apply their ordinary migrations after this checkpoint.

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
    list_id TEXT NOT NULL,
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
    UNIQUE (list_id, component_id, resource_id, source_id, source_revision)
) STRICT;

CREATE TABLE relations (
    id INTEGER PRIMARY KEY,
    source_resource_id INTEGER NOT NULL REFERENCES resources(id),
    relation_type TEXT NOT NULL CHECK (relation_type IN ('cname_to','loaded_by','redirects_to','observed_in_session')),
    target_resource_id INTEGER NOT NULL REFERENCES resources(id),
    list_id TEXT NOT NULL,
    component_id TEXT NOT NULL,
    first_seen_ns INTEGER NOT NULL,
    last_seen_ns INTEGER NOT NULL,
    valid_until_ns INTEGER NOT NULL,
    source_id TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    invalid INTEGER NOT NULL DEFAULT 0 CHECK (invalid IN (0,1)),
    UNIQUE (source_resource_id, relation_type, target_resource_id, list_id, component_id, source_id, source_revision)
) STRICT;

CREATE TABLE source_runs (
    id INTEGER PRIMARY KEY,
    list_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    started_at_ns INTEGER NOT NULL,
    completed_at_ns INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('success','failed')),
    sighting_count INTEGER NOT NULL DEFAULT 0 CHECK (sighting_count >= 0),
    relation_count INTEGER NOT NULL DEFAULT 0 CHECK (relation_count >= 0),
    error_code TEXT
) STRICT;

CREATE TABLE "effective_formats" (
    format_key TEXT NOT NULL,
    list_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    renderer_id TEXT NOT NULL,
    catalog_revision TEXT NOT NULL,
    config_json TEXT NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (format_key, list_id)
) WITHOUT ROWID, STRICT;

CREATE TABLE settings (
    key TEXT PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 64),
    value TEXT NOT NULL CHECK (length(value) <= 256),
    updated_at_ns INTEGER NOT NULL
) WITHOUT ROWID, STRICT;

CREATE TABLE "profiles" (
    id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    refresh_interval TEXT NOT NULL DEFAULT '' CHECK (refresh_interval IN ('', 'off', 'daily', 'weekly')),
    last_refreshed_at_ns INTEGER NOT NULL DEFAULT 0 CHECK (last_refreshed_at_ns >= 0),
    last_refresh_failed INTEGER NOT NULL DEFAULT 0 CHECK (last_refresh_failed IN (0, 1)),
    archived_at_ns INTEGER NOT NULL DEFAULT 0 CHECK (archived_at_ns >= 0),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

CREATE TABLE "profile_lists" (
    profile_id TEXT NOT NULL REFERENCES "profiles"(id),
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    PRIMARY KEY (profile_id, list_id)
) WITHOUT ROWID, STRICT;

CREATE TABLE "profile_categories" (
    profile_id TEXT NOT NULL REFERENCES "profiles"(id),
    category_id TEXT NOT NULL CHECK (length(category_id) BETWEEN 1 AND 64),
    PRIMARY KEY (profile_id, category_id)
) WITHOUT ROWID, STRICT;

CREATE TABLE "profile_exclusions" (
    profile_id TEXT NOT NULL REFERENCES "profiles"(id),
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    PRIMARY KEY (profile_id, list_id)
) WITHOUT ROWID, STRICT;

CREATE TABLE devices (
    id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    target_id TEXT NOT NULL CHECK (length(target_id) BETWEEN 1 AND 64),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    address TEXT NOT NULL CHECK (length(address) BETWEEN 1 AND 512),
    account TEXT NOT NULL CHECK (length(account) <= 120),
    auto_deliver INTEGER NOT NULL DEFAULT 0 CHECK (auto_deliver IN (0, 1)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
, interface TEXT NOT NULL DEFAULT '' CHECK (length(interface) <= 120)) STRICT;

CREATE TABLE outputs (
    id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    profile_id TEXT NOT NULL REFERENCES "profiles"(id),
    target_id TEXT NOT NULL,
    format_key TEXT NOT NULL,
    renderer_id TEXT NOT NULL,
    renderer_version TEXT NOT NULL,
    target_revision TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    latest_artifact_id TEXT REFERENCES artifact_builds(id),
    previous_artifact_id TEXT REFERENCES artifact_builds(id), device_id TEXT REFERENCES devices(id) ON DELETE SET NULL, fqdn_group_prefix TEXT NOT NULL DEFAULT '',
    CHECK (latest_artifact_id IS NULL OR latest_artifact_id <> previous_artifact_id)
) STRICT;

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

CREATE TABLE "profile_list_domains" (
    profile_id TEXT NOT NULL REFERENCES "profiles"(id),
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    domains_json TEXT NOT NULL CHECK (length(domains_json) BETWEEN 2 AND 16384),
    PRIMARY KEY (profile_id, list_id)
) WITHOUT ROWID, STRICT;

CREATE TABLE "custom_lists" (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 63 AND id GLOB 'custom-*'),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    domains_json TEXT NOT NULL CHECK (length(domains_json) BETWEEN 2 AND 16384),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

CREATE TABLE "list_disabled_sources" (
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    source_id TEXT NOT NULL CHECK (length(source_id) BETWEEN 1 AND 64),
    PRIMARY KEY (list_id, source_id)
) WITHOUT ROWID, STRICT;

CREATE TABLE custom_sources (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 63 AND id GLOB 'feed-*'),
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    url TEXT NOT NULL CHECK (length(url) BETWEEN 12 AND 2048),
    format TEXT NOT NULL CHECK (format IN ('text','json','domain-list')),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

CREATE TABLE "list_domain_verdicts" (
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    domain TEXT NOT NULL CHECK (length(domain) BETWEEN 1 AND 253),
    verdict TEXT NOT NULL CHECK (verdict IN ('include','exclude')),
    PRIMARY KEY (list_id, domain)
) WITHOUT ROWID, STRICT;

CREATE TABLE custom_categories (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 63 AND id GLOB 'custom-*'),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

CREATE TRIGGER custom_categories_identity_immutable BEFORE UPDATE OF id, created_at_ns ON custom_categories BEGIN
    SELECT RAISE(ABORT, 'immutable custom category identity');
END;

CREATE TABLE category_memberships (
    category_id TEXT NOT NULL CHECK (length(category_id) BETWEEN 1 AND 64),
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    state TEXT NOT NULL CHECK (state IN ('added','removed')),
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (category_id, list_id),
    CHECK (state = 'added' OR category_id NOT GLOB 'custom-*')
) WITHOUT ROWID, STRICT;

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
    created_by_routevane INTEGER NOT NULL CHECK (created_by_routevane IN (0,1)), description TEXT NOT NULL DEFAULT ''
    CHECK (length(CAST(description AS BLOB)) <= 96),
    PRIMARY KEY (scope_id,prefix)
) WITHOUT ROWID, STRICT;

CREATE TABLE managed_route_claims (
    scope_id INTEGER NOT NULL,
    output_id TEXT NOT NULL REFERENCES outputs(id) CHECK (length(output_id)=32 AND output_id NOT GLOB '*[^0-9a-f]*'),
    prefix TEXT NOT NULL CHECK (length(prefix) BETWEEN 3 AND 64), description TEXT NOT NULL DEFAULT ''
    CHECK (length(CAST(description AS BLOB)) <= 96), labels_json TEXT NOT NULL DEFAULT '[]'
    CHECK (length(CAST(labels_json AS BLOB)) BETWEEN 2 AND 4194304 AND json_valid(labels_json) AND json_type(labels_json)='array'),
    PRIMARY KEY (scope_id,output_id,prefix),
    FOREIGN KEY (scope_id,prefix) REFERENCES managed_routes(scope_id,prefix)
) WITHOUT ROWID, STRICT;

CREATE TABLE "profile_list_priorities" (
    profile_id TEXT NOT NULL REFERENCES "profiles"(id),
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    position INTEGER NOT NULL CHECK (position BETWEEN 0 AND 127),
    PRIMARY KEY (profile_id, list_id),
    UNIQUE (profile_id, position)
) WITHOUT ROWID, STRICT;

CREATE TABLE "library_list_priorities" (
    list_id TEXT NOT NULL CHECK (length(list_id) BETWEEN 1 AND 64),
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (list_id),
    UNIQUE (position)
) WITHOUT ROWID, STRICT;

CREATE TABLE "catalog_removals" (
    kind TEXT NOT NULL CHECK (kind IN ('category','list')),
    id TEXT NOT NULL CHECK (length(id) BETWEEN 1 AND 63 AND id NOT GLOB 'custom-*'),
    removed_at TEXT NOT NULL CHECK (length(removed_at) BETWEEN 1 AND 64),
    PRIMARY KEY (kind, id)
) WITHOUT ROWID, STRICT;

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

CREATE TABLE managed_fqdn_groups (
 endpoint TEXT NOT NULL,
 output_id TEXT NOT NULL REFERENCES outputs(id),
 name TEXT NOT NULL,
 interface TEXT NOT NULL,
 state_json TEXT NOT NULL,
 PRIMARY KEY(endpoint,name)
) STRICT;

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
