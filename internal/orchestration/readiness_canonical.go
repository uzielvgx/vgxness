package orchestration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"unicode/utf8"
)

const maxCanonicalBytes = 64 * 1024

func CanonicalJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > maxCanonicalBytes || !utf8.Valid(raw) {
		return nil, errors.New("invalid canonical json")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := decodeJSON(d)
	if err != nil {
		return nil, err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("trailing json")
	}
	return CanonicalValue(v)
}
func decodeJSON(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch x := t.(type) {
	case json.Delim:
		switch x {
		case '{':
			m := map[string]any{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return nil, e
				}
				key, ok := k.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				if _, exists := m[key]; exists {
					return nil, errors.New("duplicate key")
				}
				v, e := decodeJSON(d)
				if e != nil {
					return nil, e
				}
				m[key] = v
			}
			_, err = d.Token()
			return m, err
		case '[':
			a := []any{}
			for d.More() {
				v, e := decodeJSON(d)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			_, err = d.Token()
			return a, err
		default:
			return nil, errors.New("bad delimiter")
		}
	default:
		return t, nil
	}
}
func CanonicalValue(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeCanonical(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func writeCanonical(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case string:
		z, _ := json.Marshal(x)
		b.Write(z)
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		if _, err := x.Float64(); err != nil {
			return err
		}
		b.WriteString(x.String())
	case float64:
		z, err := json.Marshal(x)
		if err != nil {
			return err
		}
		b.Write(z)
	case []any:
		b.WriteByte('[')
		for i, v := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, v); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var normal any
		if err = json.Unmarshal(raw, &normal); err != nil {
			return err
		}
		if m, ok := normal.(map[string]any); ok {
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				z, _ := json.Marshal(k)
				b.Write(z)
				b.WriteByte(':')
				if err := writeCanonical(b, m[k]); err != nil {
					return err
				}
			}
			b.WriteByte('}')
		} else {
			return writeCanonical(b, normal)
		}
	}
	return nil
}
