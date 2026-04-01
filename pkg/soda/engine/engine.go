package engine

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// CollectorEventKind distinguishes the two progress events emitted per collector invocation.
type CollectorEventKind int

const (
	// CollectorEventStarted is emitted immediately before a collector runs.
	CollectorEventStarted CollectorEventKind = iota
	// CollectorEventFinished is emitted immediately after a collector completes.
	CollectorEventFinished
)

// CollectorEvent carries progress information for a single collector invocation.
// It is passed to EngineConfig.OnCollectorEvent after each start/finish.
type CollectorEvent struct {
	Kind          CollectorEventKind
	CollectorID   CollectorID
	CollectorName string
	Scope         CollectorScope
	ScopeKey      ScopeKey // Empty for ClusterWide collectors.

	// Only populated for CollectorEventFinished.
	Result *CollectorResult
}

// EngineConfig holds all inputs needed to construct and run the diagnostic engine.
type EngineConfig struct {
	// Registry
	AllCollectors map[CollectorID]CollectorMeta
	AllAnalyzers  map[AnalyzerID]AnalyzerMeta
	AllProfiles   map[string]Profile

	// Selection
	ProfileName string
	Enable      []AnalyzerID
	Disable     []AnalyzerID

	// Targets
	ScyllaClusters []ScyllaClusterInfo
	ScyllaNodes    map[ScopeKey][]ScyllaNodeInfo // ScyllaCluster key → Scylla nodes for that cluster

	// Dependency-injected capabilities
	PodExecutor    PodExecutor
	PodLogFetcher  PodLogFetcher
	ResourceLister ResourceLister

	// Artifact management
	ArtifactWriterFactory ArtifactWriterFactory

	// Progress reporting. If non-nil, called synchronously before and after
	// each collector invocation. Implementations must be safe for concurrent
	// use if the engine is ever parallelised in the future.
	OnCollectorEvent func(event CollectorEvent)

	// Behavior
	KeepGoing bool
}

// ArtifactWriterFactory creates ArtifactWriters for specific collector invocations.
type ArtifactWriterFactory interface {
	// NewWriter creates an ArtifactWriter rooted at the correct subdirectory
	// for the given collector, scope, and scope key.
	NewWriter(collectorID CollectorID, scope CollectorScope, scopeKey ScopeKey) ArtifactWriter
}

// EngineResult holds the complete results of a diagnostic engine run.
type EngineResult struct {
	Vitals *Vitals
	// AnalyzerResults maps analyzer ID → scope key → result.
	// For AnalyzerClusterWide analyzers the inner map has a single entry with an empty ScopeKey.
	// For AnalyzerPerScyllaCluster analyzers the inner map has one entry per ScyllaCluster.
	AnalyzerResults map[AnalyzerID]map[ScopeKey]*AnalyzerResult

	// Metadata for output.
	ResolvedCollectors []CollectorID
	ResolvedAnalyzers  []AnalyzerID
}

// Engine is the diagnostic orchestrator that resolves dependencies, runs
// collectors in topological order, and then runs analyzers.
type Engine struct {
	config EngineConfig
}

// NewEngine creates a new diagnostic engine with the given configuration.
func NewEngine(config EngineConfig) *Engine {
	return &Engine{config: config}
}

// Run executes the full diagnostic pipeline:
// 1. Resolve profile → final collector and analyzer sets
// 2. Topological sort collectors
// 3. Execute collectors by scope
// 4. Execute analyzers
func (e *Engine) Run(ctx context.Context) (*EngineResult, error) {
	// Step 1: Resolve profile.
	resolvedCollectors, resolvedAnalyzers, err := ResolveProfile(
		e.config.ProfileName,
		e.config.AllProfiles,
		e.config.Enable,
		e.config.Disable,
		e.config.AllAnalyzers,
		e.config.AllCollectors,
	)
	if err != nil {
		return nil, fmt.Errorf("resolving profile: %w", err)
	}

	// Step 2: Topological sort collectors.
	sortedCollectors, err := topoSortCollectors(resolvedCollectors, e.config.AllCollectors)
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}

	// Step 3: Execute collectors.
	vitals := NewVitals()
	e.executeCollectors(ctx, sortedCollectors, vitals)

	// Step 4: Execute analyzers.
	// In live mode there is no pre-existing artifact reader; analyzers that need
	// to read artifacts written by collectors can implement that access directly.
	analyzerResults := e.executeAnalyzers(resolvedAnalyzers, vitals, nil)

	return &EngineResult{
		Vitals:             vitals,
		AnalyzerResults:    analyzerResults,
		ResolvedCollectors: resolvedCollectors,
		ResolvedAnalyzers:  resolvedAnalyzers,
	}, nil
}

