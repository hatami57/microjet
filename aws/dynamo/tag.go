package dynamo

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core/errorx"
)

// keyRole marks whether a struct field owns the partition key or sort key.
type keyRole int

const (
	keyRoleNone keyRole = iota
	keyRolePK
	keyRoleSK
)

// keySegment is one piece of an encoded key — either a literal string or a reference
// to a struct field (identified by its index in the struct).
type keySegment struct {
	literal    string // non-empty for literal segments; empty for field segments
	fieldIndex int    // struct field index; -1 for literal segments
}

// keyFormat is a parsed key pattern derived from a format string like "T:{TenantID}:License:{ID}".
// Segments alternate between literals and field references.
//
// Encoding: literals are copied verbatim; field values are converted to strings and inserted.
// Decoding: literals are matched and consumed; field values are extracted from the remaining
//
//	raw string using the next literal as a delimiter (or the end of string for the last field).
type keyFormat struct {
	segments []keySegment
}

// encode builds the key string by substituting field values into the pattern.
func (kf *keyFormat) encode(v reflect.Value) (string, error) {
	var sb strings.Builder
	for _, seg := range kf.segments {
		if seg.fieldIndex < 0 {
			sb.WriteString(seg.literal)
		} else {
			s, err := fieldToString(v.Field(seg.fieldIndex))
			if err != nil {
				return "", err
			}
			sb.WriteString(s)
		}
	}
	return sb.String(), nil
}

// decode extracts field values from raw by matching the literal delimiters in the pattern.
// Adjacent field segments without a literal separator between them are not supported.
func (kf *keyFormat) decode(raw string, v reflect.Value) error {
	remaining := raw
	for i, seg := range kf.segments {
		if seg.fieldIndex < 0 {
			// Literal: must be a prefix of what remains.
			if !strings.HasPrefix(remaining, seg.literal) {
				return errorx.NewInternalError("dynamo", "expected prefix", "prefix", seg.literal, "raw", raw)
			}
			remaining = remaining[len(seg.literal):]
		} else {
			// Field: extract value up to the next literal (or end of string).
			var value string
			if nextLit := nextLiteral(kf.segments[i+1:]); nextLit != "" {
				idx := strings.Index(remaining, nextLit)
				if idx < 0 {
					return errorx.NewInternalError("dynamo", "separator not found", "separator", nextLit, "raw", raw)
				}
				value = remaining[:idx]
				remaining = remaining[idx:]
			} else {
				value = remaining
				remaining = ""
			}
			if err := setFieldFromString(v.Field(seg.fieldIndex), value); err != nil {
				return err
			}
		}
	}
	return nil
}

// nextLiteral returns the first literal string found in segments, or "" if none.
func nextLiteral(segments []keySegment) string {
	for _, s := range segments {
		if s.fieldIndex < 0 {
			return s.literal
		}
	}
	return ""
}

// parseKeyFormat parses a format string into a keyFormat.
// {FieldName} is replaced with the value of the named struct field.
// {} is shorthand for the field that carries this tag (selfName).
// prefix=X  is treated as format=X{} (X prepended to self-field value).
// const=X   is treated as format=X   (pure literal, no field reference).
func parseKeyFormat(pattern, selfName string, t reflect.Type) (*keyFormat, error) {
	kf := &keyFormat{}
	rest := pattern
	for len(rest) > 0 {
		open := strings.Index(rest, "{")
		if open < 0 {
			kf.segments = append(kf.segments, keySegment{literal: rest, fieldIndex: -1})
			break
		}
		if open > 0 {
			kf.segments = append(kf.segments, keySegment{literal: rest[:open], fieldIndex: -1})
		}
		close := strings.Index(rest[open:], "}")
		if close < 0 {
			return nil, errorx.NewInternalError("dynamo", "unclosed { in format", "format", pattern)
		}
		name := rest[open+1 : open+close]
		if name == "" {
			name = selfName
		}
		idx, err := structFieldIndex(t, name)
		if err != nil {
			return nil, errorx.NewInternalError("dynamo", "format references unknown field", "format", pattern, "field", name, "type", t.Name())
		}
		kf.segments = append(kf.segments, keySegment{fieldIndex: idx})
		rest = rest[open+close+1:]
	}
	return kf, nil
}

// fieldMeta holds parsed dynamo tag metadata for one struct field.
type fieldMeta struct {
	structIndex int
	attrName    string // dynamodbav attribute name (empty if dynamodbav:"-")
	keyRole     keyRole
	format      *keyFormat // non-nil for pk/sk fields
	autoCreate  bool       // set to time.Now() on Put if the field is zero
	autoUpdate  bool       // set to time.Now() on Put and Update (always)
}

// structMeta holds all tag-derived metadata for a struct type T.
type structMeta struct {
	pkField    *fieldMeta
	skField    *fieldMeta
	autoFields []*fieldMeta // fields with auto_create or auto_update
}

var metaCache sync.Map // map[reflect.Type]*structMeta

