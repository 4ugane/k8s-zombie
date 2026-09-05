package render

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// HTMLRenderer writes findings as a standalone, self-contained HTML report (no
// external stylesheets/scripts beyond a Google Fonts stylesheet, so it opens
// correctly straight from disk with a working font fallback either way). Uses
// html/template rather than string concatenation so every field value is
// auto-escaped — finding content (Reason especially) is detector-generated text,
// not something to trust blindly into a browser-rendered page.
type HTMLRenderer struct{}

type htmlRow struct {
	Namespace, Name, Reason, Confidence, Status, Cost string
	HasCost, IsSkipped                                bool
	Search                                            string
}

type htmlGroup struct {
	Kind     string
	Detector string
	Count    int
	Rows     []htmlRow
}

type htmlStat struct {
	Label, Value string
}

type htmlPageData struct {
	GeneratedAt string
	Stats       []htmlStat
	Groups      []htmlGroup
}

var htmlPageTemplate = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>k8s-zombie scan report</title>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700&family=IBM+Plex+Mono:wght@400;500;600&display=swap">
<style>
:root {
  --bg: #f4f6f6;
  --surface: #ffffff;
  --surface-alt: #eaf1f0;
  --border: #d9e2e1;
  --text: #16211f;
  --text-muted: #5b6b69;
  --accent: #0f766e;
  --accent-soft: #e3f3f1;
  --sev-orphaned: #b45309;
  --sev-orphaned-soft: #fdf1e2;
  --sev-skipped: #64748b;
  --sev-skipped-soft: #eef1f4;
  --conf-high: #15803d;
  --conf-medium: #b45309;
  --conf-low: #b91c1c;
  --shadow: 0 1px 2px rgba(16, 30, 28, 0.06), 0 4px 10px rgba(16, 30, 28, 0.05);
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #101716;
    --surface: #182322;
    --surface-alt: #1f2b29;
    --border: #2b3a38;
    --text: #e7efee;
    --text-muted: #92a5a2;
    --accent: #2dd4bf;
    --accent-soft: #12312e;
    --sev-orphaned: #f0a54c;
    --sev-orphaned-soft: #2e2415;
    --sev-skipped: #94a3b8;
    --sev-skipped-soft: #232c34;
    --conf-high: #4ade80;
    --conf-medium: #f0a54c;
    --conf-low: #f87171;
    --shadow: 0 1px 2px rgba(0, 0, 0, 0.3), 0 4px 14px rgba(0, 0, 0, 0.25);
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font-family: "IBM Plex Sans", -apple-system, Helvetica, Arial, sans-serif;
  line-height: 1.45;
}
.wrap { max-width: 1080px; margin: 0 auto; padding: 2.5rem 1.5rem 4rem; }
header.page-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.5rem 1.5rem;
  margin-bottom: 1.75rem;
}
h1 {
  font-size: 1.375rem;
  font-weight: 700;
  margin: 0;
  text-wrap: balance;
}
h1 .mark { color: var(--accent); }
.generated {
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-size: 0.8rem;
  color: var(--text-muted);
}
.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 0.75rem;
  margin-bottom: 1.75rem;
}
.stat {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 0.9rem 1.1rem;
  box-shadow: var(--shadow);
}
.stat .label {
  font-size: 0.72rem;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-muted);
  margin-bottom: 0.3rem;
}
.stat .value {
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-size: 1.4rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.stat.cost .value { color: var(--accent); }
.filters {
  display: flex;
  align-items: center;
  gap: 1rem;
  flex-wrap: wrap;
  margin-bottom: 1.5rem;
}
#search {
  flex: 1 1 260px;
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-size: 0.9rem;
  padding: 0.55rem 0.8rem;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text);
}
#search:focus { outline: 2px solid var(--accent); outline-offset: 1px; }
.filters label {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.85rem;
  color: var(--text-muted);
  user-select: none;
}
section.kind-group {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: var(--shadow);
  margin-bottom: 1rem;
  overflow: hidden;
}
section.kind-group > h2 {
  margin: 0;
  padding: 0.75rem 1.1rem;
  background: var(--surface-alt);
  border-bottom: 1px solid var(--border);
  font-size: 0.95rem;
  font-weight: 600;
}
section.kind-group > h2 .detector {
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-size: 0.8rem;
  font-weight: 400;
  color: var(--text-muted);
}
.table-scroll { overflow-x: auto; }
table { border-collapse: collapse; width: 100%; }
th, td {
  padding: 0.6rem 1.1rem;
  text-align: left;
  border-bottom: 1px solid var(--border);
  font-size: 0.85rem;
  white-space: nowrap;
}
td.reason { white-space: normal; min-width: 22ch; }
th {
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-size: 0.68rem;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-muted);
  font-weight: 600;
}
td.mono, td.cost { font-family: "IBM Plex Mono", ui-monospace, monospace; }
tbody tr:last-child td { border-bottom: none; }
tbody tr:hover td { background: var(--surface-alt); }
tr.status-Skipped td { color: var(--text-muted); font-style: italic; }
.pill {
  display: inline-block;
  padding: 0.12rem 0.55rem;
  border-radius: 999px;
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-size: 0.72rem;
  font-weight: 600;
}
.pill.status-Orphaned { color: var(--sev-orphaned); background: var(--sev-orphaned-soft); }
.pill.status-Skipped { color: var(--sev-skipped); background: var(--sev-skipped-soft); }
.conf-High { color: var(--conf-high); font-weight: 600; }
.conf-Medium { color: var(--conf-medium); font-weight: 600; }
.conf-Low { color: var(--conf-low); font-weight: 600; }
td.cost { font-variant-numeric: tabular-nums; font-weight: 600; }
td.cost.has-cost { color: var(--accent); }
td.cost:not(.has-cost) { color: var(--text-muted); }
.empty-state {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: var(--shadow);
  padding: 3rem 1.5rem;
  text-align: center;
  color: var(--text-muted);
}
.empty-state .check {
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  color: var(--accent);
  font-size: 1.6rem;
  display: block;
  margin-bottom: 0.5rem;
}
footer.page-foot {
  margin-top: 2rem;
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-size: 0.75rem;
  color: var(--text-muted);
  text-align: center;
}
[hidden] { display: none !important; }
</style>
</head>
<body>
<div class="wrap">
<header class="page-head">
  <h1><span class="mark">k8s-zombie</span> scan report</h1>
  <span class="generated">generated {{.GeneratedAt}}</span>