// executeCollectors runs all collectors in topological order, grouped by scope.
func (e *Engine) executeCollectors(ctx context.Context, sortedCollectors []CollectorID, vitals *Vitals) {
	for _, collectorID := range sortedCollectors {
		collector := e.config.AllCollectors[collectorID]

		switch collector.Scope() {
		case ClusterWide:
			e.executeClusterWideCollector(ctx, collector, vitals)

		case PerScyllaCluster:
			for _, cluster := range e.config.ScyllaClusters {
				clusterKey := ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
				e.executePerScyllaClusterCollector(ctx, collector, &cluster, clusterKey, vitals)
			}

		case PerScyllaNode:
			for _, cluster := range e.config.ScyllaClusters {
				clusterKey := ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
				nodes := e.config.ScyllaNodes[clusterKey]
				for _, node := range nodes {
					nodeKey := ScopeKey{Namespace: node.Namespace, Name: node.Name}
					e.executePerScyllaNodeCollector(ctx, collector, &cluster, &node, nodeKey, vitals)
				}
			}
		}
	}
}

// emitEvent calls OnCollectorEvent if one is configured.
func (e *Engine) emitEvent(event CollectorEvent) {
	if e.config.OnCollectorEvent != nil {
		e.config.OnCollectorEvent(event)
	}
}

func (e *Engine) executeClusterWideCollector(ctx context.Context, collector CollectorMeta, vitals *Vitals) {
	scopeKey := ScopeKey{} // Empty for ClusterWide.

	// Check cascade: if any dependency failed/skipped, cascade.
	if result := e.checkCascade(collector, vitals, scopeKey); result != nil {
		e.emitEvent(CollectorEvent{Kind: CollectorEventStarted, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: ClusterWide, ScopeKey: scopeKey})
		e.emitEvent(CollectorEvent{Kind: CollectorEventFinished, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: ClusterWide, ScopeKey: scopeKey, Result: result})
		vitals.Store(collector.ID(), ClusterWide, scopeKey, result)
		return
	}

	var artifactWriter ArtifactWriter
	if e.config.ArtifactWriterFactory != nil {
		artifactWriter = e.config.ArtifactWriterFactory.NewWriter(collector.ID(), ClusterWide, scopeKey)
	}

	e.emitEvent(CollectorEvent{Kind: CollectorEventStarted, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: ClusterWide, ScopeKey: scopeKey})
	start := time.Now()

	var result *CollectorResult
	var err error

	switch c := collector.(type) {
	case ClusterWideCollector:
		result, err = c.CollectClusterWide(ctx, ClusterWideCollectorParams{
			Vitals:         vitals,
			ResourceLister: e.config.ResourceLister,
			PodLogFetcher:  e.config.PodLogFetcher,
			ArtifactWriter: artifactWriter,
		})
	case Collector:
		result, err = c.Collect(ctx, CollectorParams{
			Vitals:         vitals,
			PodExecutor:    e.config.PodExecutor,
			PodLogFetcher:  e.config.PodLogFetcher,
			ResourceLister: e.config.ResourceLister,
			ArtifactWriter: artifactWriter,
		})
	default:
		err = fmt.Errorf("collector %s does not implement ClusterWideCollector", collector.ID())
	}

	if err != nil {
		result = &CollectorResult{
			Status:  CollectorFailed,
			Message: fmt.Sprintf("collector error: %v", err),
		}
	}
	result.Duration = time.Since(start)
	e.emitEvent(CollectorEvent{Kind: CollectorEventFinished, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: ClusterWide, ScopeKey: scopeKey, Result: result})

	vitals.Store(collector.ID(), ClusterWide, scopeKey, result)
}

