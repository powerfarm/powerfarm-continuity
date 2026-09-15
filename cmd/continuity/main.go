package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"powerfarm.dev/continuity/v2/internal/actgraph"
	"powerfarm.dev/continuity/v2/internal/compiler"
	"powerfarm.dev/continuity/v2/internal/engines"
	cruntime "powerfarm.dev/continuity/v2/internal/runtime"
	"powerfarm.dev/continuity/v2/internal/semanticchange"
	"powerfarm.dev/continuity/v2/internal/semanticgraph"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "compile":
		compileCmd(os.Args[2:])
	case "doctor":
		doctorCmd(os.Args[2:])
	case "smoke":
		smokeCmd(os.Args[2:])
	case "graph":
		graphCmd(os.Args[2:])
	case "graph-query":
		graphQueryCmd(os.Args[2:])
	case "graph-context":
		graphContextCmd(os.Args[2:])
	case "graph-verify":
		graphVerifyCmd(os.Args[2:])
	case "change-stage":
		changeStageCmd(os.Args[2:])
	case "change-verify":
		changeVerifyCmd(os.Args[2:])
	case "change-accept":
		changeAcceptCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		// Backward-compatible slice-0 invocation: continuity -workflow ...
		if len(os.Args) > 1 && os.Args[1][0] == '-' {
			compileCmd(os.Args[1:])
			return
		}
		fmt.Fprintf(os.Stderr, "continuity: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func compileCmd(args []string) {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	workflow := fs.String("workflow", "", "Open Workflow JSON file")
	capabilities := fs.String("capabilities", "", "directory of Continuity capability profiles")
	_ = fs.Parse(args)
	if *workflow == "" || *capabilities == "" {
		fmt.Fprintln(os.Stderr, "usage: continuity compile -workflow workflow.json -capabilities capabilities/")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*workflow)
	if err != nil {
		fatal(err)
	}
	bundle, err := compiler.Compile(raw, *capabilities)
	if err != nil {
		fatal(err)
	}
	out, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(out))
}

func doctorCmd(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	root := fs.String("root", ".", "Continuity v2 root")
	jsonOut := fs.Bool("json", false, "emit JSON")
	_ = fs.Parse(args)
	abs, err := filepath.Abs(*root)
	if err != nil {
		fatal(err)
	}
	statuses, err := engines.Doctor(abs)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		out, err := engines.MarshalPretty(statuses)
		if err != nil {
			fatal(err)
		}
		fmt.Println(string(out))
		return
	}
	fmt.Printf("Continuity engine doctor: %s\n", abs)
	fmt.Printf("%-24s %-10s %-10s %s\n", "ENGINE", "SOURCE", "RUNTIME", "VERSION")
	for _, s := range statuses {
		src := "missing"
		if s.SourcePresent {
			src = "present"
		}
		runtime := "-"
		if s.RuntimePresent {
			runtime = "present"
		} else if s.Name == "temporal" || s.Name == "opa" || s.Name == "nats-server" || s.Name == "cue" || s.Name == "open62541" || s.Name == "paho-mqtt-c" || s.Name == "libmodbus" {
			runtime = "missing"
		}
		fmt.Printf("%-24s %-10s %-10s %s\n", s.Name, src, runtime, s.VersionOutput)
	}
}

