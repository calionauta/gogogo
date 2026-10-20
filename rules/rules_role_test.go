package gorules

// roleFixtures are the RoleCompareOutsideRBAC cases. A rule that matches
// nothing fails open, so both the flagged shapes and the quiet ones are
// pinned here (see the harness rationale in rules_test.go).
func roleFixtures() []ruleFixture {
	return []ruleFixture{
		{
			name: "a bare role literal comparison is flagged",
			source: "package fixture\n\n" +
				"func gateRoleLit(role string) bool {\n\tif role == \"admin\" {\n\t\treturn true\n\t}\n\treturn false\n}\n",
			wantAtLeast: 1,
		},
		{
			name: "a negated role literal comparison is flagged",
			source: "package fixture\n\n" +
				"// rbacPkg stands in for the real internal package: the fixture\n" +
				"// cannot import an internal/ path from a throwaway module, and the\n" +
				"// rule matches on the OBJECT TEXT (rbac.*RoleX*), not the import.\n" +
				"var rbacPkg = struct{ RoleViewer, RoleAdmin string }{\"viewer\", \"admin\"}\n\n" +
				"func gateNegLit(role string) bool {\n\treturn role != rbacPkg.RoleViewer\n}\n",
			wantAtLeast: 1,
		},
		{
			name: "a session role comparison is flagged",
			source: "package fixture\n\n" +
				"var rbacPkgB = struct{ RoleViewer, RoleAdmin string }{\"viewer\", \"admin\"}\n\n" +
				"type session struct{ Role string }\n\n" +
				"func gateSessRole(sess session) bool {\n\treturn sess.Role == rbacPkgB.RoleAdmin\n}\n",
			wantAtLeast: 1,
		},
		{
			name: "a non-role string comparison stays quiet",
			source: "package fixture\n\n" +
				"func gateNonRole(kind string) bool {\n\treturn kind == \"admin\"\n}\n",
			wantAtLeast: 0,
		},
		{
			name: "comparing a role to a non-role literal stays quiet",
			source: "package fixture\n\n" +
				"func gateOtherLit(role string) bool {\n\treturn role == \"editor\"\n}\n",
			wantAtLeast: 0,
		},
	}
}