func (e *Engine) executePerScyllaClusterCollector(ctx context.Context, collector CollectorMeta, cluster *ScyllaClusterInfo, clusterKey ScopeKey, vitals *Vitals) {
	if result := e.checkCascade(collector, vitals, clusterKey); result != nil {
		e.emitEvent(CollectorEvent{Kind: CollectorEventStarted, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: PerScyllaCluster, ScopeKey: clusterKey})
		e.emitEvent(CollectorEvent{Kind: CollectorEventFinished, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: PerScyllaCluster, ScopeKey: clusterKey, Result: result})
		vitals.Store(collector.ID(), PerScyllaCluster, clusterKey, result)
		return
	}

	var artifactWriter ArtifactWriter
	if e.config.ArtifactWriterFactory != nil {
		artifactWriter = e.config.ArtifactWriterFactory.NewWriter(collector.ID(), PerScyllaCluster, clusterKey)
	}

	e.emitEvent(CollectorEvent{Kind: CollectorEventStarted, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: PerScyllaCluster, ScopeKey: clusterKey})
	start := time.Now()

	var result *CollectorResult
	var err error

	switch c := collector.(type) {
	case PerScyllaClusterCollector:
		result, err = c.CollectPerScyllaCluster(ctx, PerScyllaClusterCollectorParams{
			Vitals:         vitals,
			ScyllaCluster:  cluster,
			ResourceLister: e.config.ResourceLister,
			PodLogFetcher:  e.config.PodLogFetcher,
			ArtifactWriter: artifactWriter,
		})
	case Collector:
		result, err = c.Collect(ctx, CollectorParams{
			Vitals:         vitals,
			ScyllaCluster:  cluster,
			PodExecutor:    e.config.PodExecutor,
			PodLogFetcher:  e.config.PodLogFetcher,
			ResourceLister: e.config.ResourceLister,
			ArtifactWriter: artifactWriter,
		})
	default:
		err = fmt.Errorf("collector %s does not implement PerScyllaClusterCollector", collector.ID())
	}

	if err != nil {
		result = &CollectorResult{
			Status:  CollectorFailed,
			Message: fmt.Sprintf("collector error: %v", err),
		}
	}
	result.Duration = time.Since(start)
	e.emitEvent(CollectorEvent{Kind: CollectorEventFinished, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: PerScyllaCluster, ScopeKey: clusterKey, Result: result})

	vitals.Store(collector.ID(), PerScyllaCluster, clusterKey, result)
}

