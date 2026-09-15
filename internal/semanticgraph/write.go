package semanticgraph

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func Write(outDir string, bundle Bundle) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "graph.json"), append(pretty, '\n'), 0o644); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "nodes.jsonl"), bundle.Nodes); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "edges.jsonl"), bundle.Edges); err != nil {
		return err
	}
	manifest := map[string]any{"schemaVersion": bundle.SchemaVersion, "module": bundle.Module, "nodeCount": len(bundle.Nodes), "edgeCount": len(bundle.Edges), "files": []string{"graph.json", "nodes.jsonl", "edges.jsonl", "domain.json", "runtime.json", "code.json", "domain.dot", "README.md"}}
	m, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), append(m, '\n'), 0o644); err != nil {
		return err
	}
	if err := writeView(filepath.Join(outDir, "domain.json"), bundle, map[string]bool{"domain": true, "compiler": true, "effect-semantics": true, "policy": true, "runtime": true, "substrate": true}); err != nil {
		return err
	}
	if err := writeView(filepath.Join(outDir, "runtime.json"), bundle, map[string]bool{"runtime": true, "effect-semantics": true, "persistence": true, "policy": true, "transport": true}); err != nil {
		return err
	}
	if err := writeView(filepath.Join(outDir, "code.json"), bundle, map[string]bool{"code": true, "domain": true, "compiler": true, "runtime": true, "persistence": true, "policy": true, "transport": true, "interface": true}); err != nil {
		return err
	}
	if err := writeDOT(filepath.Join(outDir, "domain.dot"), bundle); err != nil {
		return err
	}
	return writeReadme(filepath.Join(outDir, "README.md"), bundle)
}

func writeJSONL[T any](path string, xs []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, x := range xs {
		if err := enc.Encode(x); err != nil {
			return err
		}
	}
	return nil
}

func writeView(path string, b Bundle, layers map[string]bool) error {
	nodes := []Node{}
	included := map[string]bool{}
	for _, n := range b.Nodes {
		if layers[n.Layer] {
			nodes = append(nodes, n)
			included[n.ID] = true
		}
	}
	edges := []Edge{}
	for _, e := range b.Edges {
		if included[e.From] && included[e.To] {
			edges = append(edges, e)
		}
	}
	v := Bundle{SchemaVersion: b.SchemaVersion, Module: b.Module, Root: b.Root, Nodes: nodes, Edges: edges}
	raw, _ := json.MarshalIndent(v, "", "  ")
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func writeDOT(path string, b Bundle) error {
	kinds := map[string]bool{"workflow": true, "workflow_task": true, "capability": true, "execution_bundle": true, "bundle_step": true, "effect_class": true, "effect_state": true, "recovery_strategy": true, "contract": true, "authorization_mode": true}
	included := map[string]Node{}
	for _, n := range b.Nodes {
		if kinds[n.Kind] {
			included[n.ID] = n
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(f, "digraph continuity {")
	fmt.Fprintln(f, "  rankdir=LR;")
	fmt.Fprintln(f, "  graph [fontname=\"Helvetica\"]; node [shape=box,fontname=\"Helvetica\"]; edge [fontname=\"Helvetica\"];")
	ids := make([]string, 0, len(included))
	for id := range included {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	alias := map[string]string{}
	for i, id := range ids {
		alias[id] = fmt.Sprintf("n%d", i)
		n := included[id]
		fmt.Fprintf(f, "  %s [label=%q];\n", alias[id], n.Kind+"\n"+n.Name)
	}
	for _, e := range b.Edges {
		a, ok1 := alias[e.From]
		c, ok2 := alias[e.To]
		if ok1 && ok2 {
			label := e.Kind
			if e.Kind == "transitions_to" && e.Attributes != nil {
				if event, ok := e.Attributes["event"].(string); ok {
					label += ": " + event
				}
			}
			fmt.Fprintf(f, "  %s -> %s [label=%q];\n", a, c, label)
		}
	}
	fmt.Fprintln(f, "}")
	return nil
}

func writeReadme(path string, b Bundle) error {
	counts := map[string]int{}
	for _, n := range b.Nodes {
		counts[n.Kind]++
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# Continuity semantic graph\n\nGenerated deterministically from source code, example workflows/capabilities, the effect state machine, and engine locks.\n\n- Schema: `%s`\n- Nodes: **%d**\n- Edges: **%d**\n\n## Files\n\n- `graph.json` complete graph\n- `nodes.jsonl` / `edges.jsonl` streaming-friendly graph\n- `domain.json` domain/runtime projection for LLM reasoning\n- `runtime.json` effect/runtime projection\n- `code.json` static code graph\n- `domain.dot` Graphviz source\n\n## Node kinds\n\n", b.SchemaVersion, len(b.Nodes), len(b.Edges))
	for _, k := range kinds {
		fmt.Fprintf(f, "- `%s`: %d\n", k, counts[k])
	}
	fmt.Fprint(f, "\n## LLM retrieval rule\n\nStart from semantic nodes (`workflow`, `capability`, `bundle_step`, `effect_class`, `effect_state`) and expand 1-2 hops before retrieving their `source` spans. Use code nodes only when the semantic graph does not answer the question.\n\nWhen generated through `continuity graph`, the sibling `acts/` directory contains the `logline.semantic-graph.v0` receipted projection. Use `continuity graph-context` to retrieve a semantic neighborhood together with the receipts and provenance that justify it.\n")
	return nil
}
