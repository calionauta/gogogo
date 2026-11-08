// Storage location env knobs, split from config.go for the
// function-length gate. The database token stays in
// config.go (the secrets owner file); only the non-secret
// locations live here.
package config

// loadStorageEnv fills the storage location from env.
func (c *Config) loadStorageEnv() {
	c.Turso.URL = getEnv("TURSO_DATABASE_URL", "")
	c.Turso.DBPath = getEnv("TURSO_DB_PATH", "data/app.db")
	c.ChiAddr = getEnv("CHI_ADDR", "")
}
