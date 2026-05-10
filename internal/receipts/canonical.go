package receipts

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Canonical serialises r into a stable byte sequence suitable for
// hashing or signing. Two signers presented with the same Receipt MUST
// produce the same bytes.
//
// Properties of the encoding:
//
//   - struct fields are emitted in declaration order (Go's json package
//     guarantees this);
//   - no HTML-escaping (so timestamps + signatures don't get mangled);
//   - no trailing newline.
//
// The function is intentionally tiny so a second-pair audit of the
// Receipt → bytes path is fast.
func Canonical(r Receipt) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	out = bytes.TrimRight(out, "\n")
	if len(out) == 0 {
		return nil, errors.New("receipts: canonical produced empty bytes")
	}
	return out, nil
}

func unmarshal(b []byte, r *Receipt) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(r)
}
