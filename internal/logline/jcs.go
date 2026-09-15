package logline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// Canonicalize implements the JSON Canonicalization Scheme surface used by
// logline.receipt.v0: UTF-16 object-key ordering, minimal JSON string escaping,
// array order preservation, and ECMAScript-compatible float serialization.
func Canonicalize(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeCanonical(&b, normalize(v)); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func normalize(v any) any {
	raw, ok := v.(json.RawMessage)
	if ok {
		var out any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&out) == nil {
			return out
		}
	}
	return v
}

func writeCanonical(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeJSONString(b, x)
	case json.Number:
		return writeNumber(b, x.String())
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Errorf("non-finite number is not valid JSON")
		}
		if x == 0 {
			b.WriteByte('0')
			return nil
		}
		raw, err := json.Marshal(x)
		if err != nil {
			return err
		}
		b.Write(raw)
	case float32:
		return writeCanonical(b, float64(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int8:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int16:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case uint:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint8:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint16:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint32:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		b.WriteString(strconv.FormatUint(x, 10))
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case []string:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, item)
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("canonicalize %T: %w", v, err)
		}
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			return err
		}
		return writeCanonical(b, decoded)
	}
	return nil
}

func writeNumber(b *bytes.Buffer, s string) error {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("invalid JSON number %q", s)
	}
	if f == 0 {
		b.WriteByte('0')
		return nil
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	b.Write(raw)
	return nil
}

func writeJSONString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && n == 1 {
			// Match JSON behavior for malformed input: replacement character.
			r = '\uFFFD'
		}
		s = s[n:]
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r >= 0 && r <= 0x1f {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

func utf16Less(a, c string) bool {
	aa := utf16.Encode([]rune(a))
	cc := utf16.Encode([]rune(c))
	n := len(aa)
	if len(cc) < n {
		n = len(cc)
	}
	for i := 0; i < n; i++ {
		if aa[i] != cc[i] {
			return aa[i] < cc[i]
		}
	}
	return len(aa) < len(cc)
}
