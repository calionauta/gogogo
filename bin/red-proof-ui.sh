#!/usr/bin/env bash
# Red-proof the guards added for the one-skin/one-shell change: mutate the
# source so each guard's defect is real, watch the test fail, restore, watch it
# pass again. The mutation is never left behind.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

pass=0
fail=0
bak=$(mktemp -d)
trap 'rm -rf "$bak"' EXIT

prove() {
  local name="$1" file="$2" python_mutation="$3" test_pkg="$4" test_run="$5"
  cp "$file" "$bak/$(basename "$file")"
  python3 -c "$python_mutation" || { echo "  ✗ $name: mutation script failed"; fail=$((fail + 1)); return; }
  # A .templ edit only reaches the rendered page after regeneration: the server
  # renders from the generated *_templ.go, so without this the mutation would be
  # invisible and the guard would look blind.
  if [ "${file##*.}" = "templ" ]; then
    go tool templ generate ./... >/dev/null 2>&1 || { echo "  ✗ $name: templ generate failed"; fail=$((fail + 1)); return; }
  fi
  if go test "$test_pkg" -run "$test_run" -count=1 >/tmp/redproof.log 2>&1; then
    echo "  ✗ $name: test still PASSED with the defect present (guard is blind)"
    fail=$((fail + 1))
  else
    echo "  ✓ $name: failed as required — $(grep -m1 -E 'FAIL:|no_legacy|dead markup|mentions|does not compile|lists|carries' /tmp/redproof.log | sed 's/^ *//' | cut -c1-110)"
  fi
  cp "$bak/$(basename "$file")" "$file"
  if [ "${file##*.}" = "templ" ]; then
    go tool templ generate ./... >/dev/null 2>&1 || echo "    ⚠ templ generate after restore failed"
  fi
  if go test "$test_pkg" -run "$test_run" -count=1 >/tmp/redproof2.log 2>&1; then
    echo "    (restored: green)"
    pass=$((pass + 1))
  else
    echo "    ✗ restore did not go green — inspect $file"
    fail=$((fail + 1))
  fi
}

echo "→ Red-proofing the no-legacy / no-dead-markup / theme-catalog guards"

# 1. a removed surface named in a doc
prove "removed brand in docs" "docs/ui-skins.md" "
import pathlib
p = pathlib.Path('docs/ui-skins.md'); s = p.read_text()
p.write_text(s + '\nThe old Basecoat skin did this differently.\n')
" ./internal/capabilities TestNoRemovedBrandsInSourcesOrDocs

# 2. a removed surface named in a Go comment
prove "removed brand in code" "internal/ui/modules.go" "
import pathlib
p = pathlib.Path('internal/ui/modules.go'); s = p.read_text()
p.write_text(s.replace('// Module is one destination in the shell.', '// Module is one destination (was Basecoat-era markup).'))
" ./internal/capabilities TestNoRemovedBrandsInSourcesOrDocs

# 3. a dead markup attribute back on an element
prove "dead attribute in markup" "internal/ui/shell.templ" "
import pathlib
p = pathlib.Path('internal/ui/shell.templ'); s = p.read_text()
p.write_text(s.replace('class=\"app-body\"', 'class=\"app-body\" data-variant=\"ghost\"', 1))
" ./internal/capabilities TestNoDeadMarkupAttributes

# 3b. a removed surface named in an extensionless build file (the first sweep
# missed these: Makefile/Dockerfile carry no extension to match on)
prove "removed brand in Makefile" "Makefile" "
import pathlib
p = pathlib.Path('Makefile'); s = p.read_text()
p.write_text(s + '\n# was driven by basecoat\n')
" ./internal/capabilities TestNoRemovedBrandsInSourcesOrDocs

# 4. catalog theme the bundle does not compile
prove "catalog theme without CSS" "internal/themes/themes.go" "
import pathlib
p = pathlib.Path('internal/themes/themes.go'); s = p.read_text()
p.write_text(s.replace('\t{Name: \"dim\", Label: \"Zinc\", Dark: true},', '\t{Name: \"dim\", Label: \"Zinc\", Dark: true},\n\t{Name: \"night\", Label: \"Noite\", Dark: true},'))
" ./internal/themes TestCatalogMatchesCompiledThemes

# 5. bundle theme missing from the catalog
prove "CSS theme unreachable" "src/css/input.css" "
import pathlib
p = pathlib.Path('src/css/input.css'); s = p.read_text()
p.write_text(s.replace('themes: light --default, dark, lofi, dim;', 'themes: light --default, dark, lofi, dim, corporate;'))
" ./internal/themes TestCatalogMatchesCompiledThemes

# 6. duplicate catalog entry
prove "duplicate catalog entry" "internal/themes/themes.go" "
import pathlib
p = pathlib.Path('internal/themes/themes.go'); s = p.read_text()
p.write_text(s.replace('\t{Name: \"dim\", Label: \"Zinc\", Dark: true},', '\t{Name: \"dim\", Label: \"Zinc\", Dark: true},\n\t{Name: \"dim\", Label: \"Zinc 2\", Dark: true},'))
" ./internal/themes TestCatalogIsWellFormed

# 7. the page contract a revamp must not break
prove "todo list patch root renamed" "features/todo/components/todo_list.templ" "
import pathlib
p = pathlib.Path('features/todo/components/todo_list.templ'); s = p.read_text()
# target the ELEMENT, not the two doc comments that also quote the id
p.write_text(s.replace('class=\"divide-y divide-base-200\" id=\"todo-list\"', 'class=\"divide-y divide-base-200\" id=\"todo-list-renamed\"', 1))
" ./internal/server TestPagesContract

# 8. admin-only module leaking into a non-admin sidebar
prove "admin module visible to viewers" "internal/ui/modules.go" "
import pathlib
p = pathlib.Path('internal/ui/modules.go'); s = p.read_text()
p.write_text(s.replace('URL: \"/config\", Admin: true}', 'URL: \"/config\"}'))
" ./internal/ui TestGroupedHidesAdminModulesFromNonAdmins

echo
echo "red-proof: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
