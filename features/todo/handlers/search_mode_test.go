// SCOPE:feature — the runtime search-mode selector (red-proof).
//
// Split from search_test.go for the file-size gate, and along a real seam: that
// file covers search itself (does it narrow, does it rank, does it degrade),
// this one covers CHOOSING the layer at runtime, which is the feature that made
// the three layers reachable from the UI at all.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/calionauta/gogogo/internal/search"
)

func testSearchLayers(mode string, layer search.Searcher) SearchLayers {
	return SearchLayers{
		ByMode:  map[string]Searcher{mode: NewTodoSearcher(layer)},
		Default: mode,
	}
}

// TestSearchModeIsRuntimeSelectable is the red-proof for the reported gap: the
// three layers existed but the UI had no way to reach two of them (SEARCH_MODE
// was boot-only). With all three wired, the SAME query must be answered by the
// layer the request names — and the response must report which one ran, so the
// picker can show the truth rather than the request.
func TestSearchModeIsRuntimeSelectable(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	sub, err := search.New(search.SubstringMode, NewTodoSource(f.q), nil)
	if err != nil {
		t.Fatalf("substring: %v", err)
	}
	fuz, err := search.New(search.FuzzyMode, NewTodoSource(f.q), nil)
	if err != nil {
		t.Fatalf("fuzzy: %v", err)
	}
	f.h.SetSearch(SearchLayers{
		ByMode: map[string]Searcher{
			search.SubstringMode: NewTodoSearcher(sub),
			search.FuzzyMode:     NewTodoSearcher(fuz),
		},
		Default: search.SubstringMode,
	}, nil, nil)
	cookie := f.signup("modes@example.com")
	me := f.ownerOf(t, cookie)
	createStamped(t, f, me, "kestrel migration plan")

	// A typo: substring finds nothing, fuzzy tolerates it. That difference is
	// what proves the request's mode reached the layer.
	typo := "kestrle"
	body := searchJSONWithMode(t, f, cookie, typo, search.SubstringMode).Body.String()
	if contains(body, "kestrel migration plan") {
		t.Errorf("substring matched a typo (it must not):\n%s", body)
	}
	body = searchJSONWithMode(t, f, cookie, typo, search.FuzzyMode).Body.String()
	if !contains(body, "kestrel migration plan") {
		t.Errorf("fuzzy failed to match the typo it exists for:\n%s", body)
	}

	// An unknown mode falls back to the default instead of erroring: a stale
	// tab holding a layer the operator removed must keep searching.
	body = searchJSONWithMode(t, f, cookie, "kestrel", "not-a-layer").Body.String()
	if !contains(body, "kestrel migration plan") {
		t.Errorf("an unknown mode must fall back to the default:\n%s", body)
	}
}

// TestSearchModeSignalReportsTheActiveLayer pins the server-authored half the
// picker renders from: `searchMode` is what actually ran (the fallback, when
// one happened), and `searchModes` is what boot built. A SELECT reading only
// the request would show a mode that is not in effect.
func TestSearchModeSignalReportsTheActiveLayer(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	layer, err := search.New(search.SubstringMode, NewTodoSource(f.q), nil)
	if err != nil {
		t.Fatalf("search.New: %v", err)
	}
	f.h.SetSearch(testSearchLayers(search.SubstringMode, layer), nil, nil)
	cookie := f.signup("modesignal@example.com")

	body := searchJSONWithMode(t, f, cookie, "anything", "unwired").Body.String()
	if !contains(body, `"searchMode":"substring"`) {
		t.Errorf("the response must report the RESOLVED mode, not the request:\n%s", body)
	}
	if !contains(body, `"searchModes":["substring"]`) {
		t.Errorf("the response must list the wired modes for the picker:\n%s", body)
	}
}

// searchJSONWithMode is searchJSON for a request that names a search layer.
// Kept separate rather than adding a parameter to searchJSON: the mode-less
// call is the shape almost every existing test wants (it exercises the
// default), and changing that signature would touch all of them for no
// coverage gain.
func searchJSONWithMode(
	t *testing.T, f *handlersFixture, cookie *http.Cookie, query, mode string,
) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/todos/search?q=" + url.QueryEscape(query) + "&mode=" + url.QueryEscape(mode)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d (body: %s)", target, rec.Code, rec.Body.String())
	}
	return rec
}

// TestSearchModesOrderIsCheapToStrong pins the picker's option order. bm25
// alphabetically precedes fuzzy and substring, so a sorted listing led with the
// most expensive, least-literal layer — the first thing a user saw when opening
// Match was "relevance", before they had asked for ranking.
//
// Red-proof: go back to sort.Strings in SearchLayers.Modes and this fails with
// [bm25 fuzzy substring].
func TestSearchModesOrderIsCheapToStrong(t *testing.T) {
	t.Parallel()
	layers := SearchLayers{
		ByMode: map[string]Searcher{
			search.SubstringMode: nil,
			search.FuzzyMode:     nil,
			search.BM25Mode:      nil,
		},
		Default: search.SubstringMode,
		Order:   search.Modes(),
	}
	got := layers.Modes()
	want := []string{search.SubstringMode, search.FuzzyMode, search.BM25Mode}
	if !slices.Equal(got, want) {
		t.Errorf("Modes() = %v, want %v (cheap to strong)", got, want)
	}
}

// TestSearchModesReportsOnlyWhatIsWired pins the other half: boot may supply a
// full order while this build wires a subset (a trimmed install), so the picker
// must offer exactly the layers that can actually answer.
func TestSearchModesReportsOnlyWhatIsWired(t *testing.T) {
	t.Parallel()
	layers := SearchLayers{
		ByMode:  map[string]Searcher{search.SubstringMode: nil},
		Default: search.SubstringMode,
		Order:   search.Modes(),
	}
	if got := layers.Modes(); !slices.Equal(got, []string{search.SubstringMode}) {
		t.Errorf("Modes() = %v, want only the wired substring layer", got)
	}
	// A set built without an order (test fixtures) still reports what it has.
	bare := SearchLayers{ByMode: map[string]Searcher{search.FuzzyMode: nil}}
	if got := bare.Modes(); !slices.Equal(got, []string{search.FuzzyMode}) {
		t.Errorf("Modes() without an order = %v, want the wired layer", got)
	}
}