func smokeCmd(args []string) {
	fs := flag.NewFlagSet("smoke", flag.ExitOnError)
	opaURL := fs.String("opa", "http://127.0.0.1:8181", "OPA base URL")
	natsAddr := fs.String("nats", "127.0.0.1:4222", "NATS address")
	temporalAddr := fs.String("temporal", "127.0.0.1:7233", "Temporal frontend address")
	timeout := fs.Duration("timeout", 10*time.Second, "overall smoke timeout")
	_ = fs.Parse(args)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := cruntime.Smoke(ctx, *opaURL, *natsAddr, *temporalAddr)
	if err != nil {
		fatal(err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
}

func graphCmd(args []string) {
	fs := flag.NewFlagSet("graph", flag.ExitOnError)
	root := fs.String("root", ".", "Continuity repository root")
	out := fs.String("out", "./graph", "output directory")
	_ = fs.Parse(args)
	bundle, err := semanticgraph.Build(*root)
	if err != nil {
		fatal(err)
	}
	if err := semanticgraph.Write(*out, bundle); err != nil {
		fatal(err)
	}
	projection, err := actgraph.Project(bundle)
	if err != nil {
		fatal(err)
	}
	if err := actgraph.Write(filepath.Join(*out, "acts"), projection); err != nil {
		fatal(err)
	}
	fmt.Printf("semantic graph: %d nodes, %d edges, %d LogLine receipts -> %s\n", len(bundle.Nodes), len(bundle.Edges), len(projection.Receipts), *out)
}

func graphQueryCmd(args []string) {
	fs := flag.NewFlagSet("graph-query", flag.ExitOnError)
	graphPath := fs.String("graph", "./graph/graph.json", "semantic graph bundle")
	id := fs.String("id", "", "exact node id")
	query := fs.String("q", "", "substring search over node id/name/qualified/summary")
	depth := fs.Int("depth", 2, "incoming/outgoing graph hops (0-6)")
	limit := fs.Int("limit", 100, "maximum returned nodes")
	_ = fs.Parse(args)
	if *id == "" && *query == "" {
		fmt.Fprintln(os.Stderr, "usage: continuity graph-query -graph ./graph/graph.json (-id NODE_ID | -q TEXT) [-depth 2]")
		os.Exit(2)
	}
	bundle, err := semanticgraph.Load(*graphPath)
	if err != nil {
		fatal(err)
	}
	view := semanticgraph.Neighborhood(bundle, semanticgraph.Query{ID: *id, Text: *query, Depth: *depth, Limit: *limit})
	out, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(out))
}

func graphContextCmd(args []string) {
	fs := flag.NewFlagSet("graph-context", flag.ExitOnError)
	graphPath := fs.String("graph", "./graph/graph.json", "semantic graph bundle")
	actsDir := fs.String("acts", "./graph/acts", "receipted semantic projection")
	id := fs.String("id", "", "exact node id")
	query := fs.String("q", "", "substring search over semantic nodes")
	depth := fs.Int("depth", 2, "incoming/outgoing graph hops (0-6)")
	limit := fs.Int("limit", 100, "maximum returned nodes")
	_ = fs.Parse(args)
	if *id == "" && *query == "" {
		fmt.Fprintln(os.Stderr, "usage: continuity graph-context (-id NODE_ID | -q TEXT) [-depth 2]")
		os.Exit(2)
	}
	graph, err := semanticgraph.Load(*graphPath)
	if err != nil {
		fatal(err)
	}
	projection, err := actgraph.Load(*actsDir)
	if err != nil {
		fatal(err)
	}
	if err := actgraph.CheckGraphDigest(graph, projection); err != nil {
		fatal(err)
	}
	view := semanticgraph.Neighborhood(graph, semanticgraph.Query{ID: *id, Text: *query, Depth: *depth, Limit: *limit})
	ctx := actgraph.BuildContext(graph, view, projection)
	out, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(out))
}

func graphVerifyCmd(args []string) {
	fs := flag.NewFlagSet("graph-verify", flag.ExitOnError)
	graphPath := fs.String("graph", "./graph/graph.json", "semantic graph bundle")
	actsDir := fs.String("acts", "./graph/acts", "receipted semantic projection")
	_ = fs.Parse(args)
	graph, err := semanticgraph.Load(*graphPath)
	if err != nil {
		fatal(err)
	}
	projection, err := actgraph.Load(*actsDir)
	if err != nil {
		fatal(err)
	}
	if err := actgraph.CheckGraphDigest(graph, projection); err != nil {
		fatal(err)
	}
	fmt.Printf("verified %d LogLine receipts, %d evidence records, %d causal links for graph %s\n", len(projection.Receipts), len(projection.Evidence), len(projection.Links), projection.GraphDigest)
}

