// Auth-gated read-only /config view (masked secrets)
//
// SafeView builds the read-only data shape that the /config page
// renders. Field-group boundaries in the schema mirror the
// top-level sub-structs of *config.Config so operators can correlate
// "what's set in .env" with "what's wired in code" at a glance.
//
// Two safety rules govern the output:
//   - Secret-shaped fields (Token, Key, URL-with-creds) only ever
//     surface as masked — never the literal value.
//   - AGE_SECRET_KEY is excluded entirely; we render a boolean only.
//     Any agent running with shell access can `env | grep AGE_`, but
//     we deliberately don't pipe it to a browser.
package components

import (
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/calionauta/gogogo/config"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/recipe"
)

// ageSecretKey is the constant field name shown on the /config
// page for the master age-decryption key (boolean-only).
//
// Note: group-name constants (groupApp, groupStorage, …) live in
// mask.go where they back envMap; the same package, one block.
const (
	ageSecretKey = "AGESecretKey"
	secretsGroup = "Secrets"
)

// Row is one entry on the /config page: a label, the env var name
// the operator can grep, and a Display variant that the templ
// layer switches on.
type Row struct {
	Group       string
	Key         string // field name as seen in code, e.g. "APIKey"
	Env         string // env var name, e.g. "GOAI_API_KEY"
	Display     string // value to render when group/key policy allows
	Masked      bool   // true when Display is a partial mask
	BoolOnly    bool   // true for fields like AGE_SECRET_KEY (no value at all)
	Set         bool   // true when the underlying env-backed string was non-empty
	Description string // one-liner, sourced from config.go's header comment
	URLShape    bool   // true when Display is a URL we trust (LeafNodeURL after masking)
}

// PageData holds the rows grouped for rendering. Strings are
// pre-grouped so the .templ stays presentation-only.
type PageData struct {
	AppName    string
	BuildLabel string
	Groups     []Group
	// Status is the at-a-glance list (environment, enforcement, keys,
	// engines): derived from the same cfg as Groups, so the card and the
	// tables cannot disagree.
	Status []StatusItem
	// StatusGroups is Status arranged under its column captions, so the
	// template stays presentation-only.
	StatusGroups []StatusGroup
	// SlotSections is the `settings.section` slot: content declared by recipes,
	// rendered inside this page's own frame. The page never learns a recipe id,
	// which is the slot contract's whole point.
	SlotSections []recipe.Fragment
	// AnswerCatalog/AnswerPanels: answer-components data (genui-owned, trimmed with the unit).
	AnswerCatalog []comp.ComponentInfo
	AnswerPanels  []comp.ComponentInfo
	// Role is the viewer's users.role at render time ("" when unknown).
	// Display-only: enforcement stays in the RBAC middleware. The page
	// template pairs it with RBACEnabled so the Casbin posture is
	// visible where it is inspected, not just in policy.csv.
	Role string
	// RBACEnabled mirrors cfg.RBACEnabled: pass-through when false,
	// enforced denies when true.
	RBACEnabled bool
	// Units is the inventory of everything this binary ships, grouped by
	// removal class (Core / Servants / Features). Status is "in" (always
	// shipped) or the unit's RuntimeOff switch text (opt-out); whether a
	// switch is currently off is the Runtime tab's job, not guessed here.
	Units []UnitGroup
	// Users is the admin-only account table. Empty in BuildPageData; the
	// handler fills it from the user store.
	Users            []UserRow
	AdminStepUp      bool
	AdminPasswordSet bool
	Events           []SecurityEventRow
	EventCounts      []SecurityEventCount
	EventNames       []string
	EventFilter      string
	PrevURL          string
	NextURL          string
	CSVURL           string
	AuditAlert       string
	Notice           string
	Error            string
	ComponentFilter  string
}

// UserRow is one row of the admin user table.
type UserRow struct {
	ID       string
	Email    string
	Role     string
	Created  string
	Verified bool
	TOTP     bool
}

// SecurityEventRow is one audit row rendered on the admin page.
type SecurityEventRow struct {
	Event   string
	Reason  string
	Email   string
	UserID  string
	ActorID string
	Created string
}

// SecurityEventCount is one 24h aggregate.
type SecurityEventCount struct {
	Event  string
	Reason string
	Count  int64
}

// Group is a labelled slice of Rows. Order is stable across
// renders so operators see the same ordering they know from the
// file tree.
type Group struct {
	Name string
	Rows []Row
	Note string // optional HTML-safe note (e.g. sensitivity caveat)
}