// getStructMeta returns cached (or newly parsed) structMeta for T.
func getStructMeta[T any]() (*structMeta, error) {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return nil, errorx.NewInternalError("dynamo", "T must not be an interface type")
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if v, ok := metaCache.Load(t); ok {
		return v.(*structMeta), nil
	}
	meta, err := buildStructMeta(t)
	if err != nil {
		return nil, err
	}
	metaCache.Store(t, meta)
	return meta, nil
}

func buildStructMeta(t reflect.Type) (*structMeta, error) {
	meta := &structMeta{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("dynamo")
		if tag == "" {
			continue
		}
		fm := &fieldMeta{
			structIndex: i,
			attrName:    dynamoAttrName(f),
		}
		var rawFormat, prefix, constVal string
		for part := range strings.SplitSeq(tag, ",") {
			part = strings.TrimSpace(part)
			switch {
			case part == "pk":
				fm.keyRole = keyRolePK
			case part == "sk":
				fm.keyRole = keyRoleSK
			case strings.HasPrefix(part, "format="):
				rawFormat = strings.TrimPrefix(part, "format=")
			case strings.HasPrefix(part, "prefix="):
				prefix = strings.TrimPrefix(part, "prefix=")
			case strings.HasPrefix(part, "const="):
				constVal = strings.TrimPrefix(part, "const=")
			case part == "auto_create":
				fm.autoCreate = true
			case part == "auto_update":
				fm.autoUpdate = true
			}
		}

		if fm.keyRole != keyRoleNone {
			// Resolve the format string from whichever option was provided.
			// format= wins; prefix= and const= are sugar for simple cases.
			var pattern string
			switch {
			case rawFormat != "":
				pattern = rawFormat
			case prefix != "":
				pattern = prefix + "{}" // {} expands to this field's own value
			case constVal != "":
				pattern = constVal // pure literal, no field reference
			default:
				pattern = "{}" // bare pk/sk: just the field value
			}
			var err error
			fm.format, err = parseKeyFormat(pattern, f.Name, t)
			if err != nil {
				return nil, err
			}
		}

		switch fm.keyRole {
		case keyRolePK:
			meta.pkField = fm
		case keyRoleSK:
			meta.skField = fm
		}
		if fm.autoCreate || fm.autoUpdate {
			meta.autoFields = append(meta.autoFields, fm)
		}
	}
	if meta.pkField == nil {
		return nil, errorx.NewInternalError("dynamo", "no field tagged with dynamo:\"pk,...\"", "type", t.Name())
	}
	if meta.skField == nil {
		return nil, errorx.NewInternalError("dynamo", "no field tagged with dynamo:\"sk,...\"", "type", t.Name())
	}
	return meta, nil
}

// applyTimestamps sets auto_create (if zero and not isUpdate) and auto_update fields to now.
func applyTimestamps(v reflect.Value, meta *structMeta, isUpdate bool, now time.Time) {
	for _, fm := range meta.autoFields {
		field := v.Field(fm.structIndex)
		if !field.CanSet() {
			continue
		}
		var val reflect.Value
		switch field.Interface().(type) {
		case Timestamp:
			val = reflect.ValueOf(Timestamp(now))
		default:
			val = reflect.ValueOf(now)
		}
		if fm.autoCreate && !isUpdate && field.IsZero() {
			field.Set(val)
		}
		if fm.autoUpdate {
			field.Set(val)
		}
	}
}

// dynamoAttrName returns the DynamoDB attribute name for a struct field
// by reading its dynamodbav tag; returns "" when the field is excluded (dynamodbav:"-").
func dynamoAttrName(f reflect.StructField) string {
	tag := f.Tag.Get("dynamodbav")
	if tag == "" {
		return f.Name
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return ""
	}
	if name == "" {
		return f.Name
	}
	return name
}

// structFieldIndex returns the index of the exported field with the given name.
func structFieldIndex(t reflect.Type, name string) (int, error) {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Name == name {
			return i, nil
		}
	}
	return -1, errorx.NewInternalError("dynamo", "field not found", "field", name, "type", t.Name())
}

// fieldToString converts a struct field value to its string key representation.
func fieldToString(v reflect.Value) (string, error) {
	switch val := v.Interface().(type) {
	case uuid.UUID:
		return val.String(), nil
	case string:
		return val, nil
	default:
		return fmt.Sprintf("%v", v.Interface()), nil
	}
}

// setFieldFromString parses s and assigns it to dest based on its type.
func setFieldFromString(dest reflect.Value, s string) error {
	switch dest.Interface().(type) {
	case uuid.UUID:
		id, err := uuid.Parse(s)
		if err != nil {
			return errorx.NewInternalError("dynamo", "parse uuid failed", "value", s).WithInner(err)
		}
		dest.Set(reflect.ValueOf(id))
	case string:
		dest.SetString(s)
	default:
		return errorx.NewInternalError("dynamo", "unsupported key field type", "type", dest.Type())
	}
	return nil
}
