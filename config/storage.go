// Storage location env knobs, split from config.go for the
// function-length gate. The database token stays in
// config.go (the secrets owner file); only the non-secret
// locations live here.
package config

import "path/filepath"

// loadStorageEnv fills the storage location from env.
//
// The defaults live UNDER DataDir, which is the contract docs/configuration.md
// states. They used to be the literal "data/…", which made DATA_DIR half a
// setting: moving the data dir moved the queues, NATS and the credentials but
// left the database behind at ./data/app.db — so two processes on different
// DATA_DIRs silently shared one SQLite file. That is a data-loss coupling, not
// a cosmetic default.
//
// An explicit TURSO_DB_PATH still wins: a deploy that pins the path is never
// moved by DATA_DIR.
func (c *Config) loadStorageEnv() {
	c.Turso.URL = getEnv("TURSO_DATABASE_URL", "")
	c.Turso.DBPath = getEnv("TURSO_DB_PATH", filepath.Join(c.DataDir, "app.db"))
	c.ChiAddr = getEnv("CHI_ADDR", "")
}
