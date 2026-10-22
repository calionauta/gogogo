// SCOPE:plugin — installer engine: which upstream files a migration touches,
// and whether they matter in THIS tree.
package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// compareFileLimit is GitHub's cap on the compare API's file list. Reaching it
// means the range is larger than the response: the report says so instead of
// presenting part of the work as all of it.
const compareFileLimit = 300

// How one changed file relates to THIS tree. The names are the verdict, not a
// category: a file in a unit you trimmed is work you must NOT do.
const (
	impactCore    = "core"    // no unit owns it: always applies
	impactKept    = "kept"    // a unit this tree kept: apply it
	impactRemoved = "removed" // a unit this tree trimmed: nothing here to change
)

// impactFile is one file upstream changed between two refs.
//
// Capability names the surface the file serves (`notes`), Unit names the
// installable thing that carries it (`whiteboard`, which trims notes with it).
// Both are reported because both are asked: "what is this file?" and "does my
// tree still have it?".
type impactFile struct {
	Path       string `json:"path"`
	Capability string `json:"capability,omitempty"`
	Unit       string `json:"unit,omitempty"`
	UnitState  string `json:"unitState"`
	Status     string `json:"status"`
	Additions  int    `json:"additions"`
	Deletions  int    `json:"deletions"`
}

// compareResponse is the slice of GitHub's compare payload this needs.
type compareResponse struct {
	Files []struct {
		Filename  string `json:"filename"`
		Status    string `json:"status"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	} `json:"files"`
}

// fetchCompare lists every file changed between two refs, and reports whether
// the answer hit the API's cap.
//
// A 404 (unknown ref) and a 403 (anonymous rate limit spent) are returned
// as-is: the caller named the refs, so it can tell a typo from a busy IP, and a
// rephrased message would hide which one happened.
func fetchCompare(ctx context.Context, api, repo, from, to string) (files []impactFile, truncated bool, err error) {
	if api == "" {
		api = githubAPI
	}
	url := fmt.Sprintf("%s/repos/%s/compare/%s...%s", api, repo, from, to)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gogogo/"+Version)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("compare API answered %s", resp.Status)
	}
	var doc compareResponse
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, false, err
	}
	out := make([]impactFile, 0, len(doc.Files))
	for _, f := range doc.Files {
		out = append(out, impactFile{
			Path: f.Filename, Status: f.Status,
			Additions: f.Additions, Deletions: f.Deletions,
		})
	}
	return out, len(doc.Files) >= compareFileLimit, nil
}

// classifyImpact labels each changed file by the capability that owns it, by
// the unit that carries that capability, and by what this tree did with the
// unit.
//
// This is the whole point of the report: "upstream touched 40 files" is noise
// when 30 of them belong to units you trimmed, and the honest answer for those
// 30 is "nothing here to change". A capability no unit owns is core: it ships
// in every tree, so its changes always apply.
func classifyImpact(files []impactFile, removed map[string]bool) []impactFile {
	index := ownedPathIndex()
	units := capUnit()
	out := make([]impactFile, len(files))
	for i, f := range files {
		capability := ownerOfPath(index, f.Path)
		unit := units[capability]
		state := impactCore
		if unit != "" {
			state = impactKept
			if removed[unit] {
				state = impactRemoved
			}
		}
		f.Capability, f.Unit, f.UnitState = capability, unit, state
		out[i] = f
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UnitState != out[j].UnitState {
			return out[i].UnitState < out[j].UnitState
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// ownedPath is one path a capability owns, with the capability id. exact marks
// a single file; every other entry is a directory prefix.
type ownedPath struct {
	capability string
	path       string
	exact      bool
}

// ownedPathIndex lists every path any capability owns.
func ownedPathIndex() []ownedPath {
	var out []ownedPath
	for _, c := range capabilities.All {
		for _, d := range c.Dirs {
			out = append(out, ownedPath{capability: c.ID, path: strings.TrimSuffix(d, "/")})
		}
		for _, f := range c.Files {
			out = append(out, ownedPath{capability: c.ID, path: f, exact: true})
		}
	}
	return out
}

// ownerOfPath returns the capability owning path, or "" for core.
//
// The LONGEST matching path wins, not the first: ownedPathIndex is built from a
// registry whose entries are not ordered by specificity, and an index-order rule
// is the kind of invariant that holds until someone adds a nested capability. A
// longer path is strictly more specific, so it is also the right answer when
// several match (features/notes beats features/).
func ownerOfPath(index []ownedPath, path string) string {
	best, bestLen := "", -1
	for _, op := range index {
		if op.exact {
			if path == op.path && len(op.path) > bestLen {
				best, bestLen = op.capability, len(op.path)
			}
			continue
		}
		if (path == op.path || strings.HasPrefix(path, op.path+"/")) && len(op.path) > bestLen {
			best, bestLen = op.capability, len(op.path)
		}
	}
	return best
}

// impactLine is the one-sentence verdict: how much of the range is yours.
func impactLine(files []impactFile) string {
	var core, kept, removed int
	for _, f := range files {
		switch f.UnitState {
		case impactCore:
			core++
		case impactKept:
			kept++
		case impactRemoved:
			removed++
		}
	}
	return fmt.Sprintf("summary: %d core, %d in units you kept, %d in units you removed (skippable)",
		core, kept, removed)
}
