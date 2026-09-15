package actgraph

import (
	"os"
	"path/filepath"
	"testing"

	"powerfarm.dev/continuity/v2/internal/logline"
)

func TestSemanticGraphProfileVectors(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "spec", "logline-semantic-graph-v0", "vectors"))
	valid, _ := filepath.Glob(filepath.Join(root, "valid", "*.json"))
	invalid, _ := filepath.Glob(filepath.Join(root, "invalid", "*.json"))
	if len(valid) < 4 || len(invalid) < 3 {
		t.Fatalf("profile corpus too small: %d valid %d invalid", len(valid), len(invalid))
	}
	for _, path := range valid {
		t.Run("valid/"+filepath.Base(path), func(t *testing.T) {
			r := loadVector(t, path)
			if err := VerifyProfile(r); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, path := range invalid {
		t.Run("invalid/"+filepath.Base(path), func(t *testing.T) {
			r := loadVector(t, path)
			if err := logline.Verify(r); err != nil {
				t.Fatalf("invalid profile vector must remain a valid receipt-v0 object: %v", err)
			}
			if err := VerifyProfile(r); err == nil {
				t.Fatal("expected profile rejection")
			}
		})
	}
}
func loadVector(t *testing.T, path string) logline.Receipt {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := logline.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
