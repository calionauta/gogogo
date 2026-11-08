# .recipes — what this project installed

An installed recipe leaves one thing here:

- **`<id>.yml`** — the receipt: the id, the version the recipe declared, the
  source it came from, the pristine hash of every file it wrote, and the
  surface it declared. It is committed, because it is the ownership record:
  without it nothing can tell a file the tool installed from a file the user
  wrote, and `recipe update` has no baseline. `recipe outdated` compares the
  recorded version with the latest reviewed catalog entry
  (`catalog/<id>/<version>.yaml`); a receipt installed without a version
  reports unknown.

The recipe's own DDL is not kept here. A recipe that owns tables ships
`migrations/**.sql` inside its package, and `recipe install` mirrors those files
flat into `internal/db/recipe_schema/` as `<id>__<migration>.sql`, which is the
shape `sqlc` reads (one level deep, no dot-directories).

Edit the code freely; leave the receipt alone. `gogogo recipe status` compares
the tree against it — and against the mirrored DDL — and `recipe uninstall` only
removes files whose bytes still match their pristine hash.
