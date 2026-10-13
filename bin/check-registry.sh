#!/bin/bash
# registry-conformance — pre-commit hook for the capability registry.
#
# Every package under internal/ + features/ must be owned by a capability
# (a new package without lifecycle metadata fails TestRegistryCoversPackages
# in CI — and inside trimmed preset trees, which run the product suite).
# Same class: the installer drift gate (TestCheckTreeAgainstRepoRoot)
# also ran CI-only, so a chi.go edit killing a trim marker went red
# there first (the dagnats addAfter anchor). Both are instant file-read
# tests — same implementation CI runs, not parallel reimplementations.
set -uo pipefail

go test ./internal/capabilities/ -run 'TestRegistryCoversPackages|TestRegistryIDsUniqueAndWellFormed|TestRegistryDependsOnResolves' -count=1
go test ./internal/installer/ -run 'TestCheckTreeAgainstRepoRoot' -count=1
