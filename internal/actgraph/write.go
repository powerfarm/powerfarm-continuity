package actgraph

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"powerfarm.dev/continuity/v2/internal/logline"
)

func Write(outDir string, p Projection) error {
	if err := VerifyProjection(p); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writeReceipts(filepath.Join(outDir, "receipts.jsonl"), p.Receipts); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "evidence.jsonl"), p.Evidence); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "links.jsonl"), p.Links); err != nil {
		return err
	}
	index := map[string]any{"profile": p.Profile, "graph_digest": p.GraphDigest, "root_receipt_id": p.RootReceiptID, "entries": p.Index}
	if err := writePretty(filepath.Join(outDir, "index.json"), index); err != nil {
		return err
	}
	manifest := map[string]any{
		"profile": p.Profile, "graph_digest": p.GraphDigest, "root_receipt_id": p.RootReceiptID,
		"receipt_count": len(p.Receipts), "evidence_count": len(p.Evidence), "link_count": len(p.Links),
		"files": []string{"receipts.jsonl", "evidence.jsonl", "links.jsonl", "index.json", "README.md"},
	}
	if err := writePretty(filepath.Join(outDir, "manifest.json"), manifest); err != nil {
		return err
	}
	return writeReadme(filepath.Join(outDir, "README.md"), p)
}

func Load(outDir string) (Projection, error) {
	var p Projection
	raw, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		return p, err
	}
	var manifest struct {
		Profile       string `json:"profile"`
		GraphDigest   string `json:"graph_digest"`
		RootReceiptID string `json:"root_receipt_id"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return p, err
	}
	p.Profile, p.GraphDigest, p.RootReceiptID = manifest.Profile, manifest.GraphDigest, manifest.RootReceiptID
	if p.Receipts, err = readReceipts(filepath.Join(outDir, "receipts.jsonl")); err != nil {
		return p, err
	}
	if err = readJSONL(filepath.Join(outDir, "evidence.jsonl"), &p.Evidence); err != nil {
		return p, err
	}
	if err = readJSONL(filepath.Join(outDir, "links.jsonl"), &p.Links); err != nil {
		return p, err
	}
	raw, err = os.ReadFile(filepath.Join(outDir, "index.json"))
	if err != nil {
		return p, err
	}
	var index struct {
		Entries []IndexEntry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		return p, err
	}
	p.Index = index.Entries
	return p, VerifyProjection(p)
}

func writeReceipts(path string, xs []logline.Receipt) error {
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

func readReceipts(path string) ([]logline.Receipt, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	out := []logline.Receipt{}
	for sc.Scan() {
		r, err := logline.Decode(sc.Bytes())
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, sc.Err()
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

func readJSONL[T any](path string, out *[]T) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var x T
		if err := json.Unmarshal(sc.Bytes(), &x); err != nil {
			return err
		}
		*out = append(*out, x)
	}
	return sc.Err()
}

func writePretty(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func writeReadme(path string, p Projection) error {
	counts := map[string]int{}
	for _, entry := range p.Index {
		counts[entry.SemanticKind]++
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
	fmt.Fprintf(f, "# Continuity LogLine semantic graph\n\nThis directory is the receipted projection of the semantic graph. Every semantic node and edge is represented by a conformant `logline.receipt.v0` act under profile `%s`. Provenance is stored separately in content-addressed evidence records.\n\n- Graph digest: `%s`\n- Root receipt: `%s`\n- Receipts: **%d**\n- Evidence records: **%d**\n- Causality links: **%d**\n\n", p.Profile, p.GraphDigest, p.RootReceiptID, len(p.Receipts), len(p.Evidence), len(p.Links))
	fmt.Fprint(f, "## Files\n\n- `receipts.jsonl` LogLine acts\n- `evidence.jsonl` content-addressed provenance records referenced by `confirmed_by`\n- `links.jsonl` causality and assertion links between receipts\n- `index.json` semantic-id → receipt/evidence index\n- `manifest.json` projection identity and counts\n\n")
	fmt.Fprint(f, "## Invariant\n\nThe nine LogLine slots remain unchanged. Graph-specific facts live in AUX and are included in `content_hash` but not `tuple_hash`. No execution result, embedded evidence, or transport metadata is placed inside a receipt.\n")
	return nil
}
