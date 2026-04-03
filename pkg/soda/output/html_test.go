package output

import (
	"strings"
	"testing"
	"time"

	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

func TestBuildHTMLReportData_SingleCluster(t *testing.T) {
	vitals := engine.NewVitals()

	// ClusterWide collector.
	vitals.Store("NodeResourcesCollector", engine.ClusterWide, engine.ScopeKey{}, &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: "Collected 3 nodes",
		Artifacts: []engine.Artifact{
			{RelativePath: "nodes.yaml", Description: "Kubernetes node manifests"},
		},
	})

	// PerScyllaNode collectors.
	podKey := engine.ScopeKey{Namespace: "scylla", Name: "my-cluster-0"}
	vitals.Store("OSInfoCollector", engine.PerScyllaNode, podKey, &engine.CollectorResult{
		Status:   engine.CollectorPassed,
		Message:  "Ubuntu 22.04 x86_64",
		Duration: 150 * time.Millisecond,
	})

	clusters := []engine.ScyllaClusterInfo{
		{Name: "my-cluster", Namespace: "scylla", Kind: "ScyllaCluster"},
	}
	pods := map[engine.ScopeKey][]engine.ScyllaNodeInfo{
		{Namespace: "scylla", Name: "my-cluster"}: {
			{Name: "my-cluster-0", Namespace: "scylla", DatacenterName: "dc1", RackName: "rack1"},
		},
	}

	collectorNames := map[engine.CollectorID]string{
		"NodeResourcesCollector": "Kubernetes Node Resources",
		"OSInfoCollector":        "OS Information",
	}
	analyzerNames := map[engine.AnalyzerID]string{}

	report := &JSONReport{
		Metadata: JSONMetadata{
			Profile:     "full",
			Timestamp:   "2025-01-01T00:00:00Z",
			ToolVersion: "0.1.0",
		},
		Analysis: map[engine.AnalyzerID]map[string]*JSONAnalyzerResult{
			"ScyllaVersionSupportAnalyzer": {
				"scylla/my-cluster": {
					Status:  "passed",
					Message: "ScyllaDB 6.2.2 is supported",
				},
			},
		},
	}

	data := BuildHTMLReportData(clusters, pods, vitals, report, collectorNames, analyzerNames)

	// Check metadata.
	if data.Metadata.Profile != "full" {
		t.Errorf("profile = %q, want 'full'", data.Metadata.Profile)
	}
	if data.Metadata.Timestamp != "2025-01-01T00:00:00Z" {
		t.Errorf("timestamp = %q, want '2025-01-01T00:00:00Z'", data.Metadata.Timestamp)
	}

	// Check cluster-wide collectors.
	if len(data.ClusterWide) != 1 {
		t.Fatalf("cluster-wide count = %d, want 1", len(data.ClusterWide))
	}
	if data.ClusterWide[0].Name != "Kubernetes Node Resources" {
		t.Errorf("cluster-wide name = %q, want 'Kubernetes Node Resources'", data.ClusterWide[0].Name)
	}
	if data.ClusterWide[0].Status != "passed" {
		t.Errorf("cluster-wide status = %q, want 'passed'", data.ClusterWide[0].Status)
	}
	if len(data.ClusterWide[0].Artifacts) != 1 {
		t.Fatalf("cluster-wide artifacts = %d, want 1", len(data.ClusterWide[0].Artifacts))
	}
	if !strings.Contains(data.ClusterWide[0].Artifacts[0].URL, "/artifacts/cluster-wide/NodeResourcesCollector/nodes.yaml") {
		t.Errorf("artifact URL = %q, want to contain '/artifacts/cluster-wide/NodeResourcesCollector/nodes.yaml'", data.ClusterWide[0].Artifacts[0].URL)
	}

	// Check clusters.
	if len(data.Clusters) != 1 {
		t.Fatalf("clusters count = %d, want 1", len(data.Clusters))
	}
	cluster := data.Clusters[0]
	if cluster.Name != "my-cluster" {
		t.Errorf("cluster name = %q, want 'my-cluster'", cluster.Name)
	}
	if cluster.NodeCount != 1 {
		t.Errorf("node count = %d, want 1", cluster.NodeCount)
	}

	// Check topology.
	if len(cluster.Datacenters) != 1 {
		t.Fatalf("dc count = %d, want 1", len(cluster.Datacenters))
	}
	dc := cluster.Datacenters[0]
	if dc.Name != "dc1" {
		t.Errorf("dc name = %q, want 'dc1'", dc.Name)
	}
	if len(dc.Racks) != 1 {
		t.Fatalf("rack count = %d, want 1", len(dc.Racks))
	}
	rack := dc.Racks[0]
	if rack.Name != "rack1" {
		t.Errorf("rack name = %q, want 'rack1'", rack.Name)
	}
	if len(rack.Nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(rack.Nodes))
	}
	node := rack.Nodes[0]
	if node.PodName != "my-cluster-0" {
		t.Errorf("pod name = %q, want 'my-cluster-0'", node.PodName)
	}

	// Check node collectors.
	if len(node.Collectors) != 1 {
		t.Fatalf("node collector count = %d, want 1", len(node.Collectors))
	}
	if node.Collectors[0].Name != "OS Information" {
		t.Errorf("node collector name = %q, want 'OS Information'", node.Collectors[0].Name)
	}
	if node.Collectors[0].Duration != "150ms" {
		t.Errorf("node collector duration = %q, want '150ms'", node.Collectors[0].Duration)
	}

	// Check analysis.
	if len(data.Analysis) != 1 {
		t.Fatalf("analysis count = %d, want 1", len(data.Analysis))
	}
	if data.PassedAnalyzers != 1 {
		t.Errorf("passed analyzers = %d, want 1", data.PassedAnalyzers)
	}

	// Check totals.
	if data.TotalNodes != 1 {
		t.Errorf("total nodes = %d, want 1", data.TotalNodes)
	}
	if data.TotalCollectors != 2 {
		t.Errorf("total collectors = %d, want 2", data.TotalCollectors)
	}
}

