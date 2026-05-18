/**
 * Knowledge Graph - Interactive D3.js force-directed graph renderer.
 *
 * Reads knowledge-graph.json and renders an interactive, zoomable,
 * clickable documentation map.
 */
(function () {
  "use strict";

  const container = document.getElementById("knowledge-graph");
  if (!container) return;

  const width = container.clientWidth || 1200;
  const height = 700;

  const svg = d3
    .select(container)
    .append("svg")
    .attr("width", "100%")
    .attr("height", height)
    .attr("viewBox", [0, 0, width, height]);

  // Zoom layer
  const g = svg.append("g");
  svg.call(
    d3.zoom().scaleExtent([0.3, 4]).on("zoom", (event) => {
      g.attr("transform", event.transform);
    })
  );

  // Load data
  const dataUrl = container.dataset.src || "../_static/knowledge-graph.json";
  d3.json(dataUrl).then((data) => {
    if (!data || !data.nodes || !data.edges) {
      container.innerHTML = "<p>Error loading knowledge graph data.</p>";
      return;
    }

    const nodes = data.nodes;
    const links = data.edges.map((e) => ({ source: e.source, target: e.target }));
    const sections = data.sections || {};

    // Scale node radius by connections
    const maxConn = Math.max(...nodes.map((n) => n.connections || 0), 1);
    const radiusScale = d3.scaleSqrt().domain([0, maxConn]).range([6, 22]);

    // Simulation
    const simulation = d3
      .forceSimulation(nodes)
      .force(
        "link",
        d3.forceLink(links).id((d) => d.id).distance(90)
      )
      .force("charge", d3.forceManyBody().strength(-200))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collision", d3.forceCollide().radius((d) => radiusScale(d.connections || 0) + 4));

    // Links
    const link = g
      .append("g")
      .attr("class", "links")
      .selectAll("line")
      .data(links)
      .join("line")
      .attr("stroke", "#999")
      .attr("stroke-opacity", 0.4)
      .attr("stroke-width", 1);

    // Nodes
    const node = g
      .append("g")
      .attr("class", "nodes")
      .selectAll("g")
      .data(nodes)
      .join("g")
      .attr("cursor", "pointer")
      .call(drag(simulation));

    node
      .append("circle")
      .attr("r", (d) => radiusScale(d.connections || 0) + (d.type === "index" ? 3 : 0))
      .attr("fill", (d) => d.color || "#bdc3c7")
      .attr("stroke", (d) => d.type === "index" ? "#333" : "#fff")
      .attr("stroke-width", (d) => d.type === "index" ? 2 : 1.5)
      .attr("stroke-dasharray", (d) => d.type === "index" ? "4 2" : "none");

    node
      .append("text")
      .text((d) => d.label)
      .attr("x", (d) => radiusScale(d.connections || 0) + 4)
      .attr("y", 4)
      .attr("font-size", "11px")
      .attr("font-family", "system-ui, sans-serif")
      .attr("fill", "#333");

    // Tooltip / click to navigate
    node.on("click", (event, d) => {
      if (d.page) {
        // Navigate relative to docs root (dirhtml builder uses /page-name/ URLs)
        const basePath = window.location.pathname.replace(/\/map\/?$/, "/");
        const pagePath = d.page.replace(/\.md$/, "").replace(/\/index$/, "");
        window.location.href = basePath + pagePath + "/";
      }
    });

    // Highlight on hover
    node
      .on("mouseenter", function (event, d) {
        // Highlight connected edges
        link
          .attr("stroke-opacity", (l) =>
            l.source.id === d.id || l.target.id === d.id ? 1 : 0.1
          )
          .attr("stroke-width", (l) =>
            l.source.id === d.id || l.target.id === d.id ? 2 : 1
          );
        // Dim unconnected nodes
        const connected = new Set();
        links.forEach((l) => {
          if (l.source.id === d.id) connected.add(l.target.id);
          if (l.target.id === d.id) connected.add(l.source.id);
        });
        connected.add(d.id);
        node.attr("opacity", (n) => (connected.has(n.id) ? 1 : 0.2));
      })
      .on("mouseleave", function () {
        link.attr("stroke-opacity", 0.4).attr("stroke-width", 1);
        node.attr("opacity", 1);
      });

    // Tick
    simulation.on("tick", () => {
      link
        .attr("x1", (d) => d.source.x)
        .attr("y1", (d) => d.source.y)
        .attr("x2", (d) => d.target.x)
        .attr("y2", (d) => d.target.y);
      node.attr("transform", (d) => `translate(${d.x},${d.y})`);
    });

    // Legend
    buildLegend(container, sections);
  });

  function drag(simulation) {
    return d3
      .drag()
      .on("start", (event, d) => {
        if (!event.active) simulation.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
      })
      .on("drag", (event, d) => {
        d.fx = event.x;
        d.fy = event.y;
      })
      .on("end", (event, d) => {
        if (!event.active) simulation.alphaTarget(0);
        d.fx = null;
        d.fy = null;
      });
  }

  function buildLegend(container, sections) {
    const legend = document.createElement("div");
    legend.style.cssText =
      "display:flex;flex-wrap:wrap;gap:12px;padding:12px 0;font-size:13px;font-family:system-ui,sans-serif;";
    for (const [name, color] of Object.entries(sections)) {
      const item = document.createElement("span");
      item.style.cssText = "display:inline-flex;align-items:center;gap:4px;";
      item.innerHTML = `<span style="width:12px;height:12px;border-radius:50%;background:${color};display:inline-block;"></span>${name.replace(/-/g, " ")}`;
      legend.appendChild(item);
    }
    container.appendChild(legend);
  }
})();
