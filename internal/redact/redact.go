// SPDX-License-Identifier: GPL-3.0-or-later

// Package redact replaces the identifiers unique to a physical drive in smartctl
// JSON with fixed placeholders, and checks that a file carries none.
package redact

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Placeholders the committed fixtures use.
const (
	SerialPrefix = "EXAMPLE"
	ID           = 305419896 // 0x12345678, for wwn.id and eui64.ext_id
	FarmWWN      = "0x5000c50012345678"
	HexValue     = "0x0"
)

// idHex is how ID appears inside a hex WWN or EUI-64 string, as in a by-id path.
const idHex = "12345678"

// hexKeys hold an identifier as one hex value; HexValue replaces each.
var hexKeys = map[string]bool{
	"nguid": true, "logical_unit_id": true, "sas_address": true, "attached_sas_address": true,
}

// Finding is one identifier that is not a placeholder.
type Finding struct {
	Path  string
	Value string
}

func (f Finding) String() string { return f.Path + " = " + f.Value }

// Redact returns data with every drive identifier replaced by its placeholder,
// including copies of them inside other strings such as a /dev/disk/by-id path.
// Formatting, key order and number literals are kept exactly.
func Redact(data []byte, serial string) ([]byte, error) {
	if !strings.HasPrefix(serial, SerialPrefix) {
		return nil, fmt.Errorf("serial placeholder %q must start with %q", serial, SerialPrefix)
	}
	v, err := parse(data)
	if err != nil {
		return nil, err
	}
	r := replacer{serial: serial, subs: map[string]string{}}
	r.fields(v)
	r.strings(v)
	var b bytes.Buffer
	encode(&b, v, "")
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// Check lists every identifier in data that is not a placeholder, in document order.
func Check(data []byte) ([]Finding, error) {
	v, err := parse(data)
	if err != nil {
		return nil, err
	}
	var out []Finding
	check(v, "", &out)
	return out, nil
}

// Rename sets the top-level device.name to name and rewrites the old name where
// it opens another string, such as device.info_name or an smartctl.argv entry.
func Rename(data []byte, name string) ([]byte, error) {
	v, err := parse(data)
	if err != nil {
		return nil, err
	}
	root, _ := v.(object)
	dev, _ := root.get("device").(object)
	old, _ := dev.get("name").(string)
	if old == "" {
		return nil, errors.New("no device.name to rename")
	}
	r := func(s string) string {
		if s == old || strings.HasPrefix(s, old+" ") {
			return name + s[len(old):]
		}
		return s
	}
	var visit func(v any)
	visit = func(v any) {
		switch v := v.(type) {
		case []any:
			for i, e := range v {
				if s, ok := e.(string); ok {
					v[i] = r(s)
				} else {
					visit(e)
				}
			}
		case object:
			for i := range v {
				if s, ok := v[i].Val.(string); ok {
					v[i].Val = r(s)
				} else {
					visit(v[i].Val)
				}
			}
		}
	}
	visit(v)
	var b bytes.Buffer
	encode(&b, v, "")
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// DeviceName returns the top-level device.name, or "" when absent.
func DeviceName(data []byte) (string, error) {
	v, err := parse(data)
	if err != nil {
		return "", err
	}
	root, _ := v.(object)
	dev, _ := root.get("device").(object)
	s, _ := dev.get("name").(string)
	return s, nil
}

// Serials returns every serial_number in data, in document order.
func Serials(data []byte) ([]string, error) {
	v, err := parse(data)
	if err != nil {
		return nil, err
	}
	var out []string
	walk(v, func(k string, e any) {
		if s, ok := e.(string); ok && k == "serial_number" {
			out = append(out, s)
		}
	})
	return out, nil
}

type replacer struct {
	serial string
	subs   map[string]string // original text -> placeholder text, for strings elsewhere
}

// sub records old for replacement inside other strings; short values are skipped
// because they could match unrelated text.
func (r *replacer) sub(old, repl string) {
	old = strings.TrimPrefix(strings.ToLower(old), "0x")
	repl = strings.TrimPrefix(strings.ToLower(repl), "0x")
	if _, dup := r.subs[old]; len(old) >= 6 && old != repl && !dup {
		r.subs[old] = repl
	}
}

func (r *replacer) fields(v any) {
	switch v := v.(type) {
	case []any:
		for _, e := range v {
			r.fields(e)
		}
	case object:
		for i := range v {
			m := &v[i]
			switch {
			case m.Key == "serial_number":
				if s, ok := m.Val.(string); ok {
					if len(s) >= 4 && s != r.serial {
						r.subs[s] = r.serial
					}
					m.Val = r.serial
				}
			case m.Key == "world_wide_name":
				if s, ok := m.Val.(string); ok {
					r.sub(s, FarmWWN)
				}
				m.Val = FarmWWN
			case m.Key == "wwn":
				r.wide(m.Val, "id", 60, 36)
			case m.Key == "eui64":
				r.wide(m.Val, "ext_id", -1, 40)
			case hexKeys[m.Key]:
				// Elsewhere the same value can be an EUI-64 in a by-id path, so it keeps its width there.
				if s, ok := m.Val.(string); ok && len(s) > 2+len(idHex) {
					body := strings.TrimPrefix(s, "0x")
					r.sub(s, strings.Repeat("0", max(0, len(body)-len(idHex)))+idHex)
				}
				m.Val = HexValue
			default:
				r.fields(m.Val)
			}
		}
	}
}

// wide sets obj[idKey] to ID and records the 64-bit hex form before and after,
// built as naa<<naaShift | oui<<ouiShift | id (naaShift < 0: no naa field).
func (r *replacer) wide(v any, idKey string, naaShift, ouiShift int) {
	o, ok := v.(object)
	if !ok {
		return
	}
	num := func(k string) uint64 {
		n, _ := o.get(k).(json.Number)
		u, _ := strconv.ParseUint(string(n), 10, 64)
		return u
	}
	head := num("oui") << ouiShift
	if naaShift >= 0 {
		head |= num("naa") << naaShift
	}
	if o.get(idKey) == nil {
		return
	}
	r.sub(fmt.Sprintf("%016x", head|num(idKey)), fmt.Sprintf("%016x", head|ID))
	o.set(idKey, json.Number(strconv.Itoa(ID)))
}

func (r *replacer) strings(v any) {
	switch v := v.(type) {
	case []any:
		for i, e := range v {
			if s, ok := e.(string); ok {
				v[i] = r.replace(s)
			} else {
				r.strings(e)
			}
		}
	case object:
		for i := range v {
			if s, ok := v[i].Val.(string); ok {
				v[i].Val = r.replace(s)
			} else {
				r.strings(v[i].Val)
			}
		}
	}
}

func (r *replacer) replace(s string) string {
	olds := slices.Collect(maps.Keys(r.subs))
	slices.SortFunc(olds, func(a, b string) int { return cmp.Or(len(b)-len(a), strings.Compare(a, b)) })
	for _, old := range olds {
		repl := r.subs[old]
		s = strings.ReplaceAll(s, old, repl)
		s = strings.ReplaceAll(s, strings.ToUpper(old), strings.ToUpper(repl))
	}
	return s
}

func check(v any, path string, out *[]Finding) {
	bad := func(p string, e any) { *out = append(*out, Finding{p, show(e)}) }
	switch v := v.(type) {
	case []any:
		for i, e := range v {
			check(e, path+"["+strconv.Itoa(i)+"]", out)
		}
	case object:
		for _, m := range v {
			at := path + "." + m.Key
			switch {
			case m.Key == "serial_number":
				if s, _ := m.Val.(string); s != "" && !strings.HasPrefix(s, SerialPrefix) {
					bad(at, m.Val)
				}
			case m.Key == "world_wide_name":
				if m.Val != FarmWWN {
					bad(at, m.Val)
				}
			case m.Key == "wwn" || m.Key == "eui64":
				idKey := map[string]string{"wwn": "id", "eui64": "ext_id"}[m.Key]
				if o, ok := m.Val.(object); ok && o.get(idKey) != json.Number(strconv.Itoa(ID)) {
					bad(at+"."+idKey, o.get(idKey))
				}
			case hexKeys[m.Key]:
				if m.Val != HexValue && m.Val != json.Number("0") {
					bad(at, m.Val)
				}
			default:
				check(m.Val, at, out)
			}
		}
	case string:
		if strings.Contains(v, "/by-id/") && !strings.Contains(v, SerialPrefix) && !strings.Contains(v, idHex) {
			bad(path, v)
		}
	}
}

func show(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(v)
}

// walk calls fn for every object member in v, depth first.
func walk(v any, fn func(key string, val any)) {
	switch v := v.(type) {
	case []any:
		for _, e := range v {
			walk(e, fn)
		}
	case object:
		for _, m := range v {
			fn(m.Key, m.Val)
			walk(m.Val, fn)
		}
	}
}

// object is a JSON object that keeps its key order; values are object, []any,
// string, json.Number, bool or nil.
type object []member

type member struct {
	Key string
	Val any
}

func (o object) get(k string) any {
	for _, m := range o {
		if m.Key == k {
			return m.Val
		}
	}
	return nil
}

func (o object) set(k string, v any) {
	for i := range o {
		if o[i].Key == k {
			o[i].Val = v
		}
	}
}

func parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON document")
	}
	return v, nil
}