func TestBuildHTMLReportData_MultiCluster(t *testing.T) {
	vitals := engine.NewVitals()

	pod1Key := engine.ScopeKey{Namespace: "ns1", Name: "cluster1-0"}
	pod2Key := engine.ScopeKey{Namespace: "ns2", Name: "cluster2-0"}
	vitals.Store("OSInfoCollector", engine.PerScyllaNode, pod1Key, &engine.CollectorResult{
		Status:  engine.CollectorPassed,
		Message: "Ubuntu 22.04",
	})
	vitals.Store("OSInfoCollector", engine.PerScyllaNode, pod2Key, &engine.CollectorResult{
		Status:  engine.CollectorFailed,
		Message: "failed to collect",
	})

	clusters := []engine.ScyllaClusterInfo{
		{Name: "cluster1", Namespace: "ns1", Kind: "ScyllaCluster"},
		{Name: "cluster2", Namespace: "ns2", Kind: "ScyllaDBDatacenter"},
	}
	pods := map[engine.ScopeKey][]engine.ScyllaNodeInfo{
		{Namespace: "ns1", Name: "cluster1"}: {
			{Name: "cluster1-0", Namespace: "ns1", DatacenterName: "dc1", RackName: "rack1"},
		},
		{Namespace: "ns2", Name: "cluster2"}: {
			{Name: "cluster2-0", Namespace: "ns2", DatacenterName: "dc2", RackName: "rack2"},
		},
	}

	collectorNames := map[engine.CollectorID]string{"OSInfoCollector": "OS Information"}

	data := BuildHTMLReportData(clusters, pods, vitals, nil, collectorNames, nil)

	if len(data.Clusters) != 2 {
		t.Fatalf("clusters count = %d, want 2", len(data.Clusters))
	}
	if data.TotalNodes != 2 {
		t.Errorf("total nodes = %d, want 2", data.TotalNodes)
	}

	// Second cluster should have a failed node collector.
	c2 := data.Clusters[1]
	if len(c2.Datacenters) != 1 || len(c2.Datacenters[0].Racks) != 1 || len(c2.Datacenters[0].Racks[0].Nodes) != 1 {
		t.Fatal("unexpected topology for cluster2")
	}
	node := c2.Datacenters[0].Racks[0].Nodes[0]
	if len(node.Collectors) != 1 || node.Collectors[0].Status != "failed" {
		t.Errorf("expected failed collector, got status=%q", node.Collectors[0].Status)
	}
}

func TestBuildHTMLReportData_NilReport(t *testing.T) {
	vitals := engine.NewVitals()
	data := BuildHTMLReportData(nil, nil, vitals, nil, nil, nil)

	if len(data.Clusters) != 0 {
		t.Errorf("clusters count = %d, want 0", len(data.Clusters))
	}
	if len(data.Analysis) != 0 {
		t.Errorf("analysis count = %d, want 0", len(data.Analysis))
	}
}

