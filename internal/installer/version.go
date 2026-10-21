// SCOPE:plugin — installer engine: version parsing and ordering
//
// Two callers need ordering, and neither needs a dependency: the update check
// ("is this binary behind the latest release?") and the migration range
// ("which releases sit between the tree's recorded revision and now?").
//
// The comparison is semver precedence, restricted to the shape this project's
// tags actually take. Anything else reports "not comparable" instead of
// guessing an order — an unknown version that sorts itself above a release is
// worse than an honest "unknown".
package installer

import (
	"strconv"
	"strings"
)

// Statuses the update check reports. They are a contract in the JSON output,
// so they are constants rather than inline strings.
const (
	statusBehind  = "behind"
	statusCurrent = "current"
	statusAhead   = "ahead"
	statusUnknown = "unknown"
)

// semver is a parsed release tag: MAJOR.MINOR.PATCH with an optional
// pre-release suffix. Build metadata (+sha) is dropped, as semver says.
type semver struct {
	major, minor, patch int
	pre                 string
}

// parseSemver accepts "0.53.0", "v0.53.0", "0.53.0-rc.1", with or without
// build metadata. Two-part and one-part numbers are rejected on purpose: this
// project always ships three, and accepting "0.53" would silently order it
// above "0.53.1".
func parseSemver(raw string) (semver, bool) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "v"))
	if s == "" {
		return semver{}, false
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	pre := ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		nums[i] = n
	}
	return semver{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, true
}

// compareSemver returns -1, 0 or 1 for a<b, a==b, a>b in semver precedence.
func compareSemver(a, b semver) int {
	for _, d := range [][2]int{
		{a.major, b.major},
		{a.minor, b.minor},
		{a.patch, b.patch},
	} {
		switch {
		case d[0] < d[1]:
			return -1
		case d[0] > d[1]:
			return 1
		}
	}
	// A release outranks every pre-release of the same number, which is what
	// keeps "0.54.0-rc.1 installed, 0.54.0 available" from reading as current.
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	case a.pre < b.pre:
		return -1
	default:
		return 1
	}
}

// versionOrder is the string-level wrapper. ok=false means at least one side
// is not a comparable version ("dev", "", a date, a branch name).
func versionOrder(a, b string) (int, bool) {
	pa, ok := parseSemver(a)
	if !ok {
		return 0, false
	}
	pb, ok := parseSemver(b)
	if !ok {
		return 0, false
	}
	return compareSemver(pa, pb), true
}

// versionStatus classifies one version against another.
func versionStatus(installed, latest string) string {
	cmp, ok := versionOrder(installed, latest)
	if !ok {
		return statusUnknown
	}
	switch {
	case cmp < 0:
		return statusBehind
	case cmp > 0:
		return statusAhead
	default:
		return statusCurrent
	}
}
