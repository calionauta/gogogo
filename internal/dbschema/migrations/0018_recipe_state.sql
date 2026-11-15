-- Runtime enable/disable state for installed recipes.
--
-- Install/uninstall is a BUILD fact (the code is copied or removed); enabled/
-- disabled is a RUNTIME fact (the operator can keep the code and stop mounting
-- it). One row per recipe, written only by the admin toggle. The table is core:
-- it owns no domain data, and a recipe must never own the switch that controls
-- another recipe.
CREATE TABLE IF NOT EXISTS recipe_state (
    id         TEXT PRIMARY KEY,
    enabled    INTEGER NOT NULL DEFAULT 1,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);
