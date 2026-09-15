package engines

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFetched(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fetched.tsv")
	data := "# h\nopa\tcore\tgithub-tag\tv1\topen-policy-agent/opa@v1\tabc\n"
	if err := os.WriteFile(p, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFetched(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "opa" || got[0].Version != "v1" {
		t.Fatalf("unexpected: %#v", got)
	}
}
