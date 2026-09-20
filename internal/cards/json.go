package cards

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// wireCard is the on-disk representation of a Card. Field order here defines
// the key order of the generated JSON, which is chosen to match the files
// already committed in PPLALE-web so that a card addition produces a minimal
// diff.
type wireCard struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Type        CardType     `json:"type"`
	Version     *CardVersion `json:"version,omitempty"`
	Fruit       FruitType    `json:"fruit"`
	Description string       `json:"description"`
	ImageURL    string       `json:"imageUrl"`
	Cost        int          `json:"cost"`
	HP          int          `json:"hp"`
	Attack      int          `json:"attack"`
	Effect      *string      `json:"effect,omitempty"`
	Role        *CardRole    `json:"role,omitempty"`
	SweetType   *SweetType   `json:"sweetType,omitempty"`
}

func (c Card) wire() wireCard {
	return wireCard{
		ID: c.ID, Name: c.Name, Type: c.Type, Version: c.Version, Fruit: c.Fruit,
		Description: c.Description, ImageURL: c.ImageURL, Cost: c.Cost, HP: c.HP,
		Attack: c.Attack, Effect: c.Effect, Role: c.Role, SweetType: c.SweetType,
	}
}

func (w wireCard) card() Card {
	return Card{
		ID: w.ID, Name: w.Name, Type: w.Type, Version: w.Version, Fruit: w.Fruit,
		Description: w.Description, ImageURL: w.ImageURL, Cost: w.Cost, HP: w.HP,
		Attack: w.Attack, Effect: w.Effect, Role: w.Role, SweetType: w.SweetType,
	}
}

// Decode parses a dataset file. Unknown fields are rejected: PPLALE-web adding
// a field to CardInfo must surface as a loud failure here rather than being
// silently dropped when the file is written back out.
func Decode(ds Dataset, data []byte) ([]Card, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("cards: decode %s: %w", ds.JSONPath, err)
	}
	if len(raw) != 1 {
		return nil, fmt.Errorf("cards: %s must hold exactly one root key, found %d", ds.JSONPath, len(raw))
	}
	body, ok := raw[ds.RootKey]
	if !ok {
		return nil, fmt.Errorf("cards: %s is missing root key %q", ds.JSONPath, ds.RootKey)
	}

	inner := json.NewDecoder(bytes.NewReader(body))
	inner.DisallowUnknownFields()
	var wires []wireCard
	if err := inner.Decode(&wires); err != nil {
		return nil, fmt.Errorf("cards: decode %s cards: %w", ds.JSONPath, err)
	}

	list := make([]Card, len(wires))
	for i, w := range wires {
		list[i] = w.card()
	}
	return list, nil
}

// Encode renders a dataset file exactly the way PPLALE-web stores it: two
// space indentation, unescaped UTF-8 and a trailing newline.
func Encode(ds Dataset, list []Card) ([]byte, error) {
	var b strings.Builder
	b.WriteString("{\n  ")
	key, err := encodeScalar(ds.RootKey)
	if err != nil {
		return nil, err
	}
	b.Write(key)
	b.WriteString(": [")
	if len(list) == 0 {
		b.WriteString("]\n}\n")
		return []byte(b.String()), nil
	}
	b.WriteString("\n")
	for i, c := range list {
		obj, err := encodeCard(c)
		if err != nil {
			return nil, err
		}
		b.Write(obj)
		if i < len(list)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return []byte(b.String()), nil
}

func encodeCard(c Card) ([]byte, error) {
	buf, err := json.Marshal(c.wire())
	if err != nil {
		return nil, fmt.Errorf("cards: encode %s: %w", c.ID, err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, buf, "    ", "  "); err != nil {
		return nil, fmt.Errorf("cards: indent %s: %w", c.ID, err)
	}
	// json.Marshal escapes <, > and & by default; the committed files do not.
	out, err := unescapeHTML(indented.Bytes())
	if err != nil {
		return nil, err
	}
	return append([]byte("    "), out...), nil
}

func encodeScalar(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("cards: encode value: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// unescapeHTML re-encodes an already valid JSON document with HTML escaping
// disabled while preserving the indentation produced by json.Indent.
func unescapeHTML(in []byte) ([]byte, error) {
	if !bytes.ContainsAny(in, `\`) {
		return in, nil
	}
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		if in[i] == '\\' && i+5 < len(in) && in[i+1] == 'u' {
			switch string(in[i : i+6]) {
			case `<`:
				out = append(out, '<')
				i += 5
				continue
			case `>`:
				out = append(out, '>')
				i += 5
				continue
			case `&`:
				out = append(out, '&')
				i += 5
				continue
			}
		}
		if in[i] == '\\' && i+1 < len(in) {
			out = append(out, in[i], in[i+1])
			i++
			continue
		}
		out = append(out, in[i])
	}
	return out, nil
}