func (e *Engine) executePerScyllaNodeCollector(ctx context.Context, collector CollectorMeta, cluster *ScyllaClusterInfo, node *ScyllaNodeInfo, nodeKey ScopeKey, vitals *Vitals) {
	if result := e.checkCascade(collector, vitals, nodeKey); result != nil {
		e.emitEvent(CollectorEvent{Kind: CollectorEventStarted, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: PerScyllaNode, ScopeKey: nodeKey})
		e.emitEvent(CollectorEvent{Kind: CollectorEventFinished, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: PerScyllaNode, ScopeKey: nodeKey, Result: result})
		vitals.Store(collector.ID(), PerScyllaNode, nodeKey, result)
		return
	}

	var artifactWriter ArtifactWriter
	if e.config.ArtifactWriterFactory != nil {
		artifactWriter = e.config.ArtifactWriterFactory.NewWriter(collector.ID(), PerScyllaNode, nodeKey)
	}

	e.emitEvent(CollectorEvent{Kind: CollectorEventStarted, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: PerScyllaNode, ScopeKey: nodeKey})
	start := time.Now()

	var result *CollectorResult
	var err error

	switch c := collector.(type) {
	case PerScyllaNodeCollector:
		result, err = c.CollectPerScyllaNode(ctx, PerScyllaNodeCollectorParams{
			Vitals:         vitals,
			ScyllaCluster:  cluster,
			ScyllaNode:     node,
			PodExecutor:    e.config.PodExecutor,
			PodLogFetcher:  e.config.PodLogFetcher,
			ResourceLister: e.config.ResourceLister,
			ArtifactWriter: artifactWriter,
		})
	case Collector:
		result, err = c.Collect(ctx, CollectorParams{
			Vitals:         vitals,
			ScyllaCluster:  cluster,
			ScyllaNode:     node,
			PodExecutor:    e.config.PodExecutor,
			PodLogFetcher:  e.config.PodLogFetcher,
			ResourceLister: e.config.ResourceLister,
			ArtifactWriter: artifactWriter,
		})
	default:
		err = fmt.Errorf("collector %s does not implement PerScyllaNodeCollector", collector.ID())
	}

	if err != nil {
		result = &CollectorResult{
			Status:  CollectorFailed,
			Message: fmt.Sprintf("collector error: %v", err),
		}
	}
	result.Duration = time.Since(start)
	e.emitEvent(CollectorEvent{Kind: CollectorEventFinished, CollectorID: collector.ID(), CollectorName: collector.Name(), Scope: PerScyllaNode, ScopeKey: nodeKey, Result: result})

	vitals.Store(collector.ID(), PerScyllaNode, nodeKey, result)
}

// checkCascade checks if any of the collector's dependencies have failed or
// been skipped, and returns an appropriate cascade result. Returns nil if all
// dependencies passed (i.e., the collector should proceed normally).
func (e *Engine) checkCascade(collector CollectorMeta, vitals *Vitals, scopeKey ScopeKey) *CollectorResult {
	for _, depID := range collector.DependsOn() {
		depCollector := e.config.AllCollectors[depID]

		// For dependencies with a broader scope, use an appropriate key.
		depKey := scopeKey
		if depCollector.Scope() == ClusterWide {
			depKey = ScopeKey{} // ClusterWide uses empty key.
		}

		result, ok := vitals.Get(depID, depKey)
		if !ok {
			// Dependency result not found — shouldn't happen if topo sort is correct,
			// but treat as failed.
			return &CollectorResult{
				Status:  CollectorFailed,
				Message: fmt.Sprintf("required %s result not found", depID),
			}
		}

		switch result.Status {
		case CollectorSkipped:
			return &CollectorResult{
				Status:  CollectorSkipped,
				Message: fmt.Sprintf("required %s was skipped", depID),
			}
		case CollectorFailed:
			return &CollectorResult{
				Status:  CollectorFailed,
				Message: fmt.Sprintf("required %s failed: %s", depID, result.Message),
			}
		}
	}
	return nil // All dependencies passed.
}

// OfflineRun skips the collection phase entirely and runs analyzers against
// pre-loaded vitals (typically deserialized from a vitals.json stored in a
// previous live run or archive). The artifactReader is passed through to each
// analyzer so they can access raw artifact files written during the original run.
//
// The EngineConfig must still have AllCollectors, AllAnalyzers, AllProfiles,
// ProfileName, Enable, Disable, ScyllaClusters, and Pods populated (they are
// used for profile resolution and per-cluster analyzer dispatch). The Kubernetes
// client fields and ArtifactWriterFactory are not used.
func (e *Engine) OfflineRun(ctx context.Context, vitals *Vitals, artifactReader ArtifactReader) (*EngineResult, error) {
	// Resolve the profile to determine which analyzers to run.
	resolvedCollectors, resolvedAnalyzers, err := ResolveProfile(
		e.config.ProfileName,
		e.config.AllProfiles,
		e.config.Enable,
		e.config.Disable,
		e.config.AllAnalyzers,
		e.config.AllCollectors,
	)
	if err != nil {
		return nil, fmt.Errorf("resolving profile: %w", err)
	}

	// Run analyzers against the pre-loaded vitals.
	analyzerResults := e.executeAnalyzers(resolvedAnalyzers, vitals, artifactReader)

	return &EngineResult{
		Vitals:             vitals,
		AnalyzerResults:    analyzerResults,
		ResolvedCollectors: resolvedCollectors,
		ResolvedAnalyzers:  resolvedAnalyzers,
	}, nil
}