func changeStageCmd(args []string) {
	fs := flag.NewFlagSet("change-stage", flag.ExitOnError)
	root := fs.String("root", ".", "Continuity repository root")
	proposalPath := fs.String("proposal", "", "logline.semantic-change.v0 proposal JSON")
	out := fs.String("out", "./var/candidates/latest", "candidate bundle directory")
	_ = fs.Parse(args)
	if *proposalPath == "" {
		fmt.Fprintln(os.Stderr, "usage: continuity change-stage -proposal proposal.json [-root .] [-out candidate-dir]")
		os.Exit(2)
	}
	p, err := semanticchange.LoadProposal(*proposalPath)
	if err != nil {
		fatal(err)
	}
	candidate, err := semanticchange.Stage(*root, *out, p)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("semantic candidate %s: graph %s -> %s; +%d/-%d nodes, +%d/-%d edges; eligible=%v -> %s\n",
		candidate.ProposalID, candidate.BaseGraphDigest, candidate.CandidateGraphDigest,
		len(candidate.Diff.NodesAdded), len(candidate.Diff.NodesRemoved), len(candidate.Diff.EdgesAdded), len(candidate.Diff.EdgesRemoved),
		candidate.EligibleForAcceptance, *out)
}

func changeVerifyCmd(args []string) {
	fs := flag.NewFlagSet("change-verify", flag.ExitOnError)
	candidateDir := fs.String("candidate", "", "candidate bundle directory")
	_ = fs.Parse(args)
	if *candidateDir == "" {
		fmt.Fprintln(os.Stderr, "usage: continuity change-verify -candidate candidate-dir")
		os.Exit(2)
	}
	if err := semanticchange.VerifyCandidate(*candidateDir); err != nil {
		fatal(err)
	}
	_, c, _, err := semanticchange.LoadCandidate(*candidateDir)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("verified semantic candidate %s for graph %s\n", c.ProposalID, c.CandidateGraphDigest)
}

func changeAcceptCmd(args []string) {
	fs := flag.NewFlagSet("change-accept", flag.ExitOnError)
	root := fs.String("root", ".", "Continuity repository root")
	candidateDir := fs.String("candidate", "", "reviewed candidate bundle directory")
	who := fs.String("who", "human:operator", "accountable accepting principal")
	confirm := fs.String("confirm", "", "explicit confirmation token; must equal proposal id")
	_ = fs.Parse(args)
	if *candidateDir == "" || *confirm == "" {
		fmt.Fprintln(os.Stderr, "usage: continuity change-accept -candidate candidate-dir -confirm PROPOSAL_ID [-who human:operator]")
		os.Exit(2)
	}
	if err := semanticchange.VerifyCandidate(*candidateDir); err != nil {
		fatal(err)
	}
	_, c, _, err := semanticchange.LoadCandidate(*candidateDir)
	if err != nil {
		fatal(err)
	}
	if *confirm != c.ProposalID {
		fatal(fmt.Errorf("confirmation token must equal proposal id %s", c.ProposalID))
	}
	a, err := semanticchange.Accept(*root, *candidateDir, *who, *confirm)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("accepted semantic change %s; live graph %s; receipt %s\n", a.ProposalID, a.GraphDigest, a.Receipt["id"])
}

func usage() {
	fmt.Fprintln(os.Stderr, `Continuity v2

Commands:
  compile   compile Open Workflow + capability profiles into an ExecutionBundle
  doctor    verify pinned OSS engine sources and local runtime binaries
  smoke     exercise live OPA + NATS + Temporal endpoints
  graph     compile repository semantics into an LLM-friendly graph
  graph-query retrieve a bounded semantic neighborhood for reasoning
  graph-context retrieve graph neighborhood + LogLine receipts + provenance
  graph-verify verify the semantic graph and its receipted projection
  change-stage stage a receipted source proposal in an isolated candidate workspace
  change-verify verify a staged semantic-change candidate
  change-accept explicitly accept a reviewed candidate into the live repository

Examples:
  continuity compile -workflow examples/rescue.workflow.json -capabilities examples/capabilities
  continuity doctor -root .
  continuity smoke
  continuity graph -root . -out ./graph
  continuity graph-query -q service.restart -depth 2
  continuity graph-context -q service.restart -depth 2
  continuity graph-verify
  continuity change-stage -proposal proposal.json -out ./var/candidates/p1
  continuity change-verify -candidate ./var/candidates/p1
  continuity change-accept -candidate ./var/candidates/p1 -confirm PROPOSAL_ID`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "continuity:", err)
	os.Exit(1)
}