func TestNodeEnrichment_SystemPeersLocal(t *testing.T) {
	vitals := engine.NewVitals()

	// Simulate two nodes in the same cluster.
	node1Key := engine.ScopeKey{Namespace: "scylla", Name: "my-cluster-0"}
	node2Key := engine.ScopeKey{Namespace: "scylla", Name: "my-cluster-1"}

	// Node 1's system.local has its own HostID. Its peers list shows node 2.
	vitals.Store(collectors.SystemPeersLocalCollectorID, engine.PerScyllaNode, node1Key, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.SystemPeersLocalResult{
			Local: &collectors.SystemLocalRow{
				HostID:     "host-id-1",
				DataCenter: "dc1",
				Rack:       "rack1",
			},
			Peers: []collectors.SystemPeerRow{
				{Peer: "10.0.0.2", HostID: "host-id-2", DataCenter: "dc1", Rack: "rack1"},
			},
		},
	})

	// Node 2's system.local has its own HostID. Its peers list shows node 1.
	vitals.Store(collectors.SystemPeersLocalCollectorID, engine.PerScyllaNode, node2Key, &engine.CollectorResult{
		Status: engine.CollectorPassed,
		Data: &collectors.SystemPeersLocalResult{
			Local: &collectors.SystemLocalRow{
				HostID:     "host-id-2",
				DataCenter: "dc1",
				Rack:       "rack1",
			},
			Peers: []collectors.SystemPeerRow{
				{Peer: "10.0.0.1", HostID: "host-id-1", DataCenter: "dc1", Rack: "rack1"},
			},
		},
	})

	nodes := []engine.ScyllaNodeInfo{
		{Name: "my-cluster-0", Namespace: "scylla", DatacenterName: "dc1", RackName: "rack1"},
		{Name: "my-cluster-1", Namespace: "scylla", DatacenterName: "dc1", RackName: "rack1"},
	}

	clusterKey := engine.ScopeKey{Namespace: "scylla", Name: "my-cluster"}
	enrichment := buildNodeEnrichment(nodes, vitals, clusterKey)

	// Node 1 should have HostID from its own local, and IP from node 2's peers.
	e1 := enrichment[node1Key]
	if e1.hostID != "host-id-1" {
		t.Errorf("node1 hostID = %q, want 'host-id-1'", e1.hostID)
	}
	if e1.ip != "10.0.0.1" {
		t.Errorf("node1 ip = %q, want '10.0.0.1'", e1.ip)
	}

	// Node 2 should have HostID from its own local, and IP from node 1's peers.
	e2 := enrichment[node2Key]
	if e2.hostID != "host-id-2" {
		t.Errorf("node2 hostID = %q, want 'host-id-2'", e2.hostID)
	}
	if e2.ip != "10.0.0.2" {
		t.Errorf("node2 ip = %q, want '10.0.0.2'", e2.ip)
	}
}

func TestNodeEnrichment_GracefulFallback(t *testing.T) {
	vitals := engine.NewVitals()

	// No SystemPeersLocal or SystemTopology results.
	nodes := []engine.ScyllaNodeInfo{
		{Name: "my-cluster-0", Namespace: "scylla", DatacenterName: "dc1", RackName: "rack1"},
	}

	clusterKey := engine.ScopeKey{Namespace: "scylla", Name: "my-cluster"}
	enrichment := buildNodeEnrichment(nodes, vitals, clusterKey)

	nodeKey := engine.ScopeKey{Namespace: "scylla", Name: "my-cluster-0"}
	e := enrichment[nodeKey]
	if e.hostID != "" {
		t.Errorf("hostID = %q, want empty", e.hostID)
	}
	if e.ip != "" {
		t.Errorf("ip = %q, want empty", e.ip)
	}
}

