// SPDX-License-Identifier: EUPL-1.2

package redo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Sentinel decode errors. The offending token is added by wrapping these with
// %w, keeping them matchable with errors.Is.
var (
	errExpectedObject    = errors.New("redo: parts: expected JSON object")
	errExpectedStringKey = errors.New("redo: parts: expected string key")
)

// Parts is an insertion-ordered map of partition device name to Part. Redo
// Rescue writes partitions in discovery order; preserving that order keeps the
// output stable and easy to diff, even though Go's built-in map encoding would
// otherwise sort keys.
//
//nolint:recvcheck // UnmarshalJSON needs a pointer receiver; the read-only methods stay value receivers by design.
type Parts struct {
	keys []string
	m    map[string]Part
}

// NewParts returns an empty, ready-to-use Parts.
func NewParts() Parts {
	return Parts{keys: nil, m: make(map[string]Part)}
}

// Set inserts or updates the Part for the given device name, recording first
// insertion order.
func (p *Parts) Set(name string, part Part) {
	if p.m == nil {
		p.m = make(map[string]Part)
	}

	if _, exists := p.m[name]; !exists {
		p.keys = append(p.keys, name)
	}

	p.m[name] = part
}

// Get returns the Part for name and whether it was present.
func (p Parts) Get(name string) (Part, bool) {
	part, ok := p.m[name]

	return part, ok
}

// Keys returns the device names in insertion order.
func (p Parts) Keys() []string {
	out := make([]string, len(p.keys))
	copy(out, p.keys)

	return out
}

// Len returns the number of partitions.
func (p Parts) Len() int { return len(p.keys) }

// MarshalJSON renders the partitions as a JSON object in insertion order,
// without HTML escaping, matching the rest of the descriptor.
func (p Parts) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')

	for i, k := range p.keys {
		if i > 0 {
			buf.WriteByte(',')
		}

		key, err := marshalCompact(k)
		if err != nil {
			return nil, err
		}

		buf.Write(key)
		buf.WriteByte(':')

		val, err := marshalCompact(p.m[k])
		if err != nil {
			return nil, err
		}

		buf.Write(val)
	}

	buf.WriteByte('}')

	return buf.Bytes(), nil
}

// UnmarshalJSON decodes a JSON object into Parts while preserving the order in
// which keys appear in the input.
func (p *Parts) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))

	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("redo: parts: %w", err)
	}

	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("%w, got %v", errExpectedObject, tok)
	}

	p.keys = nil
	p.m = make(map[string]Part)

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("redo: parts: %w", err)
		}

		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("%w, got %v", errExpectedStringKey, keyTok)
		}

		var part Part
		if err := dec.Decode(&part); err != nil {
			return fmt.Errorf("redo: parts: decoding %q: %w", key, err)
		}

		p.Set(key, part)
	}

	// Consume the closing '}'.
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("redo: parts: %w", err)
	}

	return nil
}