func decode(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if d == '[' {
		a := []any{}
		for dec.More() {
			e, err := decode(dec)
			if err != nil {
				return nil, err
			}
			a = append(a, e)
		}
		_, err := dec.Token()
		return a, err
	}
	o := object{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		e, err := decode(dec)
		if err != nil {
			return nil, err
		}
		o = append(o, member{k.(string), e})
	}
	_, err = dec.Token()
	return o, err
}

// encode writes v in smartctl's own layout: two-space indent, one value per line.
func encode(b *bytes.Buffer, v any, indent string) {
	inner := indent + "  "
	switch v := v.(type) {
	case object:
		if len(v) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, m := range v {
			b.WriteString(inner)
			encodeString(b, m.Key)
			b.WriteString(": ")
			encode(b, m.Val, inner)
			if i < len(v)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(indent + "}")
	case []any:
		if len(v) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range v {
			b.WriteString(inner)
			encode(b, e, inner)
			if i < len(v)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(indent + "]")
	case string:
		encodeString(b, v)
	case json.Number:
		b.WriteString(string(v))
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case nil:
		b.WriteString("null")
	}
}

func encodeString(b *bytes.Buffer, s string) {
	var t bytes.Buffer
	enc := json.NewEncoder(&t)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	b.Write(bytes.TrimSuffix(t.Bytes(), []byte("\n")))
}
