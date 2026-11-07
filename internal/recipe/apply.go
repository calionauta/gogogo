// Spike 3: install and remove are mechanical, and both are the
// same operation over a plan plus a receipt.
//
// The claim under test: installing and removing a recipe touches nothing but the
// copied tree, the receipt and the tree's own files — never a core file, never a
// string anchor. If that holds, the anchor machinery has no role in extension
// distribution and the recipe model is installable.
package recipe

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReceiptDir is where receipts live, repo-relative. One file per installed recipe.
const ReceiptDir = ".recipes"

// Permissions for written recipe files. Recipe code is source, not secrets, so
// 0644 matches a git checkout; directories follow the scaffold's 0755.
const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// ReceiptPath returns the receipt location for a recipe id.
func ReceiptPath(id string) string { return filepath.Join(ReceiptDir, id+".yml") }

// SaveReceipt writes the receipt atomically. It refuses to overwrite a receipt
// whose id differs from the one in the file it would replace, because that is
// how a rename turns into two recipes silently claiming the same files.
func SaveReceipt(root string, r Receipt) error {
	if r.ID == "" {
		return errors.New("recipe: receipt needs an id")
	}
	full := filepath.Join(root, ReceiptPath(r.ID))
	if err := os.MkdirAll(filepath.Dir(full), dirPerm); err != nil {
		return err
	}
	if prev, err := LoadReceipt(root, r.ID); err == nil && prev.ID != r.ID {
		return fmt.Errorf("recipe: %s already holds receipt for %q", ReceiptPath(r.ID), prev.ID)
	}
	raw, err := yaml.Marshal(r)
	if err != nil {
		return err
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, raw, filePerm); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

// LoadReceipt reads a recipe's receipt.
func LoadReceipt(root, id string) (Receipt, error) {
	var r Receipt
	raw, err := os.ReadFile(filepath.Join(root, ReceiptPath(id)))
	if err != nil {
		return r, err
	}
	if err := yaml.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("recipe: parse %s: %w", ReceiptPath(id), err)
	}
	return r, nil
}

// ListReceipts returns every installed recipe id, sorted, so the composition is
// enumerable without a separate composition file.
func ListReceipts(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ReceiptDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".yml"))
	}
	sort.Strings(ids)
	return ids, nil
}

// ApplyResult reports what an Apply did, so a caller can print a receipt rather
// than claim success.
type ApplyResult struct {
	Written []string
	Removed []string
	Skipped []string
	Blocked []string
}

// Apply executes a plan. It refuses to run when the plan is blocked, which is the
// single most important property of this function: a conflict must never be
// partially applied.
func Apply(root string, plan PlanResult) (ApplyResult, error) {
	var res ApplyResult
	if plan.Blocked() {
		return res, fmt.Errorf("recipe: refusing to apply a blocked plan (%d conflict(s))", len(plan.Conflicts))
	}
	for _, op := range plan.Ops {
		full := filepath.Join(root, filepath.FromSlash(op.Path))
		switch op.Kind {
		case OpWrite, OpFastForward:
			if err := os.MkdirAll(filepath.Dir(full), dirPerm); err != nil {
				return res, err
			}
			if err := os.WriteFile(full, []byte(op.Content), filePerm); err != nil {
				return res, err
			}
			res.Written = append(res.Written, op.Path)
		case OpObsolete:
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return res, err
			}
			res.Removed = append(res.Removed, op.Path)
		case OpKeep, OpGone:
			res.Skipped = append(res.Skipped, op.Path)
		case OpConflict:
			// unreachable: a blocked plan returns above. Kept so a new OpKind
			// cannot silently fall through to "do nothing".
			res.Blocked = append(res.Blocked, op.Path)
		}
	}
	return res, nil
}

// NewReceipt builds the receipt a fresh install writes: the pristine hashes come
// from the content actually written, which is what makes every later drift and
// update question answerable.
func NewReceipt(id string, up Upstream, decl Declaration, written map[string]string) Receipt {
	r := Receipt{ID: id, Upstream: up, Files: map[string]string{}, Declaration: decl}
	for p, body := range written {
		r.Files[p] = Hash([]byte(body))
	}
	return r
}

// ReceiptAfter returns the receipt an applied plan leaves behind: the pristine
// baseline moves for every file the plan wrote, entries disappear for files
// upstream dropped, and an entry the plan kept stays exactly as it was — the
// user's bytes never become the baseline.
//
// This is the update half of NewReceipt. Calling NewReceipt on the plan's writes
// would drop the baseline of every file the update did not touch, which turns the
// next drift check into "everything is new" and the next update into a conflict.
func ReceiptAfter(prev Receipt, plan PlanResult) Receipt {
	out := Receipt{ID: prev.ID, Upstream: prev.Upstream, Declaration: prev.Declaration, Files: map[string]string{}}
	maps.Copy(out.Files, prev.Files)
	for _, op := range plan.Ops {
		switch op.Kind {
		case OpWrite, OpFastForward:
			out.Files[op.Path] = Hash([]byte(op.Content))
		case OpObsolete:
			delete(out.Files, op.Path)
		case OpKeep, OpGone:
			// Keep: the baseline is still what install wrote. Gone: the user
			// deleted the file, and the entry stays so drift keeps saying so.
		case OpConflict:
			// Unreachable on an applied plan (Apply refuses a blocked one);
			// leaving the entry untouched is the safe default if it ever is.
		}
	}
	return out
}

// Remove is uninstall: it deletes only the files the receipt claims AND that are
// still pristine. A modified file is left behind and reported, because deleting
// something the user wrote is the one thing uninstall must never do.
//
// Data is never dropped here. Schema removal is an explicit, separate purge — the
// rule the research section established from Shopware, Nextly and the migration
// literature alike.
func Remove(root string, r Receipt) (ApplyResult, error) {
	var res ApplyResult
	d, err := Check(root, r)
	if err != nil {
		return res, err
	}
	for _, f := range d.Files {
		switch f.State {
		case StatePristine:
			full := filepath.Join(root, filepath.FromSlash(f.Path))
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return res, err
			}
			res.Removed = append(res.Removed, f.Path)
		case StateModified:
			res.Skipped = append(res.Skipped, f.Path)
		case StateDeleted:
			// already gone; nothing to do
		}
	}
	return res, nil
}
