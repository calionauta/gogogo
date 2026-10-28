// SCOPE:feature — the /config runtime status card (rows, groups, tones).
//
// Split from safe_view.go for the file-size gate, along a real seam: safe_view
// builds the masked field tables, this file answers "is this thing on" and
// owns the one tone vocabulary the card's badges and dots share — so a row and
// its summary cannot drift apart.
package components

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/calionauta/gogogo/config"
)

// Status tones are the closed DaisyUI vocabulary the status card speaks.
// Declared once so StatusSummary and StatusBadgeClass cannot drift apart.
const (
	toneSuccess = "success"
	toneWarning = "warning"
	toneInfo    = "info"
	toneError   = "error"
	toneNeutral = "neutral"
)

// StatusItem is one row of the /config status card: the at-a-glance answer
// to "is this thing on" without scrolling to its table. Tone is a DaisyUI
// tone (success, warning, info, error, neutral) and State is the one-word
// answer. Group places the row under its column caption.
type StatusItem struct {
	Group string
	Label string
	State string
	Tone  string
	Hint  string
}

// StatusGroup is one captioned column of the status card. Grouping exists
// because eight identical badges in a row read as a tag cloud: the operator
// could not tell the security posture from the AI keys at a glance.
type StatusGroup struct {
	Name  string
	Items []StatusItem
}

// The card's captions, one per part of the runtime an operator checks.
const (
	statusGroupRuntime  = "Runtime"
	statusGroupAI       = "AI"
	statusGroupPlatform = "Platform"
)

// statusGroupOrder is the card's reading order. Declared once so the template
// cannot invent a fourth column the data never fills.
var statusGroupOrder = []string{statusGroupRuntime, statusGroupAI, statusGroupPlatform}

// StatusSummary derives the /config status card from the runtime config:
// one row per thing an operator checks first, grouped by the part of the
// runtime it describes. States are words, not bools, so the card reads
// without a legend.
//
// ageFile reports whether the age secrets file the loader would read exists.
// It separates "the key for a configured secret store is gone" (an error)
// from "this deployment never used age secrets" (neutral): the old single
// state painted a red MISSING on a healthy deployment that injects env
// directly, which is how a true statement becomes a false alarm.
func StatusSummary(cfg *config.Config, ageSet, ageFile bool) []StatusItem {
	items := runtimeStatus(cfg, ageSet, ageFile)
	items = append(items, aiStatus(cfg)...)
	items = append(items, platformStatus(cfg)...)
	return items
}

// runtimeStatus is the posture column: where this binary runs, whether it
// enforces the policy, and how its secrets arrive.
func runtimeStatus(cfg *config.Config, ageSet, ageFile bool) []StatusItem {
	env, envTone := "production", toneSuccess
	if cfg.Dev {
		env, envTone = "development", toneWarning
	}
	rbac, rbacTone := "enforced", toneSuccess
	if !cfg.RBACEnabled {
		rbac, rbacTone = "pass-through", toneWarning
	}
	return []StatusItem{
		{
			Group: statusGroupRuntime, Label: "Environment", State: env, Tone: envTone,
			Hint: "ENVIRONMENT=production, or development otherwise.",
		},
		{
			Group: statusGroupRuntime, Label: "RBAC", State: rbac, Tone: rbacTone,
			Hint: "RBAC_ENABLED: enforced denies, or pass-through.",
		},
		secretsStatus(cfg, ageSet, ageFile),
	}
}

// aiStatus is the AI column: whether a live model and a real judge are wired.
func aiStatus(cfg *config.Config) []StatusItem {
	ai, aiTone := "configured", toneSuccess
	if cfg.GoAI.APIKey == "" {
		ai, aiTone = "missing", toneNeutral
	}
	judge, judgeTone := "custom judge", toneInfo
	if cfg.Review.APIKey == "" {
		judge, judgeTone = "demo endpoint", toneNeutral
	}
	return []StatusItem{
		{
			Group: statusGroupAI, Label: "AI key", State: ai, Tone: aiTone,
			Hint: "GOAI_API_KEY: live model, or keyless demo answers.",
		},
		{
			Group: statusGroupAI, Label: "Review judge", State: judge, Tone: judgeTone,
			Hint: "JEV_API_KEY: custom judge, or keyless demo endpoint.",
		},
	}
}

