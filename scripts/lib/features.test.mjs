// The feature-presence contract, as a test.
//
// This is the file that keeps the browser suites honest: the skip decision is
// derived from the tree, and a derivation that quietly returns "absent" would
// turn every suite into a no-op that still reports green. So the derivation is
// pinned here, in both directions — present means present, absent means absent,
// and an empty derivation is an error the caller must surface (the smoke suite
// does exactly that).
//
// Run: node --test scripts/lib/features.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { deriveFeatures, featurePresent, routeOwner, skipIfFeatureAbsent } from "./features.mjs";

function tree(...dirs) {
  const root = mkdtempSync(join(tmpdir(), "gogogo-features-"));
  for (const d of dirs) {
    mkdirSync(join(root, "features", d), { recursive: true });
  }
  return root;
}

test("a unit's own directory is what makes it present", () => {
  const root = tree("todo");
  assert.equal(featurePresent(root, "todo"), true);
  assert.equal(featurePresent(root, "chat"), false);
  rmSync(root, { recursive: true, force: true });
});

test("a recipe installed under features/ counts as present", () => {
  const root = tree();
  mkdirSync(join(root, "features", "timer"), { recursive: true });
  assert.equal(featurePresent(root, "timer"), true);
  rmSync(root, { recursive: true, force: true });
});

test("deriveFeatures keeps the requested order and drops the absent ones", () => {
  const root = tree("todo", "whiteboard");
  assert.deepEqual(deriveFeatures(root, ["todo", "chat", "whiteboard", "room"]), [
    "todo",
    "whiteboard",
  ]);
  rmSync(root, { recursive: true, force: true });
});

test("login is the todo slice's page set, so it follows todo", () => {
  assert.equal(routeOwner.login, "todo");
  const root = tree("todo");
  assert.deepEqual(deriveFeatures(root, ["login"]), ["login"]);
  const bare = tree();
  assert.deepEqual(deriveFeatures(bare, ["login"]), []);
  rmSync(root, { recursive: true, force: true });
  rmSync(bare, { recursive: true, force: true });
});

test("skipIfFeatureAbsent skips with a reason, and never skips what is there", () => {
  const root = tree("notes");
  const lines = [];
  const original = console.log;
  console.log = (line) => lines.push(line);
  try {
    assert.equal(skipIfFeatureAbsent(root, "notes"), false, "a present feature must never skip");
    assert.equal(skipIfFeatureAbsent(root, "whiteboard"), true, "an absent feature must skip");
  } finally {
    console.log = original;
  }
  assert.equal(lines.length, 1);
  assert.match(lines[0], /whiteboard/);
  assert.match(lines[0], /skipped/);
  rmSync(root, { recursive: true, force: true });
});