// executeAnalyzers runs all enabled analyzers against the collected vitals.
// For AnalyzerClusterWide analyzers the result is stored under an empty ScopeKey.
// For AnalyzerPerScyllaCluster analyzers the analyzer is invoked once per ScyllaCluster
// with vitals filtered to that cluster's pods only.
// artifactReader may be nil (live mode), in which case analyzers receive a nil reader.
func (e *Engine) executeAnalyzers(analyzerIDs []AnalyzerID, vitals *Vitals, artifactReader ArtifactReader) map[AnalyzerID]map[ScopeKey]*AnalyzerResult {
	results := make(map[AnalyzerID]map[ScopeKey]*AnalyzerResult, len(analyzerIDs))

	for _, analyzerID := range analyzerIDs {
		analyzer := e.config.AllAnalyzers[analyzerID]

		switch analyzer.Scope() {
		case AnalyzerPerScyllaCluster:
			inner := make(map[ScopeKey]*AnalyzerResult, len(e.config.ScyllaClusters))
			for _, cluster := range e.config.ScyllaClusters {
				clusterKey := ScopeKey{Namespace: cluster.Namespace, Name: cluster.Name}
				scyllaNodeKeys := make([]ScopeKey, 0, len(e.config.ScyllaNodes[clusterKey]))
				for _, node := range e.config.ScyllaNodes[clusterKey] {
					scyllaNodeKeys = append(scyllaNodeKeys, ScopeKey{Namespace: node.Namespace, Name: node.Name})
				}
				scopedVitals := vitals.ForScyllaCluster(clusterKey, scyllaNodeKeys)

				if result := e.checkAnalyzerDeps(analyzer, scopedVitals); result != nil {
					inner[clusterKey] = result
					continue
				}

				clusterCopy := cluster
				var result *AnalyzerResult
				switch a := analyzer.(type) {
				case PerScyllaClusterAnalyzer:
					result = a.AnalyzePerScyllaCluster(PerScyllaClusterAnalyzerParams{
						Vitals:         scopedVitals,
						ScyllaCluster:  &clusterCopy,
						ArtifactReader: artifactReader,
					})
				case Analyzer:
					result = a.Analyze(AnalyzerParams{
						Vitals:         scopedVitals,
						ScyllaCluster:  &clusterCopy,
						ArtifactReader: artifactReader,
					})
				default:
					result = &AnalyzerResult{
						Status:  AnalyzerFailed,
						Message: fmt.Sprintf("analyzer %s does not implement PerScyllaClusterAnalyzer", analyzer.ID()),
					}
				}
				inner[clusterKey] = result
			}
			// If no ScyllaClusters configured, produce a single skipped result.
			if len(e.config.ScyllaClusters) == 0 {
				inner[ScopeKey{}] = &AnalyzerResult{
					Status:  AnalyzerSkipped,
					Message: "no ScyllaClusters configured",
				}
			}
			results[analyzerID] = inner

		default: // AnalyzerClusterWide
			if result := e.checkAnalyzerDeps(analyzer, vitals); result != nil {
				results[analyzerID] = map[ScopeKey]*AnalyzerResult{ScopeKey{}: result}
				continue
			}

			var result *AnalyzerResult
			switch a := analyzer.(type) {
			case ClusterWideAnalyzer:
				result = a.AnalyzeClusterWide(ClusterWideAnalyzerParams{
					Vitals:         vitals,
					ArtifactReader: artifactReader,
				})
			case Analyzer:
				result = a.Analyze(AnalyzerParams{
					Vitals:         vitals,
					ArtifactReader: artifactReader,
				})
			default:
				result = &AnalyzerResult{
					Status:  AnalyzerFailed,
					Message: fmt.Sprintf("analyzer %s does not implement ClusterWideAnalyzer", analyzer.ID()),
				}
			}
			results[analyzerID] = map[ScopeKey]*AnalyzerResult{ScopeKey{}: result}
		}
	}

	return results
}

