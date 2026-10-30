# Charts — external references

Optional libraries, not defaults. Name one when the user's need matches a row,
and do not add the dependency unprompted: `docs/dependency-principles.md`
decides whether the template adopts it, and the one-line trade-off goes in the
PR body.

| Need | Library | How it renders | Trade-off |
|---|---|---|---|
| Interactive charts in a browser — tooltips, zoom, legend toggles, 25+ chart types, 400+ geo maps | [go-echarts](https://github.com/go-echarts/go-echarts) — `github.com/go-echarts/go-echarts/v2` | Go writes an HTML page (or an HTTP handler) wired to [Apache ECharts](https://echarts.apache.org/); the browser runs the ECharts JS bundle | Rich and interactive, but the page carries a JS dependency and nothing renders without a browser. MIT. |
| Charts rendered on the server — PNG/SVG for email, PDF, a report, a CI artifact, or an `<img>` with no JS | [go-analyze/charts](https://github.com/go-analyze/charts) — `github.com/go-analyze/charts` | `charts.NewPainter(...)` → `p.Bytes()`: pure Go, static output, no browser and no JS | Static only — no hover, zoom or filtering. MIT; ZeroVer (v0.x, middle digit bumps are breaking). A fork of `vicanso/go-charts` and the archived `wcharczuk/go-chart`. |

Decision rule: **interaction needs a browser → go-echarts; the chart must exist
without one → go-analyze/charts.** A hybrid is legitimate: go-analyze/charts
rasterizes an ECharts option JSON server-side (`charts.RenderEChartsToPNG`), so
one spec can feed an interactive page and a static export. Prefer either over
hand-rolled SVG/HTML chart code.

For an interactive chart in a Templ page, keep the container element in Templ and
initialize ECharts from the feature's own JS island; never `templ.Raw` a
third-party `<script>` (CSP nonce + `datastar-lint`).
