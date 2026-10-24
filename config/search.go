// SCOPE:core — the search plugin's env knob (SEARCH_MODE), split from
// config.go for the file-size gate. Config keeps one Search field.
//
// The valid names live HERE, not in internal/search, because config is core
// and the plugin is removable: importing the plugin would make
// `gogogo trim search` break the build on a missing package. The plugin's
// New() stays the authority at wiring time, and internal/search carries a
// test that fails when these two lists diverge.
package config

import "log/slog"

// SearchMode* are the layer names SEARCH_MODE accepts: the SQL substring
// filter (works with no index and no extra write), the in-memory fuzzy
// matcher, and the BM25 postings ranking.
const (
	SearchModeSubstring = "substring"
	SearchModeFuzzy     = "fuzzy"
	SearchModeBM25      = "bm25"
)

// SearchConfig selects the todo search layer.
type SearchConfig struct {
	// Mode is SEARCH_MODE (default "substring"). An unknown value resets to
	// the default with a warning instead of failing the boot — the same
	// fail-safe shape loadChatRouterEnv uses: a typo degrades to the layer
	// that always works (and needs no index), never to an unbootable
	// server. The plugin still rejects an unknown mode it is handed, so a
	// divergence between this list and the plugin's shows up as a boot
	// error, not as silent substring search.
	Mode string
}

// loadSearchEnv fills Search from env.
func (c *Config) loadSearchEnv() {
	c.Search.Mode = normalizeSearchMode(getEnv("SEARCH_MODE", SearchModeSubstring))
}

// normalizeSearchMode maps anything unknown onto the default. Only the
// three named layers are selectable; "" (unset) is the default too, so a
// caller that never touches the env still gets working search.
func normalizeSearchMode(raw string) string {
	switch raw {
	case "", SearchModeSubstring, SearchModeFuzzy, SearchModeBM25:
		if raw == "" {
			return SearchModeSubstring
		}
		return raw
	default:
		slog.Warn("config: unknown SEARCH_MODE, using the substring layer",
			"mode", raw, "default", SearchModeSubstring)
		return SearchModeSubstring
	}
}
