# Migration notes

Curated notes for releases whose changes a changelog bullet cannot carry: a
replaced layer, a moved seam, a renamed concept. `gogogo migrate` reads this
index at the tag it is migrating to and prints the notes that fall inside the
range, after the release list and before the proof gates.

One line per release, `- vX.Y.Z — one sentence`. The note itself is
`docs/migrations/vX.Y.Z.md`. This directory is repo-facing and is not published
on the docs site: it exists so a project that skipped several releases finds the
mapping instead of history.

- v0.44.0 — the flat stack replaced PocketBase, one layer per concern
