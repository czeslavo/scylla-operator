#!/usr/bin/env python3
"""Generate knowledge-graph.json from docs-staging markdown files.

Scans docs-staging/source/ for .md files, extracts titles (or graph-label
from YAML frontmatter) and cross-reference links, and outputs a JSON file
suitable for the D3.js graph renderer.

Usage:
    python hack/generate-knowledge-graph.py

No external dependencies required (stdlib only).
"""

import json
import re
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
DOCS_SOURCE = REPO_ROOT / "docs-staging" / "source"
OUTPUT_PATH = DOCS_SOURCE / "_static" / "knowledge-graph.json"

# Directories to skip (not conceptual content)
EXCLUDED_PREFIXES = ("_snippets/", "_static/", "_ext/", ".internal/", "map.md")

# Color palette for sections (assigned in order of discovery, deterministic via sorting)
COLOR_PALETTE = [
    "#4a90d9",
    "#27ae60",
    "#e67e22",
    "#8e44ad",
    "#16a085",
    "#c0392b",
    "#7f8c8d",
    "#f1c40f",
    "#2980b9",
    "#95a5a6",
    "#d35400",
    "#2c3e50",
    "#1abc9c",
    "#e74c3c",
    "#3498db",
]


def is_excluded(rel_path: str) -> bool:
    return any(rel_path.startswith(p) for p in EXCLUDED_PREFIXES)


def parse_frontmatter(content: str) -> dict:
    """Parse YAML frontmatter (simple key: value pairs)."""
    if not content.startswith("---"):
        return {}
    end = content.find("\n---", 3)
    if end == -1:
        return {}
    fm_text = content[4:end]
    result = {}
    for line in fm_text.splitlines():
        m = re.match(r"^([a-zA-Z_-]+)\s*:\s*(.+)$", line)
        if m:
            result[m.group(1).strip()] = m.group(2).strip()
    return result


def extract_label(filepath: Path, content: str) -> str:
    """Extract graph label: frontmatter graph-label > directory name (for index) > H1 title."""
    fm = parse_frontmatter(content)
    if "graph-label" in fm:
        return fm["graph-label"]
    if filepath.name == "index.md":
        # Use parent directory name, humanized
        return filepath.parent.name.replace("-", " ").title()
    for line in content.splitlines():
        m = re.match(r"^#\s+(.+)$", line)
        if m:
            return m.group(1).strip()
    return filepath.stem.replace("-", " ").title()


def extract_links(content: str) -> list[str]:
    """Extract relative markdown links to other doc pages."""
    links = []
    for m in re.finditer(r"\[([^\]]*)\]\(([^)]+)\)", content):
        target = m.group(2)
        if target.startswith(("http://", "https://", "#", "mailto:")):
            continue
        target = target.split("#")[0]
        if not target:
            continue
        links.append(target)
    return links


def resolve_link(source_dir: Path, target: str) -> str | None:
    """Resolve a relative link to a canonical path relative to DOCS_SOURCE."""
    resolved = (source_dir / target).resolve()
    try:
        return str(resolved.relative_to(DOCS_SOURCE))
    except ValueError:
        return None


def get_section(rel_path: str) -> str:
    parts = rel_path.split("/")
    # Root-level files (e.g. "index.md") belong to a "root" section
    if len(parts) == 1:
        return "root"
    return parts[0]


def main():
    all_files = sorted(DOCS_SOURCE.rglob("*.md"))

    # Build page data: rel_path -> {label, section, links, type}
    pages = {}
    for filepath in all_files:
        rel_path = str(filepath.relative_to(DOCS_SOURCE))
        if is_excluded(rel_path):
            continue
        # Skip root-level files (no section)
        if "/" not in rel_path:
            continue

        content = filepath.read_text(encoding="utf-8")
        label = extract_label(filepath, content)
        raw_links = extract_links(content)
        resolved_links = []
        for link in raw_links:
            resolved = resolve_link(filepath.parent, link)
            if resolved:
                resolved_links.append(resolved)

        pages[rel_path] = {
            "label": label,
            "section": get_section(rel_path),
            "links": resolved_links,
            "type": "index" if filepath.name == "index.md" else "page",
        }

    # Build nodes (1:1 with pages)
    page_to_node = {}
    nodes = {}
    for rel_path, data in pages.items():
        node_id = rel_path.removesuffix(".md").replace("/", "--")
        page_to_node[rel_path] = node_id
        nodes[node_id] = {
            "id": node_id,
            "label": data["label"],
            "section": data["section"],
            "page": rel_path,
            "type": data["type"],
        }

    # Build edges from links
    edges_set = set()
    for rel_path, data in pages.items():
        source_node = page_to_node[rel_path]
        for link_target in data["links"]:
            target_node = page_to_node.get(link_target)
            if not target_node:
                for variant in [
                    link_target + ".md",
                    link_target + "/index.md",
                    link_target.rstrip("/") + ".md",
                ]:
                    target_node = page_to_node.get(variant)
                    if target_node:
                        break
            if target_node and target_node != source_node:
                edge = tuple(sorted([source_node, target_node]))
                edges_set.add(edge)

    # Add implicit edges: index pages link to all sibling/child pages
    for rel_path, data in pages.items():
        if data["type"] != "index":
            continue
        index_dir = str(Path(rel_path).parent)
        index_node = page_to_node[rel_path]
        for other_path, other_data in pages.items():
            if other_path == rel_path:
                continue
            other_dir = str(Path(other_path).parent)
            if other_dir == index_dir or other_dir.startswith(index_dir + "/"):
                # Only link to immediate children (same dir or direct subdirs)
                depth = other_dir[len(index_dir) :].count("/")
                if depth <= 1:
                    other_node = page_to_node[other_path]
                    edge = tuple(sorted([index_node, other_node]))
                    edges_set.add(edge)

    # Assign colors to sections deterministically (sorted by name)
    all_sections = sorted(set(n["section"] for n in nodes.values()))
    section_colors = {
        section: COLOR_PALETTE[i % len(COLOR_PALETTE)]
        for i, section in enumerate(all_sections)
    }

    # Assign colors and connection counts
    edge_list = [{"source": e[0], "target": e[1]} for e in sorted(edges_set)]

    connection_count = {}
    for edge in edge_list:
        connection_count[edge["source"]] = connection_count.get(edge["source"], 0) + 1
        connection_count[edge["target"]] = connection_count.get(edge["target"], 0) + 1

    node_list = []
    for node in nodes.values():
        color = section_colors.get(node["section"], "#bdc3c7")
        node_list.append(
            {
                "id": node["id"],
                "label": node["label"],
                "section": node["section"],
                "color": color,
                "page": node["page"],
                "type": node["type"],
                "connections": connection_count.get(node["id"], 0),
            }
        )

    output = {
        "nodes": node_list,
        "edges": edge_list,
        "sections": section_colors,
    }

    OUTPUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT_PATH.write_text(json.dumps(output, indent=2) + "\n")
    print(f"Generated {OUTPUT_PATH} ({len(node_list)} nodes, {len(edge_list)} edges)")


if __name__ == "__main__":
    main()