// UnitGroup is one removal-class group on the Units tab: Core, Servants or
// Features. The label comes from capabilities.KindGroupLabel — the class has
// exactly one name, so the page cannot drift from the registry.
type UnitGroup struct {
	Label string
	Rows  []UnitRow
}

// UnitStatus is a unit's state in this build. It is compared and rendered, so
// the two exact-match values are constants: a typo in the template's `== "in"`
// would otherwise silently change the badge.
//
// The opt-out value carries the switch text, so it is not a constant; the
// prefix is (UnitStatusOptOut) and the row builds the rest.
type UnitStatus string

const (
	// UnitStatusIn is a unit the binary mounts with nothing to configure.
	UnitStatusIn UnitStatus = "in"
	// UnitStatusRemoved is a feature whose provider was uninstalled or is
	// absent from this build.
	UnitStatusRemoved UnitStatus = "removed"
	// UnitStatusOptOut prefixes the runtime switch that disables the unit.
	UnitStatusOptOut UnitStatus = "opt-out: "
)

// UnitRow is one unit in the inventory: its id, a one-line summary and its
// status.
type UnitRow struct {
	ID      string
	Summary string
	Status  UnitStatus
	Kind    capabilities.Kind
}

// MarkRemovedUnits corrects the registry's default for what the binary
// actually mounted: a recipe with no provider in this build is not "in" —
// it was uninstalled (its registry row stays as removal-class data) or
// switched off at boot. Switch-gated rows keep their opt-out text: for
// them, unmounted is the documented off state, not a removal. Features only —
// Core/Servants are always wired when the row exists.
func MarkRemovedUnits(data *PageData, mounted map[string]bool) {
	for i := range data.Units {
		for j := range data.Units[i].Rows {
			row := &data.Units[i].Rows[j]
			if row.Kind == capabilities.KindFeature && !mounted[row.ID] && row.Status == UnitStatusIn {
				row.Status = UnitStatusRemoved
			}
		}
	}
}

// buildUnits inventories the capability registry by removal class. A unit
// with a RuntimeOff switch is opt-out (the switch text says how); the rest
// are always in. Sorted by id per group so renders are stable.
func buildUnits() []UnitGroup {
	groups := make([]UnitGroup, 0, len(capabilities.Kinds))
	byKind := make(map[capabilities.Kind]int, len(capabilities.Kinds))
	for i, kind := range capabilities.Kinds {
		groups = append(groups, UnitGroup{Label: capabilities.KindGroupLabel(kind)})
		byKind[kind] = i
	}
	for _, u := range capabilities.All {
		idx, ok := byKind[u.Kind]
		if !ok {
			continue
		}
		status := UnitStatusIn
		if u.RuntimeOff != "" {
			status = UnitStatusOptOut + UnitStatus(u.RuntimeOff)
		}
		groups[idx].Rows = append(groups[idx].Rows, UnitRow{
			ID: u.ID, Summary: u.Summary, Status: status, Kind: u.Kind,
		})
	}
	for i := range groups {
		slices.SortFunc(groups[i].Rows, func(a, b UnitRow) int {
			return strings.Compare(a.ID, b.ID)
		})
	}
	return groups
}

