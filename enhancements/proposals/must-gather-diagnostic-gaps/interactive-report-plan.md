# Interactive HTML Report for `diagnose serve-report`

## Summary

Add a new subcommand `scylla-operator diagnose serve-report` that:
1. Loads a diagnose output directory (or extracts a `.tar.gz` archive)
2. Reads `vitals.json` to reconstruct topology, vitals, and artifact metadata
3. Generates a self-contained interactive HTML report (Go `html/template` + Tailwind CSS)
4. Serves it over HTTP on a random available port, with artifact files served from the output directory
5. Prints the URL for the user to open

## Architecture Decisions

| Decision | Choice |
|----------|--------|
| Delivery | Single `.html` page served over HTTP |
| Artifacts | Referenced on disk, served by the HTTP server |
| Frontend | Go `html/template` + Tailwind CSS (CDN via served page) |
| Analysis | Included in the report |
| Input | `--output-dir` (default: cwd) or `--from-archive` |
| Port | Random available port |
| Browser | Print URL only |

## File Changes

### 1. Convert `diagnose` from a leaf command to a parent command

**File:** `pkg/cmd/operator/diagnose.go`

- Modify `NewDiagnoseCmd()` to make `diagnose` a parent command that also has its own `RunE` (so existing `scylla-operator diagnose` still works as before)
- Add `cmd.AddCommand(NewServeReportCmd(streams))` to register the new subcommand
- No changes to existing behavior

### 2. New file: `pkg/cmd/operator/servereport.go`

The `serve-report` subcommand implementation following the project's `Options` pattern:

```
ServeReportOptions struct:
  - OutputDir   string   // --output-dir (default: cwd)
  - FromArchive string   // --from-archive (path to .tar.gz)
  
NewServeReportOptions() -> defaults
AddFlags(flagset)
Validate() -> check mutual exclusivity, validate paths exist
Complete() -> resolve output dir (extract archive if needed)
Run() -> load data, build HTML, start HTTP server
```

**Run flow:**
1. Determine the data directory (from `--output-dir` or extract `--from-archive` to temp dir)
2. Read and parse `vitals.json` using existing `engine.FromSerializable()` + `collectors.ResultTypeRegistry()`
3. Reconstruct topology via `engine.ClusterTopologyFromVitals()`
4. Read `report.json` if present (for analysis results)
5. Build a `ReportData` struct that organizes everything by cluster hierarchy
6. Extract enriched node info (IP, Host ID) from `SystemPeersLocal` / `SystemTopology` collector data with graceful fallback
7. Render HTML template into memory
8. Start HTTP server with:
   - `/` -> serves the generated HTML
   - `/artifacts/...` -> serves static artifact files from the output directory's `collectors/` subdirectory
9. Listen on `127.0.0.1:0` (random port), print URL, block until SIGINT/SIGTERM

### 3. New file: `pkg/soda/output/html.go`

The HTML report builder:

```go
type HTMLReportData struct {
    Clusters []HTMLCluster   // organized by cluster
    Analysis []HTMLAnalysis  // analyzer results
    Metadata HTMLMetadata    // profile, timestamp, etc.
}

type HTMLCluster struct {
    Name, Namespace, Kind string
    Datacenters []HTMLDatacenter
    // Cluster-level collectors (PerScyllaCluster scope)
    Collectors []HTMLCollectorResult
}

type HTMLDatacenter struct {
    Name  string
    Racks []HTMLRack
}

type HTMLRack struct {
    Name  string
    Nodes []HTMLNode
}

type HTMLNode struct {
    PodName        string
    Namespace      string
    IP             string  // from SystemPeersLocal/GossipInfo, or ""
    HostID         string  // from SystemTopology/SystemPeersLocal, or ""
    DatacenterName string
    RackName       string
    // Node-level collectors (PerScyllaNode scope)
    Collectors []HTMLCollectorResult
}

type HTMLCollectorResult struct {
    ID          string
    Name        string
    Status      string
    Message     string
    Duration    string
    Artifacts   []HTMLArtifact
}

type HTMLArtifact struct {
    RelativePath string  // for linking to /artifacts/...
    Description  string
    DisplayPath  string  // human-friendly path shown in UI
}

func BuildHTMLReport(vitals, topology, analysisResults, collectorNames) *HTMLReportData
func RenderHTML(data *HTMLReportData) ([]byte, error)  // executes template
```

