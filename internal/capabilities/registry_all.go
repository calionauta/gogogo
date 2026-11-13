// The assembled registry: base entries first, then the split unit groups.
package capabilities

var All = append(baseUnits, append(
	append(
		append(
			append(append([]Capability{}, coreExtraUnits...), identityUnits...),
			deliveryUnits...,
		),
		licenseUnits...,
	),
	append(append(append(perfUnits, dashboardUnits...), i18nUnit), append(graphUnits, coffeeUnits...)...)...,
)...)