// BuildPageData reads the runtime *config.Config and produces a
// redacted view. SAFE — never returns the original value of a
// masked field. No DB and no writes; the only filesystem touch is a
// read-only stat of the age secrets file, so the status card can tell
// "key missing" from "age never used here".
//
// AGE_SECRET_KEY is read from the process env (it never lands in
// the config.Config struct, only in the secrets loader). We surface
// its set-state but never its value.
func BuildPageData(cfg *config.Config) PageData {
	if cfg == nil {
		return PageData{}
	}

	ageSet := os.Getenv("AGE_SECRET_KEY") != ""
	status := StatusSummary(cfg, ageSet, ageSecretsFileExists(cfg.AppName))

	pd := PageData{
		AppName:       cfg.AppName,
		BuildLabel:    cfg.BuildLabel,
		RBACEnabled:   cfg.RBACEnabled,
		Status:        status,
		StatusGroups:  groupStatus(status),
		AnswerCatalog: catalogInfos(),
		AnswerPanels:  panelInfos(),
		Units:         buildUnits(),
	}

	// App group — never secret. Direct passthrough.
	pd.Groups = append(pd.Groups, Group{
		Name: "App",
		Rows: []Row{
			row("app", keyAppName, cfg.AppName, "Project name; used as the secrets-file scope."),
			row("app", keyBuildLabel, cfg.BuildLabel, "Build identifier. 'dev' for go run."),
			row("app", keyBuildCommit, cfg.BuildCommit, "Short git commit hash."),
			row("app", "Host", cfg.Host, "HTTP listen address."),
			row("app", "Port", intToStr(cfg.Port), "HTTP listen port."),
			row("app", "LogLevel", cfg.LogLevel, "log/slog level threshold."),
			rowBool("app", "Dev", cfg.Dev, "True when ENVIRONMENT != 'production'."),
		},
	})

	// Storage group — paths surfaced, encryption key masked.
	pd.Groups = append(pd.Groups, Group{
		Name: "Storage",
		Rows: []Row{
			row("storage", keyDataDir, cfg.DataDir, "Directory used by goqite, NATS, and DagNats."),
		},
	})

	// Admin group — token always masked.
	pd.Groups = append(pd.Groups, Group{
		Name: "Admin",
		Rows: []Row{
			rowSecret("admin", "AdminToken", cfg.AdminToken, "Master token for admin endpoints (masked)."),
		},
		Note: "Loaded from the age-decrypted secrets file. Never from the host env directly.",
	})

	// AI group — base URL + model surfaced, API key masked.
	// BaseURL/Model are read directly by internal/llm/goai.go, not
	// stored on the Config struct; we read os.Getenv here so the
	// /config page stays a single source of truth from the user's
	// perspective.
	pd.Groups = append(pd.Groups, Group{
		Name: "AI (GoAI)",
		Rows: []Row{
			rowSecret("ai", keyAPIKey, cfg.GoAI.APIKey, "LLM provider API key (masked)."),
			row("ai", "BaseURL", os.Getenv("GOAI_BASE_URL"), "OpenAI-compatible base URL."),
			row("ai", keyModel, os.Getenv("GOAI_MODEL"), "Model ID passed to the provider."),
		},
	})

	// Review group — jev judge endpoints surfaced, API keys masked.
	// Values come from cfg.Review (JEV_* env); the keys live in the
	// age-encrypted secrets file when set, keyless when empty.
	pd.Groups = append(pd.Groups, reviewGroup(cfg))

	// NATS group.
	pd.Groups = append(pd.Groups, Group{
		Name: "NATS", Rows: []Row{
			rowBool("nats", "Enabled", cfg.NATS.Enabled, "Embedded NATS JetStream toggle."),
			row("nats", "StoreDir", cfg.NATS.StoreDir, "Where NATS persists streams."),
			rowURL(
				"nats", "LeafNodeURL", cfg.NATS.LeafNodeURL,
				"Connect as Leaf Node when set; credentials inside the URL are masked.",
			),
		},
	})

	// DagNats group.
	pd.Groups = append(pd.Groups, Group{
		Name: "DagNats",
		Rows: []Row{
			rowBool("dagnats", "Enabled", cfg.DagNats.Enabled, "Durable workflow engine."),
			row("dagnats", "HTTPAddr", cfg.DagNats.HTTPAddr, "Console/API listen (separate port)."),
			row("dagnats", "NATSPort", intToStr(cfg.DagNats.NATSPort), "NATS port the engine owns."),
			row("dagnats", "StoreDir", cfg.DagNats.StoreDir, "Where DagNats persists workflows."),
		},
	})

	// Sync group — operational.
	pd.Groups = append(pd.Groups, Group{
		Name: "Sync",
		Rows: []Row{
			rowBool("sync", "OfflineEnabled", cfg.OfflineSync.Enabled, "Service Worker + NATS CRUD proxy."),
			row("sync", "TursoDBPath", cfg.Turso.DBPath, "Turso database file (URL env overrides)."),
		},
	})

	// Secrets group — boolean-only. AGE_SECRET_KEY literally never
	// leaves the host process.
	pd = appendSecretsGroup(pd, ageSet)

	return pd
}

// appendSecretsGroup adds the age-secrets row: boolean-only, the value never
// leaves the process.
func appendSecretsGroup(pd PageData, ageSet bool) PageData {
	pd.Groups = append(pd.Groups, Group{
		Name: secretsGroup,
		Rows: []Row{
			{
				Group:       groupSecrets,
				Key:         ageSecretKey,
				Env:         envFromKey(groupSecrets, ageSecretKey),
				BoolOnly:    true,
				Set:         ageSet,
				Description: "age-decryption master key for ~/.secrets/. Always boolean-only; the value never leaves the process.",
			},
		},
		Note: "The value of AGE_SECRET_KEY is not exposed by design. To rotate it, edit the file at ~/.secrets/<APP_NAME>.env.age.", //nolint:lll // single-line readability over column budget
	})
	return pd
}