// platformStatus is the engine column: the realtime, workflow and offline
// switches, all read straight from the runtime config.
func platformStatus(cfg *config.Config) []StatusItem {
	nats, natsTone := "on", toneSuccess
	if !cfg.NATS.Enabled {
		nats, natsTone = "off", toneNeutral
	}
	dag, dagTone := "on", toneSuccess
	if !cfg.DagNats.Enabled {
		dag, dagTone = "off", toneNeutral
	}
	syncState, syncTone := "on", toneSuccess
	if !cfg.OfflineSync.Enabled {
		syncState, syncTone = "off", toneNeutral
	}
	return []StatusItem{
		{
			Group: statusGroupPlatform, Label: "NATS", State: nats, Tone: natsTone,
			Hint: "Embedded NATS JetStream for realtime fan-out.",
		},
		{
			Group: statusGroupPlatform, Label: "DagNats", State: dag, Tone: dagTone,
			Hint: "Durable workflow engine on its own port.",
		},
		{
			Group: statusGroupPlatform, Label: "Offline sync", State: syncState, Tone: syncTone,
			Hint: "Service Worker + NATS CRUD proxy.",
		},
	}
}

// secretsStatus is the one row whose MISSING must be earned: see StatusSummary.
func secretsStatus(cfg *config.Config, ageSet, ageFile bool) StatusItem {
	secrets, secretsTone := "set", toneSuccess
	//nolint:gosec // G101: a UI hint naming an env var, not a credential.
	secretsHint := "AGE_SECRET_KEY: master key for ~/.secrets."
	switch {
	case ageSet:
	case ageFile:
		secrets, secretsTone = "missing", toneError
	default:
		secrets, secretsTone = "not in use", toneNeutral
		secretsHint = "No ~/.secrets/" + cfg.AppName +
			".env.age — this deployment supplies env another way."
	}
	return StatusItem{
		Group: statusGroupRuntime, Label: "Secrets key", State: secrets,
		Tone: secretsTone, Hint: secretsHint,
	}
}

// ageSecretsFileExists reports whether the age file the loader would decrypt
// exists. Read-only and best-effort: a missing home directory means "no file",
// which is the same state the loader treats as "age not configured".
func ageSecretsFileExists(appName string) bool {
	if appName == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, statErr := os.Stat(filepath.Join(home, ".secrets", appName+".env.age"))
	return statErr == nil
}

// groupStatus arranges items under statusGroupOrder. A group with no rows is
// omitted rather than rendered as an empty column.
func groupStatus(items []StatusItem) []StatusGroup {
	byName := make(map[string][]StatusItem, len(statusGroupOrder))
	for _, item := range items {
		byName[item.Group] = append(byName[item.Group], item)
	}
	out := make([]StatusGroup, 0, len(statusGroupOrder))
	for _, name := range statusGroupOrder {
		if len(byName[name]) == 0 {
			continue
		}
		out = append(out, StatusGroup{Name: name, Items: byName[name]})
	}
	return out
}

// StatusAttention counts the rows an operator should look at (warning or
// error). Info and neutral are postures, not problems: a keyless demo judge
// is a valid configuration, not something to fix.
func StatusAttention(items []StatusItem) int {
	n := 0
	for _, item := range items {
		if item.Tone == toneWarning || item.Tone == toneError {
			n++
		}
	}
	return n
}

// AttentionLabel is the summary badge's words. Grammar matters here: the
// badge is the first thing read, and "1 need attention" reads as a bug.
func AttentionLabel(n int) string {
	if n == 1 {
		return "1 needs attention"
	}
	return strconv.Itoa(n) + " need attention"
}

// StatusDotClass maps a StatusItem tone to DaisyUI's status-dot colour. The
// state word beside the dot always carries the meaning, so the dot is
// decoration and never the only signal.
func StatusDotClass(tone string) string {
	switch tone {
	case toneSuccess:
		return "status-success"
	case toneWarning:
		return "status-warning"
	case toneInfo:
		return "status-info"
	case toneError:
		return "status-error"
	default:
		return "status-neutral"
	}
}

// StatusBadgeClass maps a StatusItem tone to its DaisyUI badge class. One
// mapping, next to the tones StatusSummary emits, so the template cannot
// drift into a tone the card never sends.
func StatusBadgeClass(tone string) string {
	switch tone {
	case toneSuccess:
		return "badge-success"
	case toneWarning:
		return "badge-warning"
	case toneInfo:
		return "badge-info"
	case toneError:
		return "badge-error"
	default:
		return "badge-neutral"
	}
}
