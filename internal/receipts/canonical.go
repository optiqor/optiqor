package receipts

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Canonical serialises r into the byte sequence that gets signed. The
// determinism properties relied on by Verify and the transparency log:
//   - struct fields emitted in declaration order (encoding/json guarantee);
//   - SetEscapeHTML(false) so timestamps and signatures pass through verbatim;
//   - trailing newline stripped (encoding/json's Encoder appends one).
//
// Two signers given the same Receipt MUST produce identical bytes.
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
