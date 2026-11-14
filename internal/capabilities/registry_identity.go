// Capability registry entries for the account and operator
// surfaces.
//
// Split from registry.go for the file-size gate: the table grew past 500 lines
// and these entries are the identity/operator group, the same reason the
// delivery slice lives in its own file.
package capabilities

// identityUnits are appended to All after the core table: the signed-in
// account page and the admin-only config view. Both are user-facing and both
// depend on the sounds servant for the rail's mute control.
var identityUnits = []Capability{
	{
		ID:      "account",
		Kind:    KindFeature,
		Summary: "authenticated /account security page",
		DependsOn: []string{
			depSounds,
		},
		Dirs:     []string{"features/account"},
		Register: "features/account/handlers",
		Note:     "Core identity UX; remove manually with the menu link in features/authviews/controls.templ.",
	},
	{
		ID:      "config-view",
		Kind:    KindFeature,
		Summary: "admin-only /config view + user role/session actions",
		DependsOn: []string{
			depSounds,
		},
		Dirs:     []string{"features/config"},
		Register: "features/config/handlers",
		Reason:   "Delete the dir and its routes with the account-menu Config entry.",
		Warns: []string{
			"The Config rail and account-menu entries are removed by hand with the unit.",
		},
	},
}
