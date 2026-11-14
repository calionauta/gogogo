// Localization unit entry, kept beside the registry so the
// table stays under the file-size gate.
package capabilities

var i18nUnit = Capability{
	ID:         "i18n",
	Kind:       KindServant,
	Summary:    "localization seam (embedded catalogs, locale resolution, number/date formatting)",
	RuntimeOff: "I18N_ENABLED=false",
	Dirs:       []string{"internal/i18n"},
	Warns: []string{
		"With the servant off, markup stays on English strings and the " +
			"account panel offers no language picker (there is no catalog to " +
			"offer), so no page depends on the locale context to render.",
	},
	Note: "Removable seam: delete internal/i18n plus this entry and the " +
		"I18n config; the shell keeps rendering the English fallback. " +
		"Runtime-off is the blessed path until a phase converts markup.",
	Reason: "Phase one owns lookup and formatting; CLDR depth is a " +
		"documented revisit trigger, not an upfront dependency.",
}