// checkAnalyzerDeps checks whether an analyzer's collector dependencies have
// at least one passed result. Returns a skip/fail result if not, or nil if
// the analyzer should proceed.
func (e *Engine) checkAnalyzerDeps(analyzer AnalyzerMeta, vitals *Vitals) *AnalyzerResult {
	for _, depID := range analyzer.DependsOn() {
		depCollector, ok := e.config.AllCollectors[depID]
		if !ok {
			return &AnalyzerResult{
				Status:  AnalyzerFailed,
				Message: fmt.Sprintf("required collector %s not registered", depID),
			}
		}

		hasAnyResult := false
		hasSkipped := false
		hasPassed := false

		switch depCollector.Scope() {
		case ClusterWide:
			if result, ok := vitals.Get(depID, ScopeKey{}); ok {
				hasAnyResult = true
				switch result.Status {
				case CollectorPassed:
					hasPassed = true
				case CollectorSkipped:
					hasSkipped = true
				}
			}

		case PerScyllaCluster:
			for _, key := range vitals.ScyllaClusterKeys() {
				if result, ok := vitals.Get(depID, key); ok {
					hasAnyResult = true
					switch result.Status {
					case CollectorPassed:
						hasPassed = true
					case CollectorSkipped:
						hasSkipped = true
					}
				}
			}

		case PerScyllaNode:
			for _, key := range vitals.ScyllaNodeKeys() {
				if result, ok := vitals.Get(depID, key); ok {
					hasAnyResult = true
					switch result.Status {
					case CollectorPassed:
						hasPassed = true
					case CollectorSkipped:
						hasSkipped = true
					}
				}
			}
		}

		if !hasAnyResult {
			return &AnalyzerResult{
				Status:  AnalyzerFailed,
				Message: fmt.Sprintf("required collector %s has no results", depID),
			}
		}

		if !hasPassed {
			if hasSkipped {
				return &AnalyzerResult{
					Status:  AnalyzerSkipped,
					Message: fmt.Sprintf("required collector %s was skipped", depID),
				}
			}
			return &AnalyzerResult{
				Status:  AnalyzerFailed,
				Message: fmt.Sprintf("required collector %s failed", depID),
			}
		}
	}
	return nil // All dependencies have at least one passed result.
}

// topoSortCollectors performs a topological sort on the given collector IDs
// based on their dependency relationships. Returns an error if a cycle is detected.
func topoSortCollectors(ids []CollectorID, allCollectors map[CollectorID]CollectorMeta) ([]CollectorID, error) {
	// Build adjacency list and in-degree map only for the resolved set.
	idSet := make(map[CollectorID]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	inDegree := make(map[CollectorID]int, len(ids))
	dependents := make(map[CollectorID][]CollectorID, len(ids)) // dep → things that depend on it

	for _, id := range ids {
		inDegree[id] = 0
	}

	for _, id := range ids {
		collector := allCollectors[id]
		for _, depID := range collector.DependsOn() {
			if idSet[depID] {
				inDegree[id]++
				dependents[depID] = append(dependents[depID], id)
			}
		}
	}

	// Kahn's algorithm.
	var queue []CollectorID
	for _, id := range ids {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	// Sort the initial queue for deterministic output.
	sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })

	var sorted []CollectorID
	for len(queue) > 0 {
		// Pick the lexicographically smallest ready node for determinism.
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		deps := dependents[current]
		sort.Slice(deps, func(i, j int) bool { return deps[i] < deps[j] })
		for _, depID := range deps {
			inDegree[depID]--
			if inDegree[depID] == 0 {
				queue = append(queue, depID)
				// Re-sort to maintain deterministic order.
				sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })
			}
		}
	}

	if len(sorted) != len(ids) {
		return nil, fmt.Errorf("cycle detected in collector dependencies")
	}

	return sorted, nil
}
