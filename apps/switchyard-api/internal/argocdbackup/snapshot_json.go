package argocdbackup

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

func snapshotJSON(raw []byte) (map[string]interface{}, error) {
	if len(raw) == 0 || len(raw) > MaxPlaintextBytes || !utf8.Valid(raw) {
		return nil, ErrHold
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	value, err := snapshotJSONValue(d, 0)
	if err != nil {
		return nil, ErrHold
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrHold
	}
	object, ok := value.(map[string]interface{})
	if !ok {
		return nil, ErrHold
	}
	return object, nil
}

func snapshotJSONValue(d *json.Decoder, depth int) (interface{}, error) {
	if depth > 64 {
		return nil, ErrHold
	}
	token, err := d.Token()
	if err != nil {
		return nil, ErrHold
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]interface{})
		for d.More() {
			token, err := d.Token()
			name, ok := token.(string)
			if err != nil || !ok || strings.ContainsRune(name, utf8.RuneError) {
				return nil, ErrHold
			}
			if _, duplicate := object[name]; duplicate {
				return nil, ErrHold
			}
			value, err := snapshotJSONValue(d, depth+1)
			if err != nil {
				return nil, ErrHold
			}
			object[name] = value
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, ErrHold
		}
		return object, nil
	case json.Delim('['):
		array := make([]interface{}, 0)
		for d.More() {
			value, err := snapshotJSONValue(d, depth+1)
			if err != nil {
				return nil, ErrHold
			}
			array = append(array, value)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, ErrHold
		}
		return array, nil
	default:
		if _, delimiter := token.(json.Delim); delimiter {
			return nil, ErrHold
		}
		if value, ok := token.(string); ok && strings.ContainsRune(value, utf8.RuneError) {
			return nil, ErrHold
		}
		return token, nil
	}
}
