package output

import (
	"bytes"
	"fmt"
	"html/template"
)

// htmlTemplateSrc is the complete HTML template for the interactive diagnostic report.
// It uses Tailwind CSS from CDN and vanilla JavaScript for interactivity.
const htmlTemplateSrc = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>ScyllaDB Diagnostics Report</title>
<script src="https://cdn.tailwindcss.com"></script>
<style>
  /* Custom scrollbar for artifact viewer */
  .artifact-content::-webkit-scrollbar { width: 8px; height: 8px; }
  .artifact-content::-webkit-scrollbar-track { background: #1e293b; }
  .artifact-content::-webkit-scrollbar-thumb { background: #475569; border-radius: 4px; }
  .artifact-content::-webkit-scrollbar-thumb:hover { background: #64748b; }

  /* Smooth transitions */
  .collapse-content { max-height: 0; overflow: hidden; transition: max-height 0.3s ease-out; }
  .collapse-content.open { max-height: none; }

  /* Tree connector lines */
  .tree-item { position: relative; }
  .tree-item::before {
    content: '';
    position: absolute;
    left: -16px;
    top: 0;
    height: 100%;
    border-left: 1px solid #334155;
  }
  .tree-item:last-child::before { height: 20px; }
  .tree-item::after {
    content: '';
    position: absolute;
    left: -16px;
    top: 20px;
    width: 16px;
    border-top: 1px solid #334155;
  }
</style>
</head>
<body class="bg-slate-900 text-slate-200 min-h-screen">

<!-- Top navbar -->
<nav class="bg-slate-800 border-b border-slate-700 px-6 py-3 flex items-center justify-between sticky top-0 z-50">
  <div class="flex items-center gap-3">
    <svg class="w-7 h-7 text-indigo-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"/>
    </svg>
    <h1 class="text-lg font-semibold text-white">ScyllaDB Diagnostics Report</h1>
  </div>
  <div class="flex items-center gap-4 text-sm text-slate-400">
    {{if .Metadata.Profile}}<span>Profile: <span class="text-slate-200">{{.Metadata.Profile}}</span></span>{{end}}
    {{if .Metadata.Timestamp}}<span>{{.Metadata.Timestamp}}</span>{{end}}
    {{if .Metadata.ToolVersion}}<span>v{{.Metadata.ToolVersion}}</span>{{end}}
  </div>
</nav>

<div class="flex">
<!-- Sidebar -->
<aside class="w-72 min-h-screen bg-slate-800 border-r border-slate-700 p-4 overflow-y-auto sticky top-12 h-[calc(100vh-3rem)]">
  <!-- Search -->
  <div class="mb-4">
    <input type="text" id="searchInput" placeholder="Filter..."
      class="w-full bg-slate-700 border border-slate-600 rounded px-3 py-2 text-sm text-slate-200 placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-indigo-500"
      oninput="filterTree(this.value)">
  </div>

  <!-- Summary cards -->
  <div class="grid grid-cols-2 gap-2 mb-4">
    <div class="bg-slate-700 rounded p-2 text-center">
      <div class="text-lg font-bold text-white">{{len .Clusters}}</div>
      <div class="text-xs text-slate-400">Clusters</div>
    </div>
    <div class="bg-slate-700 rounded p-2 text-center">
      <div class="text-lg font-bold text-white">{{.TotalNodes}}</div>
      <div class="text-xs text-slate-400">Nodes</div>
    </div>
    <div class="bg-slate-700 rounded p-2 text-center">
      <div class="text-lg font-bold text-white">{{.TotalCollectors}}</div>
      <div class="text-xs text-slate-400">Collectors</div>
    </div>
    <div class="bg-slate-700 rounded p-2 text-center">
      <div class="text-lg font-bold text-white">{{addInts .PassedAnalyzers .WarningAnalyzers .FailedAnalyzers .SkippedAnalyzers}}</div>
      <div class="text-xs text-slate-400">Analyses</div>
    </div>
  </div>

  <!-- Navigation tree -->
  <nav class="text-sm" id="navTree">
    <!-- Analysis link (first) -->
    {{if .Analysis}}
    <a href="#analysis" class="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-slate-700 text-slate-300 mb-1 nav-item" data-search="analysis analyzers">
      <svg class="w-4 h-4 text-amber-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"/>
      </svg>
      Analysis Results
    </a>
    {{end}}

    <!-- Per-cluster trees -->
    {{range $ci, $cluster := .Clusters}}
    <div class="mb-2 nav-cluster" data-search="{{$cluster.Namespace}} {{$cluster.Name}}">
      <button onclick="toggleNav('cluster-{{$ci}}')" class="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-slate-700 text-slate-300 w-full text-left">
        <svg class="w-4 h-4 text-indigo-400 shrink-0 transition-transform" id="chevron-cluster-{{$ci}}">
          <path fill="currentColor" d="M6 9l6 6 6-6"/>
        </svg>
        <span class="truncate" title="{{$cluster.Namespace}}/{{$cluster.Name}}">{{$cluster.Namespace}}/{{$cluster.Name}}</span>
        <span class="ml-auto text-xs text-slate-500">{{$cluster.NodeCount}}</span>
      </button>
      <div class="pl-4 collapse-content open" id="nav-cluster-{{$ci}}">
        {{range $di, $dc := $cluster.Datacenters}}
        <div class="nav-dc" data-search="{{$dc.Name}}">
          <button onclick="toggleNav('dc-{{$ci}}-{{$di}}')" class="flex items-center gap-2 px-2 py-1 rounded hover:bg-slate-700 text-slate-400 w-full text-left text-xs">
            <svg class="w-3 h-3 text-emerald-400 shrink-0 transition-transform" id="chevron-dc-{{$ci}}-{{$di}}">
              <path fill="currentColor" d="M6 9l6 6 6-6"/>
            </svg>
            DC: {{$dc.Name}}
          </button>
          <div class="pl-4 collapse-content open" id="nav-dc-{{$ci}}-{{$di}}">
            {{range $ri, $rack := $dc.Racks}}
            <div class="nav-rack" data-search="{{$rack.Name}}">
              <div class="px-2 py-0.5 text-xs text-slate-500">Rack: {{$rack.Name}}</div>
              {{range $ni, $node := $rack.Nodes}}
              <a href="#node-{{$ci}}-{{$di}}-{{$ri}}-{{$ni}}"
                class="flex items-center gap-1.5 px-2 py-1 rounded hover:bg-slate-700 text-slate-400 text-xs nav-item"
                data-search="{{$node.PodName}} {{$node.IP}} {{$node.HostID}}">
                <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 shrink-0"></span>
                <span class="truncate">{{$node.PodName}}</span>
              </a>
              {{end}}
            </div>
            {{end}}
          </div>
        </div>
        {{end}}
      </div>
    </div>
    {{end}}

    <!-- Cluster-wide section (last) -->
    {{if .ClusterWide}}
    <a href="#cluster-wide" class="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-slate-700 text-slate-300 mt-2 nav-item" data-search="cluster-wide kubernetes">
      <svg class="w-4 h-4 text-blue-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064"/>
      </svg>
      Cluster-Wide
    </a>
    {{end}}
  </nav>
</aside>

<!-- Main content -->
<main class="flex-1 p-6 max-w-6xl">

  <!-- 1. Analysis Results (first) -->
  {{if .Analysis}}
  <section id="analysis" class="mb-8">
    <h2 class="text-xl font-semibold text-white mb-4 flex items-center gap-2">
      <svg class="w-5 h-5 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"/>
      </svg>
      Analysis Results
    </h2>

    <!-- Summary badges -->
    <div class="flex gap-3 mb-4">
      {{if .PassedAnalyzers}}<span class="bg-emerald-500/20 text-emerald-300 px-3 py-1 rounded text-sm">{{.PassedAnalyzers}} passed</span>{{end}}
      {{if .WarningAnalyzers}}<span class="bg-amber-500/20 text-amber-300 px-3 py-1 rounded text-sm">{{.WarningAnalyzers}} warning</span>{{end}}
      {{if .FailedAnalyzers}}<span class="bg-red-500/20 text-red-300 px-3 py-1 rounded text-sm">{{.FailedAnalyzers}} failed</span>{{end}}
      {{if .SkippedAnalyzers}}<span class="bg-slate-500/20 text-slate-400 px-3 py-1 rounded text-sm">{{.SkippedAnalyzers}} skipped</span>{{end}}
    </div>

    <div class="bg-slate-800 rounded-lg border border-slate-700 overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="bg-slate-750 border-b border-slate-700">
            <th class="text-left px-4 py-2 text-slate-400 font-medium">Analyzer</th>
            <th class="text-left px-4 py-2 text-slate-400 font-medium">Scope</th>
            <th class="text-left px-4 py-2 text-slate-400 font-medium">Status</th>
            <th class="text-left px-4 py-2 text-slate-400 font-medium">Message</th>
          </tr>
        </thead>
        <tbody>
          {{range .Analysis}}
          {{$name := .Name}}
          {{range .Results}}
          <tr class="border-b border-slate-700/50 hover:bg-slate-700/30">
            <td class="px-4 py-2 text-slate-300">{{$name}}</td>
            <td class="px-4 py-2 text-slate-400 font-mono text-xs">{{.Scope}}</td>
            <td class="px-4 py-2">{{template "statusBadge" .Status}}</td>
            <td class="px-4 py-2 text-slate-300">{{.Message}}</td>
          </tr>
          {{end}}
          {{end}}
        </tbody>
      </table>
    </div>
  </section>
  {{end}}

  <!-- 2. Per-Cluster Sections (middle) -->
  {{range $ci, $cluster := .Clusters}}
  <section id="cluster-{{$ci}}" class="mb-8">
    <div class="bg-slate-800 rounded-lg border border-slate-700 p-5 mb-4">
      <div class="flex items-center justify-between mb-2">
        <h2 class="text-xl font-semibold text-white flex items-center gap-2">
          <svg class="w-5 h-5 text-indigo-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/>
          </svg>
          {{$cluster.Namespace}}/{{$cluster.Name}}
        </h2>
        {{if $cluster.Kind}}
        <span class="text-xs bg-indigo-500/20 text-indigo-300 px-2 py-1 rounded">{{$cluster.Kind}}</span>
        {{end}}
      </div>
      <div class="text-sm text-slate-400">
        {{$cluster.NodeCount}} node(s) &middot;
        {{len $cluster.Datacenters}} datacenter(s)
      </div>
    </div>

    <!-- Topology diagram -->
    <div class="bg-slate-800/50 rounded-lg border border-slate-700/50 p-5 mb-4">
      <h3 class="text-sm font-medium text-slate-400 uppercase tracking-wide mb-4">Cluster Topology</h3>
      <div class="font-mono text-sm">
        <!-- Cluster root -->
        <div class="flex items-center gap-2 text-indigo-300 mb-1">
          <svg class="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/></svg>
          <span class="font-semibold">{{$cluster.Name}}</span>
        </div>
        {{$dcLen := len $cluster.Datacenters}}
        {{range $di, $dc := $cluster.Datacenters}}
        {{$dcLast := eq (addInts $di 1) $dcLen}}
        <div class="ml-3">
          <!-- DC connector -->
          <div class="flex items-center gap-0 text-emerald-400">
            <span class="text-slate-600 select-none">{{if $dcLast}}&#9492;&#9472;&#9472;{{else}}&#9500;&#9472;&#9472;{{end}} </span>
            <svg class="w-3.5 h-3.5 shrink-0 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17.657 16.657L13.414 20.9a1.998 1.998 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z"/></svg>
            <span class="font-semibold">DC: {{$dc.Name}}</span>
          </div>
          {{$rackLen := len $dc.Racks}}
          {{range $ri, $rack := $dc.Racks}}
          {{$rackLast := eq (addInts $ri 1) $rackLen}}
          <div class="{{if $dcLast}}ml-6{{else}}ml-6 border-l border-slate-700{{end}}">
            <div class="flex items-center gap-0 text-sky-400 pl-1">
              <span class="text-slate-600 select-none">{{if $rackLast}}&#9492;&#9472;&#9472;{{else}}&#9500;&#9472;&#9472;{{end}} </span>
              <span class="font-medium">Rack: {{$rack.Name}}</span>
            </div>
            {{$nodeLen := len $rack.Nodes}}
            {{range $ni, $node := $rack.Nodes}}
            {{$nodeLast := eq (addInts $ni 1) $nodeLen}}
            <div class="{{if $rackLast}}ml-7{{else}}ml-7 border-l border-slate-700{{end}}">
              <div class="flex items-center gap-1.5 text-slate-300 pl-1 py-0.5">
                <span class="text-slate-600 select-none">{{if $nodeLast}}&#9492;&#9472;{{else}}&#9500;&#9472;{{end}} </span>
                <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 shrink-0"></span>
                <span class="text-slate-200">{{$node.PodName}}</span>
                {{if $node.IP}}<span class="text-slate-500 text-xs">{{$node.IP}}</span>{{end}}
                {{if $node.HostID}}<span class="text-slate-600 text-xs">{{truncate $node.HostID 8}}..</span>{{end}}
              </div>
            </div>
            {{end}}
          </div>
          {{end}}
        </div>
        {{end}}
      </div>
    </div>

    <!-- Cluster-level collectors -->
    {{if $cluster.Collectors}}
    <div class="mb-4">
      <h3 class="text-sm font-medium text-slate-400 uppercase tracking-wide mb-2">Cluster Collectors</h3>
      {{range $i, $c := $cluster.Collectors}}
      {{template "collector" dict "C" $c "Prefix" (printf "cl-%d" $ci) "Index" $i}}
      {{end}}
    </div>
    {{end}}

    <!-- Topology: DC > Rack > Node -->
    {{range $di, $dc := $cluster.Datacenters}}
    <div class="mb-4">
      <h3 class="text-lg font-medium text-emerald-400 mb-3 flex items-center gap-2">
        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17.657 16.657L13.414 20.9a1.998 1.998 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z"/>
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 11a3 3 0 11-6 0 3 3 0 016 0z"/>
        </svg>
        Datacenter: {{$dc.Name}}
      </h3>

      {{range $ri, $rack := $dc.Racks}}
      <div class="ml-4 mb-4">
        <h4 class="text-sm font-medium text-slate-400 mb-2">Rack: {{$rack.Name}}</h4>

        {{range $ni, $node := $rack.Nodes}}
        <div id="node-{{$ci}}-{{$di}}-{{$ri}}-{{$ni}}" class="ml-4 mb-3">
          <!-- Node header -->
          <div class="bg-slate-800 rounded-lg border border-slate-700 p-4">
            <div class="flex items-center justify-between mb-2">
              <button onclick="toggleCollapse('node-detail-{{$ci}}-{{$di}}-{{$ri}}-{{$ni}}')"
                class="flex items-center gap-2 text-white font-medium hover:text-indigo-300 transition-colors">
                <svg class="w-4 h-4 transition-transform" id="chevron-node-detail-{{$ci}}-{{$di}}-{{$ri}}-{{$ni}}">
                  <path fill="currentColor" d="M6 9l6 6 6-6"/>
                </svg>
                <span class="w-2 h-2 rounded-full bg-emerald-500"></span>
                {{$node.PodName}}
              </button>
            </div>
            <div class="flex flex-wrap gap-x-6 gap-y-1 text-xs text-slate-400 ml-6">
              <span>Namespace: <span class="text-slate-300">{{$node.Namespace}}</span></span>
              {{if $node.IP}}<span>IP: <span class="text-slate-300 font-mono">{{$node.IP}}</span></span>{{end}}
              {{if $node.HostID}}<span>Host ID: <span class="text-slate-300 font-mono text-xs">{{truncate $node.HostID 12}}...</span></span>{{end}}
              <span>DC: <span class="text-slate-300">{{$node.DatacenterName}}</span></span>
              <span>Rack: <span class="text-slate-300">{{$node.RackName}}</span></span>
            </div>

            <!-- Node collectors (collapsed by default) -->
            <div class="collapse-content mt-3" id="node-detail-{{$ci}}-{{$di}}-{{$ri}}-{{$ni}}">
              {{if $node.Collectors}}
              {{range $nci, $c := $node.Collectors}}
              {{template "collector" dict "C" $c "Prefix" (printf "n-%d-%d-%d-%d" $ci $di $ri $ni) "Index" $nci}}
              {{end}}
              {{else}}
              <p class="text-sm text-slate-500 italic">No collector results for this node.</p>
              {{end}}
            </div>
          </div>
        </div>
        {{end}}
      </div>
      {{end}}
    </div>
    {{end}}
  </section>
  {{end}}

  <!-- 3. Cluster-Wide Section (last) -->
  {{if .ClusterWide}}
  <section id="cluster-wide" class="mb-8">
    <h2 class="text-xl font-semibold text-white mb-4 flex items-center gap-2">
      <svg class="w-5 h-5 text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064"/>
      </svg>
      Cluster-Wide Collectors
    </h2>
    {{range $i, $c := .ClusterWide}}
    {{template "collector" dict "C" $c "Prefix" "cw" "Index" $i}}
    {{end}}
  </section>
  {{end}}

  {{if and (not .Clusters) (not .ClusterWide) (not .Analysis)}}
  <div class="text-center py-20 text-slate-500">
    <svg class="w-16 h-16 mx-auto mb-4 text-slate-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1" d="M9.172 16.172a4 4 0 015.656 0M9 10h.01M15 10h.01M12 2a10 10 0 100 20 10 10 0 000-20z"/>
    </svg>
    <p class="text-lg">No diagnostic data found.</p>
    <p class="text-sm mt-2">Make sure this is a valid diagnose output directory containing vitals.json.</p>
  </div>
  {{end}}

</main>
</div>

<!-- Artifact viewer modal -->
<div id="artifactModal" class="fixed inset-0 bg-black/60 z-50 hidden flex items-center justify-center p-8" onclick="closeArtifactModal(event)">
  <div class="bg-slate-800 border border-slate-600 rounded-lg w-full max-w-5xl max-h-[85vh] flex flex-col" onclick="event.stopPropagation()">
    <div class="flex items-center justify-between px-4 py-3 border-b border-slate-700">
      <h3 id="artifactTitle" class="text-sm font-medium text-white truncate"></h3>
      <button onclick="closeArtifactModal()" class="text-slate-400 hover:text-white">
        <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
        </svg>
      </button>
    </div>
    <div class="flex-1 overflow-auto p-4">
      <pre id="artifactContent" class="artifact-content text-xs font-mono text-slate-300 whitespace-pre-wrap break-all"></pre>
      <div id="artifactLoading" class="text-center py-8 text-slate-500 hidden">Loading...</div>
      <div id="artifactError" class="text-center py-8 text-red-400 hidden"></div>
    </div>
  </div>
</div>

{{define "collector"}}
<div class="bg-slate-800/50 rounded border border-slate-700/50 mb-2">
  <button onclick="toggleCollapse('{{.Prefix}}-col-{{.Index}}')"
    class="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-slate-700/30 transition-colors">
    <svg class="w-3.5 h-3.5 transition-transform shrink-0" id="chevron-{{.Prefix}}-col-{{.Index}}">
      <path fill="currentColor" d="M6 9l6 6 6-6"/>
    </svg>
    {{template "statusDot" .C.Status}}
    <span class="text-slate-300 font-medium truncate">{{.C.Name}}</span>
    <span class="text-xs text-slate-500 ml-auto shrink-0">{{.C.Duration}}</span>
  </button>
  <div class="collapse-content" id="{{.Prefix}}-col-{{.Index}}">
    <div class="px-3 pb-3 pt-1 border-t border-slate-700/30">
      {{if .C.Message}}
      <p class="text-xs text-slate-400 mb-2">{{.C.Message}}</p>
      {{end}}
      {{if .C.Artifacts}}
      <div class="text-xs">
        <span class="text-slate-500 font-medium">Artifacts:</span>
        <ul class="mt-1 space-y-0.5">
          {{range .C.Artifacts}}
          <li>
            <button onclick="loadArtifact('{{.URL}}', '{{.DisplayPath}}')"
              class="text-indigo-400 hover:text-indigo-300 hover:underline truncate max-w-full text-left">
              {{.DisplayPath}}
            </button>
            {{if .Description}}<span class="text-slate-500 ml-1">— {{.Description}}</span>{{end}}
          </li>
          {{end}}
        </ul>
      </div>
      {{end}}
    </div>
  </div>
</div>
{{end}}

{{define "statusDot"}}
{{if eq . "passed"}}<span class="w-2 h-2 rounded-full bg-emerald-500 shrink-0"></span>
{{else if eq . "failed"}}<span class="w-2 h-2 rounded-full bg-red-500 shrink-0"></span>
{{else if eq . "skipped"}}<span class="w-2 h-2 rounded-full bg-slate-500 shrink-0"></span>
{{else if eq . "warning"}}<span class="w-2 h-2 rounded-full bg-amber-500 shrink-0"></span>
{{else}}<span class="w-2 h-2 rounded-full bg-slate-500 shrink-0"></span>
{{end}}
{{end}}

{{define "statusBadge"}}
{{if eq . "passed"}}<span class="bg-emerald-500/20 text-emerald-300 px-2 py-0.5 rounded text-xs font-medium">PASSED</span>
{{else if eq . "failed"}}<span class="bg-red-500/20 text-red-300 px-2 py-0.5 rounded text-xs font-medium">FAILED</span>
{{else if eq . "warning"}}<span class="bg-amber-500/20 text-amber-300 px-2 py-0.5 rounded text-xs font-medium">WARNING</span>
{{else if eq . "skipped"}}<span class="bg-slate-500/20 text-slate-400 px-2 py-0.5 rounded text-xs font-medium">SKIPPED</span>
{{else}}<span class="bg-slate-500/20 text-slate-400 px-2 py-0.5 rounded text-xs font-medium">{{.}}</span>
{{end}}
{{end}}

<script>
// Toggle collapse/expand for a section.
function toggleCollapse(id) {
  const el = document.getElementById(id);
  const chevron = document.getElementById('chevron-' + id);
  if (!el) return;
  el.classList.toggle('open');
  if (chevron) {
    chevron.style.transform = el.classList.contains('open') ? 'rotate(180deg)' : '';
  }
}

// Toggle sidebar navigation sections.
function toggleNav(id) {
  const el = document.getElementById('nav-' + id);
  const chevron = document.getElementById('chevron-' + id);
  if (!el) return;
  el.classList.toggle('open');
  if (chevron) {
    chevron.style.transform = el.classList.contains('open') ? 'rotate(180deg)' : '';
  }
}

// Load and display an artifact in the modal.
function loadArtifact(url, title) {
  const modal = document.getElementById('artifactModal');
  const titleEl = document.getElementById('artifactTitle');
  const contentEl = document.getElementById('artifactContent');
  const loadingEl = document.getElementById('artifactLoading');
  const errorEl = document.getElementById('artifactError');

  modal.classList.remove('hidden');
  modal.classList.add('flex');
  titleEl.textContent = title;
  contentEl.textContent = '';
  loadingEl.classList.remove('hidden');
  errorEl.classList.add('hidden');

  fetch(url)
    .then(resp => {
      if (!resp.ok) throw new Error('HTTP ' + resp.status + ': ' + resp.statusText);
      return resp.text();
    })
    .then(text => {
      loadingEl.classList.add('hidden');
      contentEl.textContent = text;
    })
    .catch(err => {
      loadingEl.classList.add('hidden');
      errorEl.textContent = 'Failed to load artifact: ' + err.message;
      errorEl.classList.remove('hidden');
    });
}

// Close the artifact modal.
function closeArtifactModal(event) {
  if (event && event.target !== event.currentTarget) return;
  const modal = document.getElementById('artifactModal');
  modal.classList.add('hidden');
  modal.classList.remove('flex');
}

// Close modal on Escape key.
document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') closeArtifactModal();
});

// Filter the navigation tree.
function filterTree(query) {
  const q = query.toLowerCase().trim();
  const navItems = document.querySelectorAll('#navTree [data-search]');
  navItems.forEach(el => {
    const searchText = el.getAttribute('data-search').toLowerCase();
    const text = el.textContent.toLowerCase();
    const match = !q || searchText.includes(q) || text.includes(q);
    el.style.display = match ? '' : 'none';
  });
}
</script>

</body>
</html>`

// htmlFuncMap provides helper functions for the HTML template.
var htmlFuncMap = template.FuncMap{
	"dict": func(values ...any) map[string]any {
		if len(values)%2 != 0 {
			panic("dict requires an even number of arguments")
		}
		m := make(map[string]any, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				panic("dict keys must be strings")
			}
			m[key] = values[i+1]
		}
		return m
	},
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n]
	},
	"addInts": func(args ...int) int {
		sum := 0
		for _, a := range args {
			sum += a
		}
		return sum
	},
}

// RenderHTML executes the HTML template with the given report data and returns
// the complete HTML page as bytes.
func RenderHTML(data *HTMLReportData) ([]byte, error) {
	tmpl, err := template.New("report").Funcs(htmlFuncMap).Parse(htmlTemplateSrc)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing HTML template: %w", err)
	}

	return buf.Bytes(), nil
}