### 4. New file: `pkg/soda/output/html_template.go`

The Go `html/template` containing the full HTML page with inlined Tailwind CSS and JS. Key UI features:

- **Layout:** Sidebar with cluster tree navigation + main content area
- **Cluster tree:** Collapsible hierarchy: Cluster > DC > Rack > Node
- **Cluster view:** Shows cluster-level collectors with status badges, click to expand vitals data and artifact list
- **Node view:** Shows pod name, IP (if available), Host ID (if available), DC, rack. Node-level collectors with same expand/collapse pattern
- **Artifact viewer:** Click an artifact to load it via fetch from `/artifacts/...` and display in a scrollable `<pre>` block (with syntax highlighting for YAML/JSON via simple JS)
- **Analysis section:** Table/cards showing analyzer verdicts with color-coded status (passed=green, warning=yellow, failed=red, skipped=gray)
- **Cluster-wide collectors:** Shown in a dedicated section (e.g., NodeResources, NodeManifest)
- **Search/filter:** Simple text filter to find collectors/nodes by name
- **Tailwind CSS:** Loaded from CDN `<script>` tag (since it's served over HTTP, CDN is fine)

### 5. New file: `pkg/soda/output/html_test.go`

Tests for the HTML report builder:
- Test `BuildHTMLReport` with various inputs (single cluster, multi-cluster, missing collector data)
- Test node enrichment (IP/HostID extraction with fallback)
- Test `RenderHTML` produces valid HTML

### 6. New file: `pkg/cmd/operator/servereport_test.go`

Tests for the serve-report command:
- Test flag validation (mutual exclusivity of `--output-dir` and `--from-archive`)
- Test loading from a directory
- Test loading from a `.tar.gz` archive

## Node Enrichment Logic (IP + Host ID extraction)

For each `ScyllaNodeInfo`, attempt to extract richer metadata:

1. Look up `SystemPeersLocal` collector result for the node's scope key
2. The `SystemLocalRow` has `HostID` for that node
3. Cross-reference with peer entries from other nodes to find the IP (match by `HostID`)
4. Fallback: check `SystemTopology` for `HostID`, `GossipInfo` for `Addrs`
5. If no collector data is available (failed/skipped), gracefully show only `ScyllaNodeInfo` fields

## HTTP Server Routes

| Route | Handler |
|-------|---------|
| `GET /` | Serves the generated HTML report |
| `GET /artifacts/{scope}/{...path}` | Serves artifact files from `<outputDir>/collectors/` |

The artifacts route maps:
- `/artifacts/cluster-wide/<collectorID>/<file>` -> `<outputDir>/collectors/cluster-wide/<collectorID>/<file>`
- `/artifacts/per-scylla-cluster/<ns>/<name>/<collectorID>/<file>` -> `<outputDir>/collectors/per-scylla-cluster/<ns>/<name>/<collectorID>/<file>`
- `/artifacts/per-scylla-node/<ns>/<name>/<collectorID>/<file>` -> `<outputDir>/collectors/per-scylla-node/<ns>/<name>/<collectorID>/<file>`

## Task Breakdown

1. Convert `diagnose` to support subcommands (modify `NewDiagnoseCmd`)
2. Create `ServeReportOptions` with flags, validation, completion (`servereport.go`)
3. Build the `HTMLReportData` model with node enrichment logic (`html.go`)
4. Create the HTML template with Tailwind CSS + JS (`html_template.go`)
5. Implement the HTTP server in `ServeReportOptions.Run()`
6. Write tests (`html_test.go`, `servereport_test.go`)
