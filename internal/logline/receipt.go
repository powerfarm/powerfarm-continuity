package logline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ReceiptVersion       = "logline.receipt.v0"
	JSONCanonicalization = "jcs-rfc8785"
)

var slots = []string{"who", "did", "this", "when", "confirmed_by", "if_ok", "if_doubt", "if_not", "status"}
var forbidden = map[string]bool{"result": true, "evidence": true, "transport": true}

type Act struct {
	Who         string
	Did         string
	This        string
	When        string
	ConfirmedBy string
	IfOK        string
	IfDoubt     string
	IfNot       string
	Status      string
}

type Receipt map[string]any

func New(act Act, aux map[string]any) (Receipt, error) {
	r := Receipt{
		"receipt_version":       ReceiptVersion,
		"who":                   act.Who,
		"did":                   act.Did,
		"this":                  act.This,
		"when":                  act.When,
		"confirmed_by":          act.ConfirmedBy,
		"if_ok":                 act.IfOK,
		"if_doubt":              act.IfDoubt,
		"if_not":                act.IfNot,
		"status":                act.Status,
		"json_canonicalization": JSONCanonicalization,
	}
	for k, v := range aux {
		if _, exists := r[k]; exists || k == "id" || k == "hashes" || forbidden[k] {
			return nil, fmt.Errorf("aux field %q is reserved or forbidden", k)
		}
		r[k] = v
	}
	tuple, err := TupleHash(r)
	if err != nil {
		return nil, err
	}
	// hashes and id are excluded from content identity.
	content, err := ContentHash(r)
	if err != nil {
		return nil, err
	}
	r["hashes"] = map[string]any{"tuple_hash": tuple, "content_hash": content, "algorithm": "sha256"}
	r["id"] = content
	return r, Verify(r)
}

func Decode(raw []byte) (Receipt, error) {
	var r Receipt
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&r); err != nil {
		return nil, err
	}
	return r, nil
}

func TupleHash(r Receipt) (string, error) {
	tuple := map[string]any{}
	for _, k := range slots {
		v, ok := r[k]
		if !ok {
			return "", fmt.Errorf("missing slot %s", k)
		}
		tuple[k] = v
	}
	return hashCanonical(tuple)
}

func ContentHash(r Receipt) (string, error) {
	content := map[string]any{}
	for k, v := range r {
		if k == "id" || k == "hashes" {
			continue
		}
		content[k] = v
	}
	return hashCanonical(content)
}

func hashCanonical(v any) (string, error) {
	raw, err := Canonicalize(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}

func Verify(r Receipt) error {
	if r == nil {
		return fmt.Errorf("receipt is nil")
	}
	if r["receipt_version"] != ReceiptVersion {
		return fmt.Errorf("receipt_version must be %q", ReceiptVersion)
	}
	if r["json_canonicalization"] != JSONCanonicalization {
		return fmt.Errorf("json_canonicalization must be %q", JSONCanonicalization)
	}
	for _, k := range slots {
		if _, ok := r[k].(string); !ok {
			return fmt.Errorf("slot %s must be a string", k)
		}
	}
	for k := range forbidden {
		if _, ok := r[k]; ok {
			return fmt.Errorf("forbidden legacy field %s", k)
		}
	}
	id, ok := r["id"].(string)
	if !ok || len(id) != 64 || strings.ToLower(id) != id {
		return fmt.Errorf("id must be 64-char lowercase hex")
	}
	hashes, ok := r["hashes"].(map[string]any)
	if !ok {
		// Decoded JSON is also map[string]any, so this should be the common form.
		return fmt.Errorf("hashes must be an object")
	}
	if len(hashes) != 3 {
		return fmt.Errorf("hashes must contain exactly tuple_hash, content_hash, algorithm")
	}
	if hashes["algorithm"] != "sha256" {
		return fmt.Errorf("hash algorithm must be sha256")
	}
	wantTuple, err := TupleHash(r)
	if err != nil {
		return err
	}
	wantContent, err := ContentHash(r)
	if err != nil {
		return err
	}
	gotTuple, _ := hashes["tuple_hash"].(string)
	gotContent, _ := hashes["content_hash"].(string)
	if gotTuple != wantTuple {
		return fmt.Errorf("tuple_hash mismatch: got %s want %s", gotTuple, wantTuple)
	}
	if gotContent != wantContent {
		return fmt.Errorf("content_hash mismatch: got %s want %s", gotContent, wantContent)
	}
	if id != gotContent {
		return fmt.Errorf("id must equal content_hash")
	}
	return nil
}

func ID(r Receipt) string {
	id, _ := r["id"].(string)
	return id
}
