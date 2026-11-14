// Feature presence, derived from the tree.
//
// WHY A HELPER INSTEAD OF A LIST: every browser suite belongs to ONE feature —
// the whiteboard suite drives /whiteboard, the notes suite drives /notes, the
// chat suite drives /chat, the SSE opener suite probes /todo. On a trimmed
// checkout that feature is gone, and a suite that keeps running fails on a
// route that no longer exists: a false red that costs CI minutes and teaches
// people to ignore the gate.
//
// But a suite that skips SILENTLY is worse than a false red — it looks like
// coverage that is not there. So the rule is:
//
//   1. derive presence from the FILESYSTEM (the unit's own directory);
//   2. when it is absent, print the skip WITH ITS REASON and exit 0;
//   3. never infer it from a hand-kept array — that is the drift this file
//      exists to remove.
//
// Why the skip cannot hide a real regression: if the directory is gone, the
// code is gone, and the build itself fails if the generated registration still
// imported that package (a compile error, not a runtime one). A tree cannot
// mount a feature whose package is not in it. The skip therefore means "there
// is nothing here to drive", never "something broke".
//
// The same derivation answers "which pages exist" for the smoke suite, so both
// read one source and one test (scripts/lib/features.test.mjs).
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// repoRoot is the checkout root, derived from THIS file's location (two levels
// up from scripts/lib) so it does not depend on the caller's working
// directory — CI runs the suites from the root, a developer may not.
export const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

// featurePresent reports whether a unit's slice is in this tree. An installed
// recipe copies into `features/<id>` too, so one check answers both.
export function featurePresent(root, id) {
  return existsSync(join(root, "features", id));
}

// routeOwner maps a page route to the unit that mounts it. `login` is the auth
// page set the todo slice declares (/login, /signup); the rest are their own
// slice. The smoke suite uses it to decide which pages to visit.
export const routeOwner = {
  todo: "todo",
  chat: "chat",
  whiteboard: "whiteboard",
  room: "room",
  login: "todo",
  review: "review",
};

// deriveFeatures returns the features present in this tree, in the order the
// caller asked for. An empty result means the tree carries none of them: the
// caller must fail loudly rather than "pass" over everything.
export function deriveFeatures(root, names) {
  return names.filter((name) => featurePresent(root, routeOwner[name] ?? name));
}

// skipIfFeatureAbsent returns true when the suite must skip, printing the
// reason. The caller exits 0 right after — a skip is a fact about the tree, not
// a verdict about the code.
export function skipIfFeatureAbsent(root, id) {
  if (featurePresent(root, id)) {
    return false;
  }
  console.log(
    `⏭ skipped: the ${id} feature is not in this tree ` +
      `(features/${id} absent) — there is nothing to drive`,
  );
  return true;
}
