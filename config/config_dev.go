//go:build dev

// SCOPE:core - DO NOT REMOVE - Development defaults.
package config

import "os"

func init() {
	defaultDev := map[string]string{
		"ENVIRONMENT": "development",
		"LOG_LEVEL":   "DEBUG",
	}

	for k, v := range defaultDev {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
