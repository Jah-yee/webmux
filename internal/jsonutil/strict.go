package jsonutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Decode rejects duplicate and case-folded keys before encoding/json can
// silently merge them. Errors intentionally contain no input text.
func Decode(data []byte, out any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := jsonValue(d, 0); err != nil {
		return errors.New("invalid JSON document")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	if err := exactShape(data, reflect.TypeOf(out)); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("invalid JSON fields")
	}
	return nil
}
func jsonValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("JSON depth")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return errors.New("duplicate JSON key")
			}
			seen[s] = true
			if err := jsonValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := jsonValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid delimiter")
	}
	_, err = d.Token()
	return err
}
func exactShape(data []byte, t reflect.Type) error {
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if json.Unmarshal(data, &object) != nil {
			return errors.New("invalid object")
		}
		fields := map[string]reflect.Type{}
		collectFields(t, fields)
		for key, value := range object {
			ft, ok := fields[key]
			if !ok {
				return errors.New("unknown JSON field")
			}
			if err := exactShape(value, ft); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if json.Unmarshal(data, &values) != nil {
			return errors.New("invalid array")
		}
		for _, v := range values {
			if err := exactShape(v, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if json.Unmarshal(data, &values) != nil {
			return errors.New("invalid map")
		}
		for _, v := range values {
			if err := exactShape(v, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
func collectFields(t reflect.Type, fields map[string]reflect.Type) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "-" {
			continue
		}
		if f.Anonymous && tag == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectFields(ft, fields)
				continue
			}
		}
		if tag == "" {
			tag = f.Name
		}
		fields[tag] = f.Type
	}
}

// Required checks persisted documents against their declared shape. Unlike a
// PATCH, a stored snapshot cannot omit non-optional fields and invent defaults.
func Required(data []byte, out any) error {
	return requiredShape(data, reflect.TypeOf(out))
}
func requiredShape(data []byte, t reflect.Type) error {
	if t == nil {
		return nil
	}
	if string(bytes.TrimSpace(data)) == "null" {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
			return nil
		}
		return errors.New("null required value")
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			return errors.New("invalid object")
		}
		var fields func(reflect.Type) error
		fields = func(typ reflect.Type) error {
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				if !f.IsExported() {
					continue
				}
				tags := strings.Split(f.Tag.Get("json"), ",")
				key := tags[0]
				if key == "-" {
					continue
				}
				if f.Anonymous && key == "" {
					ft := f.Type
					for ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
					if ft.Kind() == reflect.Struct {
						if err := fields(ft); err != nil {
							return err
						}
						continue
					}
				}
				if key == "" {
					key = f.Name
				}
				value, present := object[key]
				optional := false
				for _, tag := range tags[1:] {
					if tag == "omitempty" || tag == "omitzero" {
						optional = true
					}
				}
				if !present {
					if !optional {
						return errors.New("missing required field")
					}
					continue
				}
				if err := requiredShape(value, f.Type); err != nil {
					return err
				}
			}
			return nil
		}
		return fields(t)
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return errors.New("invalid array")
		}
		for _, v := range values {
			if err := requiredShape(v, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return errors.New("invalid map")
		}
		for _, v := range values {
			if err := requiredShape(v, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
