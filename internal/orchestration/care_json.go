package orchestration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// CanonicalCARE emits recursively sorted JSON. Arrays retain their supplied order.
func CanonicalCARE(value any) ([]byte, error) {
	var raw any
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&raw); err != nil {
		return nil, err
	}
	return canonical(raw)
}
func canonical(v any) ([]byte, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b bytes.Buffer
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			vb, e := canonical(x[k])
			if e != nil {
				return nil, e
			}
			b.Write(vb)
		}
		b.WriteByte('}')
		return b.Bytes(), nil
	case []any:
		var b bytes.Buffer
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			eb, er := canonical(e)
			if er != nil {
				return nil, er
			}
			b.Write(eb)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	default:
		return json.Marshal(x)
	}
}
func CAREDigest(value any) (Digest, error) {
	b, e := CanonicalCARE(value)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return Digest(hex.EncodeToString(h[:])), nil
}
func RequireCanonicalCARE(raw []byte, value any, max int) error {
	if len(raw) == 0 || len(raw) > max {
		return ErrCAREBlocked
	}
	b, e := CanonicalCARE(value)
	if e != nil || !bytes.Equal(raw, b) {
		return fmt.Errorf("%w: non-canonical", ErrCAREBlocked)
	}
	return nil
}
