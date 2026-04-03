package output

import (
	"fmt"
	"sort"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// HTMLReportData is the top-level data structure passed to the HTML template.
type HTMLReportData struct {
	Metadata         HTMLMetadata
	Clusters         []HTMLCluster
	ClusterWide      []HTMLCollectorResult // Cluster-wide (K8s-level) collectors.
	Analysis         []HTMLAnalysisGroup
	TotalNodes       int
	TotalCollectors  int
	PassedAnalyzers  int
	WarningAnalyzers int
	FailedAnalyzers  int
	SkippedAnalyzers int
}

// HTMLMetadata holds report-level metadata.
type HTMLMetadata struct {
	Profile     string
	Timestamp   string
	ToolVersion string
}

// HTMLCluster represents a single ScyllaDB cluster in the report.
type HTMLCluster struct {
	Name        string
	Namespace   string
	Kind        string
	NodeCount   int
	Datacenters []HTMLDatacenter
	Collectors  []HTMLCollectorResult // PerScyllaCluster-scoped collectors.
}

// HTMLDatacenter represents a datacenter within a cluster.
type HTMLDatacenter struct {
	Name  string
	Racks []HTMLRack
}

// HTMLRack represents a rack within a datacenter.
type HTMLRack struct {
	Name  string
	Nodes []HTMLNode
}

// HTMLNode represents a single Scylla node (pod).
type HTMLNode struct {
	PodName        string
	Namespace      string
	IP             string // Extracted from SystemPeersLocal or GossipInfo; may be empty.
	HostID         string // Extracted from SystemPeersLocal or SystemTopology; may be empty.
	DatacenterName string
	RackName       string
	Collectors     []HTMLCollectorResult // PerScyllaNode-scoped collectors.
}

// HTMLCollectorResult holds a single collector's result for display.
type HTMLCollectorResult struct {
	ID        string
	Name      string
	Status    string // "passed", "failed", "skipped"
	Message   string
	Duration  string
	Artifacts []HTMLArtifact
}

// HTMLArtifact holds metadata for a single artifact file.
type HTMLArtifact struct {
	// URL is the path to fetch the artifact from the HTTP server (e.g., /artifacts/...).
	URL         string
	Description string
	DisplayPath string // Human-friendly path shown in the UI.
}

// HTMLAnalysisGroup groups analyzer results by analyzer ID.
type HTMLAnalysisGroup struct {
	ID      string
	Name    string
	Results []HTMLAnalysisResult
}

// HTMLAnalysisResult holds a single analyzer result for a specific scope.
type HTMLAnalysisResult struct {
	Scope   string // "cluster-wide" or "namespace/name"
	Status  string // "passed", "warning", "failed", "skipped"
	Message string
}

// BuildHTMLReportData constructs the complete HTMLReportData from engine results.
func BuildHTMLReportData(
	clusterInfos []engine.ScyllaClusterInfo,
	podsByCluster map[engine.ScopeKey][]engine.ScyllaNodeInfo,
	vitals *engine.Vitals,
	jsonReport *JSONReport,
	collectorNames map[engine.CollectorID]string,
	analyzerNames map[engine.AnalyzerID]string,
) *HTMLReportData {
	data := &HTMLReportData{}

	// Metadata from report.json if available.
	if jsonReport != nil {
		data.Metadata = HTMLMetadata{
			Profile:     jsonReport.Metadata.Profile,
			Timestamp:   jsonReport.Metadata.Timestamp,
			ToolVersion: jsonReport.Metadata.ToolVersion,
		}
	}

	// Build cluster-wide collectors.
	data.ClusterWide = buildClusterWideCollectors(vitals, collectorNames)

	// Build per-cluster hierarchy.
	totalNodes := 0
	for _, cluster := range clusterInfos {
		clusterKey := engine.ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
		nodes := podsByCluster[clusterKey]
		totalNodes += len(nodes)

		htmlCluster := HTMLCluster{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Kind:      cluster.Kind,
			NodeCount: len(nodes),
		}

		// PerScyllaCluster collectors for this cluster.
		htmlCluster.Collectors = buildPerScopeCollectors(
			vitals.PerScyllaCluster[clusterKey],
			collectorNames,
			"per-scylla-cluster",
			clusterKey,
		)

		// Build topology: DC -> Rack -> Node.
		htmlCluster.Datacenters = buildTopologyHierarchy(nodes, vitals, collectorNames, clusterKey)

		data.Clusters = append(data.Clusters, htmlCluster)
	}
	data.TotalNodes = totalNodes

	// Count total unique collector IDs across all scopes.
	collectorIDSet := make(map[engine.CollectorID]struct{})
	for id := range vitals.ClusterWide {
		collectorIDSet[id] = struct{}{}
	}
	for _, m := range vitals.PerScyllaCluster {
		for id := range m {
			collectorIDSet[id] = struct{}{}
		}
	}
	for _, m := range vitals.PerScyllaNode {
		for id := range m {
			collectorIDSet[id] = struct{}{}
		}
	}
	data.TotalCollectors = len(collectorIDSet)

	// Build analysis results.
	if jsonReport != nil {
		data.Analysis = buildAnalysisResults(jsonReport, analyzerNames)
		// Count statuses.
		for _, group := range data.Analysis {
			for _, res := range group.Results {
				switch res.Status {
				case "passed":
					data.PassedAnalyzers++
				case "warning":
					data.WarningAnalyzers++
				case "failed":
					data.FailedAnalyzers++
				case "skipped":
					data.SkippedAnalyzers++
				}
			}
		}
	}

	return data
}

// buildClusterWideCollectors builds HTMLCollectorResult entries for cluster-wide collectors.
func buildClusterWideCollectors(vitals *engine.Vitals, collectorNames map[engine.CollectorID]string) []HTMLCollectorResult {
	var results []HTMLCollectorResult
	// Sort collector IDs for deterministic output.
	ids := make([]engine.CollectorID, 0, len(vitals.ClusterWide))
	for id := range vitals.ClusterWide {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		res := vitals.ClusterWide[id]
		results = append(results, toHTMLCollectorResult(id, res, collectorNames, "cluster-wide", engine.ScopeKey{}))
	}
	return results
}

// buildPerScopeCollectors builds HTMLCollectorResult entries for a specific scope key.
func buildPerScopeCollectors(
	scopeResults map[engine.CollectorID]*engine.CollectorResult,
	collectorNames map[engine.CollectorID]string,
	scopeDir string,
	scopeKey engine.ScopeKey,
) []HTMLCollectorResult {
	if len(scopeResults) == 0 {
		return nil
	}

	ids := make([]engine.CollectorID, 0, len(scopeResults))
	for id := range scopeResults {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var results []HTMLCollectorResult
	for _, id := range ids {
		res := scopeResults[id]
		results = append(results, toHTMLCollectorResult(id, res, collectorNames, scopeDir, scopeKey))
	}
	return results
}

// buildTopologyHierarchy organizes nodes into DC -> Rack -> Node hierarchy.
func buildTopologyHierarchy(
	nodes []engine.ScyllaNodeInfo,
	vitals *engine.Vitals,
	collectorNames map[engine.CollectorID]string,
	clusterKey engine.ScopeKey,
) []HTMLDatacenter {
	// Group by DC -> Rack.
	type rackNodes struct {
		rack  string
		nodes []engine.ScyllaNodeInfo
	}
	dcRacks := make(map[string]map[string][]engine.ScyllaNodeInfo)
	for _, node := range nodes {
		dc := node.DatacenterName
		if dc == "" {
			dc = "(unknown)"
		}
		rack := node.RackName
		if rack == "" {
			rack = "(unknown)"
		}
		if dcRacks[dc] == nil {
			dcRacks[dc] = make(map[string][]engine.ScyllaNodeInfo)
		}
		dcRacks[dc][rack] = append(dcRacks[dc][rack], node)
	}

	// Sort DCs.
	dcNames := make([]string, 0, len(dcRacks))
	for dc := range dcRacks {
		dcNames = append(dcNames, dc)
	}
	sort.Strings(dcNames)

	// Build the node enrichment map once for this cluster.
	enrichment := buildNodeEnrichment(nodes, vitals, clusterKey)

	var datacenters []HTMLDatacenter
	for _, dcName := range dcNames {
		racks := dcRacks[dcName]
		rackNames := make([]string, 0, len(racks))
		for rack := range racks {
			rackNames = append(rackNames, rack)
		}
		sort.Strings(rackNames)

		var htmlRacks []HTMLRack
		for _, rackName := range rackNames {
			rackNodes := racks[rackName]
			var htmlNodes []HTMLNode
			for _, node := range rackNodes {
				nodeKey := engine.ScopeKey{Namespace: node.Namespace, Name: node.Name}
				enriched := enrichment[nodeKey]

				htmlNode := HTMLNode{
					PodName:        node.Name,
					Namespace:      node.Namespace,
					IP:             enriched.ip,
					HostID:         enriched.hostID,
					DatacenterName: node.DatacenterName,
					RackName:       node.RackName,
					Collectors: buildPerScopeCollectors(
						vitals.PerScyllaNode[nodeKey],
						collectorNames,
						"per-scylla-node",
						nodeKey,
					),
				}
				htmlNodes = append(htmlNodes, htmlNode)
			}
			htmlRacks = append(htmlRacks, HTMLRack{
				Name:  rackName,
				Nodes: htmlNodes,
			})
		}
		datacenters = append(datacenters, HTMLDatacenter{
			Name:  dcName,
			Racks: htmlRacks,
		})
	}

	return datacenters
}

// nodeEnrichment holds IP and Host ID extracted from collector data.
type nodeEnrichment struct {
	ip     string
	hostID string
}

// buildNodeEnrichment extracts IP and Host ID for each node from collector data.
// It uses SystemPeersLocal as the primary source (it has both HostID and peer IPs),
// with SystemTopology as a fallback for HostID.
func buildNodeEnrichment(
	nodes []engine.ScyllaNodeInfo,
	vitals *engine.Vitals,
	clusterKey engine.ScopeKey,
) map[engine.ScopeKey]nodeEnrichment {
	enrichment := make(map[engine.ScopeKey]nodeEnrichment, len(nodes))

	// First pass: extract HostID from each node's own SystemPeersLocal result (system.local).
	// Also build a HostID -> ScopeKey reverse map for IP lookups.
	hostIDToKey := make(map[string]engine.ScopeKey)
	for _, node := range nodes {
		nodeKey := engine.ScopeKey{Namespace: node.Namespace, Name: node.Name}
		peersResult, err := collectors.GetSystemPeersLocalResult(vitals, nodeKey)
		if err != nil || peersResult == nil || peersResult.Local == nil {
			continue
		}

		e := enrichment[nodeKey]
		e.hostID = peersResult.Local.HostID
		enrichment[nodeKey] = e

		if peersResult.Local.HostID != "" {
			hostIDToKey[peersResult.Local.HostID] = nodeKey
		}
	}

	// Second pass: cross-reference peers to find IPs.
	// Each node's system.peers tells us the IP of its peers.
	for _, node := range nodes {
		nodeKey := engine.ScopeKey{Namespace: node.Namespace, Name: node.Name}
		peersResult, err := collectors.GetSystemPeersLocalResult(vitals, nodeKey)
		if err != nil || peersResult == nil {
			continue
		}

		for _, peer := range peersResult.Peers {
			if peerKey, ok := hostIDToKey[peer.HostID]; ok {
				e := enrichment[peerKey]
				if e.ip == "" {
					e.ip = peer.Peer
					enrichment[peerKey] = e
				}
			}
		}
	}

	// Third pass: try SystemTopology for HostID if still missing.
	for _, node := range nodes {
		nodeKey := engine.ScopeKey{Namespace: node.Namespace, Name: node.Name}
		e := enrichment[nodeKey]
		if e.hostID != "" {
			continue
		}

		topoResult, err := collectors.GetSystemTopologyResult(vitals, nodeKey)
		if err != nil || topoResult == nil {
			continue
		}

		// SystemTopology returns all nodes; we match by DC+rack to find the local node.
		// This is a best-effort heuristic when SystemPeersLocal failed.
		for _, topoNode := range topoResult.Nodes {
			if topoNode.Datacenter == node.DatacenterName && topoNode.Rack == node.RackName {
				e.hostID = topoNode.HostID
				enrichment[nodeKey] = e
				break
			}
		}
	}

	return enrichment
}

// toHTMLCollectorResult converts an engine.CollectorResult to an HTMLCollectorResult.
func toHTMLCollectorResult(
	id engine.CollectorID,
	res *engine.CollectorResult,
	collectorNames map[engine.CollectorID]string,
	scopeDir string,
	scopeKey engine.ScopeKey,
) HTMLCollectorResult {
	name := collectorNames[id]
	if name == "" {
		name = string(id)
	}

	status := "unknown"
	switch res.Status {
	case engine.CollectorPassed:
		status = "passed"
	case engine.CollectorFailed:
		status = "failed"
	case engine.CollectorSkipped:
		status = "skipped"
	}

	dur := "-"
	if res.Duration > 0 {
		dur = fmt.Sprintf("%dms", res.Duration.Milliseconds())
	}

	var htmlArtifacts []HTMLArtifact
	for _, a := range res.Artifacts {
		var artifactURL string
		if scopeKey.IsEmpty() {
			artifactURL = fmt.Sprintf("/artifacts/%s/%s/%s", scopeDir, id, a.RelativePath)
		} else {
			artifactURL = fmt.Sprintf("/artifacts/%s/%s/%s/%s/%s", scopeDir, scopeKey.Namespace, scopeKey.Name, id, a.RelativePath)
		}

		htmlArtifacts = append(htmlArtifacts, HTMLArtifact{
			URL:         artifactURL,
			Description: a.Description,
			DisplayPath: a.RelativePath,
		})
	}

	return HTMLCollectorResult{
		ID:        string(id),
		Name:      name,
		Status:    status,
		Message:   res.Message,
		Duration:  dur,
		Artifacts: htmlArtifacts,
	}
}

// buildAnalysisResults converts JSONReport analysis results into the HTML format.
func buildAnalysisResults(report *JSONReport, analyzerNames map[engine.AnalyzerID]string) []HTMLAnalysisGroup {
	if report == nil || len(report.Analysis) == 0 {
		return nil
	}

	// Sort analyzer IDs for deterministic output.
	ids := make([]engine.AnalyzerID, 0, len(report.Analysis))
	for id := range report.Analysis {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var groups []HTMLAnalysisGroup
	for _, id := range ids {
		byScope := report.Analysis[id]
		name := analyzerNames[id]
		if name == "" {
			name = string(id)
		}

		group := HTMLAnalysisGroup{
			ID:   string(id),
			Name: name,
		}

		// Sort scope keys.
		scopeKeys := make([]string, 0, len(byScope))
		for k := range byScope {
			scopeKeys = append(scopeKeys, k)
		}
		sort.Strings(scopeKeys)

		for _, scopeKey := range scopeKeys {
			res := byScope[scopeKey]
			scope := scopeKey
			if scope == "" {
				scope = "cluster-wide"
			}
			group.Results = append(group.Results, HTMLAnalysisResult{
				Scope:   scope,
				Status:  res.Status,
				Message: res.Message,
			})
		}
		groups = append(groups, group)
	}

	return groups
}