func TestRenderHTML_ProducesValidOutput(t *testing.T) {
	data := &HTMLReportData{
		Metadata: HTMLMetadata{
			Profile:     "full",
			Timestamp:   "2025-01-01T00:00:00Z",
			ToolVersion: "0.1.0",
		},
		Clusters: []HTMLCluster{
			{
				Name:      "my-cluster",
				Namespace: "scylla",
				Kind:      "ScyllaCluster",
				NodeCount: 1,
				Datacenters: []HTMLDatacenter{
					{
						Name: "dc1",
						Racks: []HTMLRack{
							{
								Name: "rack1",
								Nodes: []HTMLNode{
									{
										PodName:        "my-cluster-0",
										Namespace:      "scylla",
										IP:             "10.0.0.1",
										HostID:         "abc123def456",
										DatacenterName: "dc1",
										RackName:       "rack1",
										Collectors: []HTMLCollectorResult{
											{
												ID:       "OSInfoCollector",
												Name:     "OS Information",
												Status:   "passed",
												Message:  "Ubuntu 22.04",
												Duration: "150ms",
												Artifacts: []HTMLArtifact{
													{
														URL:         "/artifacts/per-scylla-node/scylla/my-cluster-0/OSInfoCollector/os-info.json",
														Description: "OS release info",
														DisplayPath: "os-info.json",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		ClusterWide: []HTMLCollectorResult{
			{
				ID:      "NodeResourcesCollector",
				Name:    "Kubernetes Node Resources",
				Status:  "passed",
				Message: "Collected 3 nodes",
			},
		},
		Analysis: []HTMLAnalysisGroup{
			{
				ID:   "ScyllaVersionSupportAnalyzer",
				Name: "Scylla Version Support",
				Results: []HTMLAnalysisResult{
					{Scope: "scylla/my-cluster", Status: "passed", Message: "ScyllaDB 6.2.2 is supported"},
				},
			},
		},
		TotalNodes:      1,
		TotalCollectors: 2,
		PassedAnalyzers: 1,
	}

	htmlBytes, err := RenderHTML(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	html := string(htmlBytes)

	// Verify basic structure.
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("missing DOCTYPE")
	}
	if !strings.Contains(html, "ScyllaDB Diagnostics Report") {
		t.Error("missing title")
	}

	// Verify cluster data appears.
	if !strings.Contains(html, "my-cluster") {
		t.Error("missing cluster name")
	}
	if !strings.Contains(html, "dc1") {
		t.Error("missing datacenter name")
	}
	if !strings.Contains(html, "rack1") {
		t.Error("missing rack name")
	}
	if !strings.Contains(html, "my-cluster-0") {
		t.Error("missing node name")
	}
	if !strings.Contains(html, "10.0.0.1") {
		t.Error("missing node IP")
	}

	// Verify analysis.
	if !strings.Contains(html, "Scylla Version Support") {
		t.Error("missing analyzer name")
	}
	if !strings.Contains(html, "ScyllaDB 6.2.2 is supported") {
		t.Error("missing analyzer message")
	}

	// Verify Tailwind CSS is loaded.
	if !strings.Contains(html, "cdn.tailwindcss.com") {
		t.Error("missing Tailwind CSS CDN")
	}

	// Verify artifact URL appears. Go's html/template escapes forward slashes
	// in JavaScript string contexts (onclick handlers), so we check for the
	// escaped form.
	if !strings.Contains(html, `\/artifacts\/per-scylla-node\/scylla\/my-cluster-0\/OSInfoCollector\/os-info.json`) {
		t.Error("missing artifact URL")
	}
}

func TestRenderHTML_EmptyData(t *testing.T) {
	data := &HTMLReportData{}

	htmlBytes, err := RenderHTML(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	html := string(htmlBytes)
	if !strings.Contains(html, "No diagnostic data found") {
		t.Error("expected empty state message")
	}
}

func TestBuildAnalysisResults_SortedOutput(t *testing.T) {
	report := &JSONReport{
		Analysis: map[engine.AnalyzerID]map[string]*JSONAnalyzerResult{
			"ZAnalyzer": {
				"ns-b/cluster-b": {Status: "passed", Message: "ok"},
				"ns-a/cluster-a": {Status: "failed", Message: "bad"},
			},
			"AAnalyzer": {
				"": {Status: "warning", Message: "hmm"},
			},
		},
	}

	names := map[engine.AnalyzerID]string{
		"AAnalyzer": "A Analyzer",
		"ZAnalyzer": "Z Analyzer",
	}

	groups := buildAnalysisResults(report, names)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}

	// Should be sorted by ID: A before Z.
	if groups[0].ID != "AAnalyzer" {
		t.Errorf("first group = %q, want 'AAnalyzer'", groups[0].ID)
	}
	if groups[1].ID != "ZAnalyzer" {
		t.Errorf("second group = %q, want 'ZAnalyzer'", groups[1].ID)
	}

	// ZAnalyzer results should be sorted by scope key.
	if len(groups[1].Results) != 2 {
		t.Fatalf("ZAnalyzer results = %d, want 2", len(groups[1].Results))
	}
	if groups[1].Results[0].Scope != "ns-a/cluster-a" {
		t.Errorf("first scope = %q, want 'ns-a/cluster-a'", groups[1].Results[0].Scope)
	}
	if groups[1].Results[1].Scope != "ns-b/cluster-b" {
		t.Errorf("second scope = %q, want 'ns-b/cluster-b'", groups[1].Results[1].Scope)
	}
}