// catalogInfos lists the generative answer components (everything but
// recipe panels): the fixed vocabulary the model fills with typed props.
// Sorted by Kind (catalog, container, custom) then Name — intuitive for
// operators scanning the list.
func catalogInfos() []comp.ComponentInfo {
	var out []comp.ComponentInfo
	for _, info := range comp.CatalogOverview() {
		if info.Kind == comp.KindNative {
			continue
		}
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b comp.ComponentInfo) int {
		if a.Kind != b.Kind {
			return strings.Compare(string(a.Kind), string(b.Kind))
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// panelInfos lists the installed recipe panels: the app's own data the
// model may answer with (slice chosen by the model, rows filled here).
// Sorted by Name for the same reason as catalogInfos.
func panelInfos() []comp.ComponentInfo {
	var out []comp.ComponentInfo
	for _, info := range comp.CatalogOverview() {
		if info.Kind != comp.KindNative {
			continue
		}
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b comp.ComponentInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// reviewGroup builds the jev judge section: primary + fallback
// endpoints surfaced, API keys masked. Split from BuildPageData for
// the funlen gate.
func reviewGroup(cfg *config.Config) Group {
	return Group{
		Name: "Review (Jev)",
		Rows: []Row{
			rowSecret(groupReview, keyAPIKey, cfg.Review.APIKey,
				"Primary judge API key (masked; empty = keyless free tier)."),
			row(groupReview, keyJevAPIURL, cfg.Review.APIURL,
				"Primary jev-compatible judge endpoint (THX-02 by default)."),
			row(groupReview, keyModel, cfg.Review.Model, "Primary judge model ID."),
			rowSecret(groupReview, keyJevFallbackAPIKey, cfg.Review.FallbackAPIKey,
				"Fallback judge API key (masked; empty = keyless demo)."),
			row(groupReview, keyJevFallbackAPIURL, cfg.Review.FallbackAPIURL,
				"Fallback endpoint, tried when the primary fails (SimpleJev demo by default)."),
			row(groupReview, keyJevFallbackModel, cfg.Review.FallbackModel,
				"Fallback judge model ID."),
			row(groupReview, keyJevTimeout, cfg.Review.Timeout.String(),
				"Per-attempt judge HTTP timeout (shared by both legs)."),
			row(groupReview, keyJevThreshold,
				strconv.FormatFloat(cfg.Review.ConfidenceThreshold, 'f', 2, 64),
				"Below this the verdict escalates to a human."),
		},
	}
}

// row builds a non-secret Row from a string value. The masking
// layer still runs (maskValue is a safe passthrough when
// safeValue==true) so future policy changes don't require touching
// every call site.
func row(group, key, value, desc string) Row {
	r := Row{
		Group:       group,
		Key:         key,
		Env:         envFromKey(group, key),
		Set:         value != "",
		Description: desc,
	}
	if value == "" {
		return r
	}
	r.Display = maskValue(group, key, value)
	return r
}

// rowSecret is the masked-only Row constructor. The Display is the
// partial mask, Masked=true. Empty values stay empty (so the
// template can render "not set" without "***").
func rowSecret(group, key, value, desc string) Row {
	r := Row{
		Group:       group,
		Key:         key,
		Env:         envFromKey(group, key),
		Set:         value != "",
		Description: desc,
	}
	if value == "" {
		return r
	}
	r.Masked = true
	r.Display = masked(value)
	return r
}

// rowURL is for the LeafNodeURL row only. It uses maskedURL so the
// scheme://host survives but credentials are stripped. Empty stays
// empty.
func rowURL(group, key, value, desc string) Row {
	r := Row{
		Group:       group,
		Key:         key,
		Env:         envFromKey(group, key),
		Set:         value != "",
		URLShape:    true,
		Description: desc,
	}
	if value == "" {
		return r
	}
	r.Masked = true
	r.Display = maskedURL(value)
	return r
}

// rowBool builds a "true/false — env name" row. Used for toggles
// where the value is its own meaning.
func rowBool(group, key string, value bool, desc string) Row {
	env := envFromKey(group, key)
	if value {
		return Row{Group: group, Key: key, Env: env, Set: true, Display: "true", Description: desc}
	}
	return Row{Group: group, Key: key, Env: env, Set: false, Display: "false", Description: desc}
}

// intToStr is a tiny convenience to keep the table builder terse.
// Empty when zero so the template can render "not set" cleanly.
func intToStr(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}
