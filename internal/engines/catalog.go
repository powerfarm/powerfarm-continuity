package engines

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Engine struct {
	Name     string `json:"name"`
	Profile  string `json:"profile"`
	Kind     string `json:"kind"`
	Version  string `json:"version"`
	Resolved string `json:"resolved"`
	SHA256   string `json:"sha256"`
}

type Status struct {
	Engine
	SourcePresent   bool   `json:"sourcePresent"`
	RuntimePresent  bool   `json:"runtimePresent"`
	RuntimeArtifact string `json:"runtimeArtifact,omitempty"`
	VersionOutput   string `json:"versionOutput,omitempty"`
}

func LoadFetched(path string) ([]Engine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Engine
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			return nil, fmt.Errorf("invalid fetched.tsv line: expected 6 fields, got %d", len(fields))
		}
		out = append(out, Engine{
			Name: fields[0], Profile: fields[1], Kind: fields[2], Version: fields[3], Resolved: fields[4], SHA256: fields[5],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func Doctor(v2Root string) ([]Status, error) {
	fetched := filepath.Join(v2Root, "engines", "downloaded", "meta", "fetched.tsv")
	list, err := LoadFetched(fetched)
	if err != nil {
		return nil, err
	}
	statuses := make([]Status, 0, len(list))
	for _, e := range list {
		s := Status{Engine: e}
		s.SourcePresent = sourcePresent(v2Root, e)
		if name, ok := runtimeBinaryName(e.Name); ok {
			candidate := filepath.Join(v2Root, "engines", "runtime", "bin", name)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				s.RuntimePresent = true
				s.RuntimeArtifact = candidate
				s.VersionOutput = binaryVersion(candidate, e.Name)
			}
		} else if artifact, ok := runtimeLibraryArtifact(v2Root, e.Name); ok {
			if st, err := os.Stat(artifact); err == nil && !st.IsDir() {
				s.RuntimePresent = true
				s.RuntimeArtifact = artifact
			}
		}
		statuses = append(statuses, s)
	}
	return statuses, nil
}

func sourcePresent(v2Root string, e Engine) bool {
	base := filepath.Join(v2Root, "engines", "downloaded")
	var p string
	switch e.Kind {
	case "github-tag":
		p = filepath.Join(base, "src", e.Name)
	case "npm":
		normalized := strings.ReplaceAll(e.Name, "node-wot-", "node-wot-")
		matches, _ := filepath.Glob(filepath.Join(base, "npm", normalized+"-*.tgz"))
		if len(matches) == 0 {
			// The archive names are node-wot-binding-http, node-wot-binding-mqtt, node-wot-core.
			switch e.Name {
			case "node-wot-http":
				matches, _ = filepath.Glob(filepath.Join(base, "npm", "node-wot-binding-http-*.tgz"))
			case "node-wot-mqtt":
				matches, _ = filepath.Glob(filepath.Join(base, "npm", "node-wot-binding-mqtt-*.tgz"))
			case "node-wot-core":
				matches, _ = filepath.Glob(filepath.Join(base, "npm", "node-wot-core-*.tgz"))
			}
		}
		return len(matches) > 0
	case "url":
		matches, _ := filepath.Glob(filepath.Join(base, "specs", "*"+e.Version+"*"))
		return len(matches) > 0
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func runtimeBinaryName(engine string) (string, bool) {
	switch engine {
	case "nats-server":
		return "nats-server", true
	case "opa":
		return "opa", true
	case "cue":
		return "cue", true
	case "temporal":
		// The Temporal server source is pinned separately. Local development is run by the Temporal CLI.
		return "temporal", true
	case "oras":
		return "oras", true
	case "cosign":
		return "cosign", true
	case "wasmtime":
		return "wasmtime", true
	}
	return "", false
}

func runtimeLibraryArtifact(v2Root, engine string) (string, bool) {
	prefix := filepath.Join(v2Root, "engines", "runtime", "prefix")
	switch engine {
	case "open62541":
		return filepath.Join(prefix, "lib", "libopen62541.a"), true
	case "paho-mqtt-c":
		matches, _ := filepath.Glob(filepath.Join(prefix, "lib", "libpaho-mqtt3c.so*"))
		if len(matches) > 0 {
			return matches[0], true
		}
		return filepath.Join(prefix, "lib", "libpaho-mqtt3c.so"), true
	case "libmodbus":
		matches, _ := filepath.Glob(filepath.Join(prefix, "lib", "libmodbus.so*"))
		if len(matches) > 0 {
			return matches[0], true
		}
		return filepath.Join(prefix, "lib", "libmodbus.so"), true
	}
	return "", false
}

func binaryVersion(path, engine string) string {
	var args []string
	switch engine {
	case "nats-server", "opa", "cue", "oras", "cosign", "wasmtime":
		args = []string{"--version"}
	case "temporal":
		args = []string{"--version"}
	default:
		return ""
	}
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return err.Error()
	}
	return strings.TrimSpace(string(out))
}

func MarshalPretty(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