</header>

{{if .Groups}}
<div class="stats">
{{range .Stats}}<div class="stat{{if eq .Label "EST. COST/MO"}} cost{{end}}">
  <div class="label">{{.Label}}</div>
  <div class="value">{{.Value}}</div>
</div>
{{end}}</div>

<div class="filters">
  <input id="search" type="search" placeholder="Filter by namespace, name, detector, reason…" autocomplete="off">
  <label><input type="checkbox" id="cost-only"> Only findings with a cost estimate</label>
</div>

{{range .Groups}}<section class="kind-group" data-kind="{{.Kind}}">
  <h2>{{.Kind}} <span class="detector">— {{.Detector}}</span> ({{.Count}})</h2>
  <div class="table-scroll">
  <table>
  <thead>
  <tr><th>Namespace</th><th>Name</th><th>Reason</th><th>Confidence</th><th>Cost/mo</th><th>Status</th></tr>
  </thead>
  <tbody>
  {{range .Rows}}<tr class="status-{{.Status}}" data-search="{{.Search}}" data-has-cost="{{.HasCost}}">
  <td class="mono">{{.Namespace}}</td>
  <td class="mono">{{.Name}}</td>
  <td class="reason">{{.Reason}}</td>
  <td class="conf-{{.Confidence}}">{{.Confidence}}</td>
  <td class="cost{{if .HasCost}} has-cost{{end}}">{{.Cost}}</td>
  <td><span class="pill status-{{.Status}}">{{.Status}}</span></td>
  </tr>
  {{end}}</tbody>
  </table>
  </div>
</section>
{{end}}
{{else}}
<div class="empty-state">
  <span class="check">&#10003;</span>
  No orphaned resources found.
</div>
{{end}}

<footer class="page-foot">Generated by k8s-zombie — read-only, nothing on this page was deleted or modified.</footer>
</div>
<script>
(function () {
  var search = document.getElementById("search");
  var costOnly = document.getElementById("cost-only");
  if (!search || !costOnly) return;

  function apply() {
    var q = search.value.trim().toLowerCase();
    var onlyCost = costOnly.checked;
    document.querySelectorAll("section.kind-group").forEach(function (section) {
      var visibleInSection = 0;
      section.querySelectorAll("tbody tr").forEach(function (row) {
        var matchesText = q === "" || (row.getAttribute("data-search") || "").indexOf(q) !== -1;
        var matchesCost = !onlyCost || row.getAttribute("data-has-cost") === "true";
        var visible = matchesText && matchesCost;
        row.hidden = !visible;
        if (visible) visibleInSection++;
      });
      section.hidden = visibleInSection === 0;
    });
  }

  search.addEventListener("input", apply);
  costOnly.addEventListener("change", apply);
})();
</script>
</body>
</html>
`))

func (HTMLRenderer) Render(w io.Writer, findings []finding.Finding) error {
	sorted := sortedFindings(findings)

	data := htmlPageData{GeneratedAt: time.Now().Format("2006-01-02 15:04:05 MST")}
	if len(sorted) > 0 {
		stats := summarize(sorted)
		skipped := 0
		for _, f := range sorted {
			if f.Status == finding.StatusSkipped {
				skipped++
			}
		}
		costValue := "-"
		if stats.CostedCount > 0 {
			costValue = fmt.Sprintf("$%.2f", stats.TotalCostUSD)
		}
		groups := groupedByKind(sorted)
		data.Stats = []htmlStat{
			{Label: "FINDINGS", Value: fmt.Sprintf("%d", stats.Total)},
			{Label: "EST. COST/MO", Value: costValue},
			{Label: "RESOURCE KINDS", Value: fmt.Sprintf("%d", len(groups))},
			{Label: "SKIPPED", Value: fmt.Sprintf("%d", skipped)},
		}
		for _, group := range groups {
			g := htmlGroup{Kind: group.Kind, Detector: group.Detector, Count: len(group.Findings)}
			for _, f := range group.Findings {
				g.Rows = append(g.Rows, htmlRow{
					Namespace:  formatNamespace(f.Namespace),
					Name:       f.Name,
					Reason:     f.Reason,
					Confidence: string(f.Confidence),
					Status:     string(f.Status),
					Cost:       formatCost(f.CostUSDPerMonth),
					HasCost:    f.CostUSDPerMonth != nil,
					IsSkipped:  f.Status == finding.StatusSkipped,
					// Detector isn't shown as its own column (it's in the group
					// header instead, per detectorLabel), but still searchable.
					Search: strings.ToLower(f.Detector + " " + f.Namespace + " " + f.Name + " " + f.Reason),
				})
			}
			data.Groups = append(data.Groups, g)
		}
	}
	return htmlPageTemplate.Execute(w, data)
}
