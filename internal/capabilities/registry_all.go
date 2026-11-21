// The assembled registry: base entries first, then the split unit groups.
package capabilities

// All is every unit the build knows about: the base list, then each split group
// in the order the groups were introduced. It is a builder rather than nested
// appends so the ordering is readable at a glance — the split is about where
// the literals live, not about a second registry.
var All = buildAll()

func buildAll() []Capability {
	groups := [][]Capability{
		baseUnits,
		coreExtraUnits,
		identityUnits,
		deliveryUnits,
		licenseUnits,
		perfUnits,
		dashboardUnits,
		{i18nUnit},
		graphUnits,
		kioskUnits,
		promptUnits,
		eventsUnits,
		featureUnits,
	}
	var out []Capability
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}
