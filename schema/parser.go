package schema

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/orjanda-framework/orjanda/errors"
)

// ParseFields reflects on a struct type and extracts its Field definitions.
// It skips fields belonging to embedded BaseDocument or BaseChild, and fields marked with `oj:"-"`.
func ParseFields(t reflect.Type) ([]Field, []CompiledChild, error) {
	return parseFieldsInternal(t, nil)
}

func parseFieldsInternal(t reflect.Type, visited []reflect.Type) ([]Field, []CompiledChild, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, nil, nil
	}

	// Check for circular child table definition
	for _, prev := range visited {
		if prev == t {
			return nil, nil, errors.New(errors.CodeValidation, "circular child table definition detected: "+t.Name(), nil, nil)
		}
	}
	visited = append(visited, t)

	var fields []Field
	var children []CompiledChild

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		// Skip embedded BaseDocument and BaseChild
		if f.Anonymous && (f.Name == "BaseDocument" || f.Name == "BaseChild") {
			continue
		}

		tagVal := f.Tag.Get("oj")
		if tagVal == "-" {
			continue
		}

		parsedTags := parseTag(tagVal)

		// Determine field type
		fType := getFieldType(f.Type)

		// If it has oj:"child_table", force it to be child table if it's a slice of structs
		isChildTable := parsedTags["child_table"] == "true"
		if isChildTable && fType != FieldTypeChildTable {
			// Ensure it is indeed a slice of structs
			if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Struct {
				fType = FieldTypeChildTable
			}
		}

		if fType == FieldTypeChildTable {
			elemType := f.Type.Elem()
			childFields, _, err := parseFieldsInternal(elemType, visited)
			if err != nil {
				return nil, nil, err
			}

			typeName := elemType.Name()
			docType := camelToSnake(typeName) // default name for child doc type is snake case of struct name

			children = append(children, CompiledChild{
				FieldName: f.Name,
				TypeName:  typeName,
				DocType:   docType,
				Fields:    childFields,
			})
			continue
		}

		// Build Field
		field := Field{
			Name:        f.Name,
			DBColumn:    camelToSnake(f.Name),
			Type:        fType,
			Required:    parsedTags["required"] == "true",
			Unique:      parsedTags["unique"] == "true",
			Searchable:  parsedTags["searchable"] == "true",
			Label:       parsedTags["label"],
			Default:     parsedTags["default"],
			Format:      parsedTags["format"],
			LinkTarget:  parsedTags["link"],
			Hidden:      parsedTags["hidden"] == "true",
			ReadOnly:    parsedTags["readonly"] == "true",
			Computed:    parsedTags["computed"] == "true",
			AgentHint:   parsedTags["agent_hint"],
			AgentHidden: parsedTags["agent_hidden"] == "true",
		}

		if val, ok := parsedTags["options"]; ok {
			field.Options = strings.Split(val, "|")
		}

		if val, ok := parsedTags["precision"]; ok {
			if prec, err := strconv.Atoi(val); err == nil {
				field.Precision = prec
			}
		}

		if val, ok := parsedTags["permission"]; ok {
			field.PermissionRole = val
		}

		if val, ok := parsedTags["validator"]; ok {
			field.ValidatorName = val
		}

		// Apply label default if empty
		if field.Label == "" {
			field.Label = f.Name
		}

		fields = append(fields, field)
	}

	return fields, children, nil
}

func getFieldType(t reflect.Type) FieldType {
	if t.PkgPath() == "time" && t.Name() == "Time" {
		return FieldTypeDateTime
	}

	// Match custom defined types first
	switch t.String() {
	case "schema.Link", "Link":
		return FieldTypeLink
	case "schema.Date", "Date":
		return FieldTypeDate
	case "schema.DateTime", "DateTime":
		return FieldTypeDateTime
	case "schema.Currency", "Currency":
		return FieldTypeCurrency
	case "schema.Text", "Text":
		return FieldTypeText
	case "schema.RichText", "RichText":
		return FieldTypeRichText
	case "schema.DynamicLink", "DynamicLink":
		return FieldTypeDynamicLink
	case "schema.Attachment", "Attachment":
		return FieldTypeAttachment
	case "schema.JSON", "JSON":
		return FieldTypeJSON
	}

	// Match primitive types
	switch t.Kind() {
	case reflect.String:
		return FieldTypeString
	case reflect.Int:
		return FieldTypeInt
	case reflect.Int64:
		return FieldTypeInt64
	case reflect.Float64:
		return FieldTypeFloat64
	case reflect.Bool:
		return FieldTypeBool
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return FieldTypeJSON
		}
		if t.Elem().Kind() == reflect.Struct {
			return FieldTypeChildTable
		}
	}

	return FieldTypeString
}

func parseTag(tag string) map[string]string {
	res := make(map[string]string)
	if tag == "" || tag == "-" {
		return res
	}
	parts := strings.Split(tag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "="); idx != -1 {
			res[part[:idx]] = part[idx+1:]
		} else {
			res[part] = "true"
		}
	}
	return res
}

func camelToSnake(s string) string {
	var res []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := rune(s[i-1])
			isPrevLower := (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')
			isNextLower := false
			if i+1 < len(s) {
				next := rune(s[i+1])
				isNextLower = next >= 'a' && next <= 'z'
			}
			if isPrevLower || isNextLower {
				res = append(res, '_')
			}
		}
		if r >= 'A' && r <= 'Z' {
			res = append(res, r+'a'-'A')
		} else {
			res = append(res, r)
		}
	}
	return string(res)
}
