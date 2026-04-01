package engine

// ClusterTopologyFromVitals reconstructs the ScyllaClusterInfo and pod topology
// needed for offline analyzer dispatch. It prefers the Topology embedded in
// SerializableVitals (populated during a live run) for accuracy. When Topology
// is absent (older archives), it falls back to inferring one cluster per distinct
// namespace from the PerScyllaNode keys — a best-effort approximation sufficient for
// single-cluster namespaces.
func ClusterTopologyFromVitals(sv *SerializableVitals, vitals *Vitals) ([]ScyllaClusterInfo, map[ScopeKey][]ScyllaNodeInfo) {
	// Preferred path: use the stored topology from the live run.
	if sv.Topology != nil && len(sv.Topology.Clusters) > 0 {
		clusterInfos := make([]ScyllaClusterInfo, 0, len(sv.Topology.Clusters))
		for _, ci := range sv.Topology.Clusters {
			clusterInfos = append(clusterInfos, ScyllaClusterInfo{
				Namespace:  ci.Namespace,
				Name:       ci.Name,
				Kind:       ci.Kind,
				APIVersion: ci.APIVersion,
			})
		}
		podsByCluster := make(map[ScopeKey][]ScyllaNodeInfo, len(sv.Topology.ScyllaNodes))
		for key, snodes := range sv.Topology.ScyllaNodes {
			nodes := make([]ScyllaNodeInfo, 0, len(snodes))
			for _, sn := range snodes {
				nodes = append(nodes, ScyllaNodeInfo{
					Namespace:      sn.Namespace,
					Name:           sn.Name,
					ClusterName:    sn.ClusterName,
					DatacenterName: sn.DatacenterName,
					RackName:       sn.RackName,
				})
			}
			podsByCluster[key] = nodes
		}
		return clusterInfos, podsByCluster
	}

	// Fallback: infer topology from PerScyllaCluster keys (if any) and PerScyllaNode keys.
	// This handles archives produced before topology was stored in vitals.json.
	clusterKeys := vitals.ScyllaClusterKeys()
	clusterInfos := make([]ScyllaClusterInfo, 0, len(clusterKeys))
	for _, key := range clusterKeys {
		clusterInfos = append(clusterInfos, ScyllaClusterInfo{
			Namespace: key.Namespace,
			Name:      key.Name,
		})
	}

	podKeys := vitals.ScyllaNodeKeys()

	// If there are no PerScyllaCluster keys, synthesize one cluster per distinct
	// namespace seen in the PerScyllaNode keys.  This is the common case when only
	// PerScyllaNode-scope collectors ran (e.g. the default full profile today).
	if len(clusterKeys) == 0 && len(podKeys) > 0 {
		seen := make(map[string]bool)
		for _, podKey := range podKeys {
			if !seen[podKey.Namespace] {
				seen[podKey.Namespace] = true
				syntheticKey := ScopeKey{Namespace: podKey.Namespace, Name: podKey.Namespace}
				clusterKeys = append(clusterKeys, syntheticKey)
				clusterInfos = append(clusterInfos, ScyllaClusterInfo{
					Namespace: podKey.Namespace,
					Name:      podKey.Namespace, // best-effort; real name not available
				})
			}
		}
	}

	podsByCluster := make(map[ScopeKey][]ScyllaNodeInfo)
	for _, podKey := range podKeys {
		// Pods are associated with clusters that share the same namespace.
		// If multiple clusters share a namespace, pods go to the first match.
		for _, clusterKey := range clusterKeys {
			if clusterKey.Namespace == podKey.Namespace {
				podsByCluster[clusterKey] = append(podsByCluster[clusterKey], ScyllaNodeInfo{
					Namespace: podKey.Namespace,
					Name:      podKey.Name,
				})
				break
			}
		}
	}

	return clusterInfos, podsByCluster
}
