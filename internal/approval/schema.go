package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

type fieldKind string

const (
	kindString      fieldKind = "string"
	kindBool        fieldKind = "bool"
	kindNumber      fieldKind = "number"
	kindObject      fieldKind = "object"
	kindStringArray fieldKind = "string_array"
	kindObjectArray fieldKind = "object_array"
	kindTodoArray   fieldKind = "todo_array"
	kindEditArray   fieldKind = "edit_array"
)

type toolSchema struct {
	required map[string]fieldKind
	optional map[string]fieldKind
}

var claudeToolSchemas = map[string]toolSchema{
	"Bash": {
		required: map[string]fieldKind{"command": kindString},
		optional: map[string]fieldKind{"description": kindString, "timeout": kindNumber},
	},
	"Read": {
		required: map[string]fieldKind{"file_path": kindString},
		optional: map[string]fieldKind{"offset": kindNumber, "limit": kindNumber},
	},
	"Write": {
		required: map[string]fieldKind{"file_path": kindString, "content": kindString},
	},
	"Edit": {
		required: map[string]fieldKind{"file_path": kindString, "old_string": kindString, "new_string": kindString},
		optional: map[string]fieldKind{"replace_all": kindBool},
	},
	"MultiEdit": {
		required: map[string]fieldKind{"file_path": kindString, "edits": kindEditArray},
	},
	"Glob": {
		required: map[string]fieldKind{"pattern": kindString},
		optional: map[string]fieldKind{"path": kindString},
	},
	"Grep": {
		required: map[string]fieldKind{"pattern": kindString},
		optional: map[string]fieldKind{"path": kindString, "include": kindString},
	},
	"LS": {
		required: map[string]fieldKind{"path": kindString},
		optional: map[string]fieldKind{"ignore": kindStringArray},
	},
	"WebFetch": {
		required: map[string]fieldKind{"url": kindString, "prompt": kindString},
	},
	"WebSearch": {
		required: map[string]fieldKind{"query": kindString},
		optional: map[string]fieldKind{"allowed_domains": kindStringArray, "blocked_domains": kindStringArray},
	},
	"TodoWrite": {
		required: map[string]fieldKind{"todos": kindTodoArray},
	},
	"NotebookEdit": {
		required: map[string]fieldKind{"notebook_path": kindString, "cell_id": kindString, "new_source": kindString},
		optional: map[string]fieldKind{"cell_type": kindString, "edit_mode": kindString},
	},
}

func ValidateEditedInput(tool, originalJSON, editedJSON string) error {
	edited, err := decodeObject(editedJSON)
	if err != nil {
		return err
	}
	if schema, ok := claudeToolSchemas[tool]; ok {
		return validateObject(tool, edited, schema)
	}
	var original any
	if err := decodeJSON(originalJSON, &original); err != nil {
		return fmt.Errorf("original input schema unavailable: %w", err)
	}
	var editedAny any
	if err := decodeJSON(editedJSON, &editedAny); err != nil {
		return err
	}
	if !sameShape(original, editedAny) {
		return fmt.Errorf("%s edited input must preserve original JSON shape", toolName(tool))
	}
	return nil
}

func validateObject(tool string, got map[string]any, schema toolSchema) error {
	allowed := map[string]fieldKind{}
	for k, v := range schema.required {
		allowed[k] = v
		if _, ok := got[k]; !ok {
			return fmt.Errorf("%s input missing required field %q", toolName(tool), k)
		}
	}
	for k, v := range schema.optional {
		allowed[k] = v
	}
	for k, v := range got {
		kind, ok := allowed[k]
		if !ok {
			return fmt.Errorf("%s input field %q is not allowed", toolName(tool), k)
		}
		if err := validateKind(tool, k, v, kind); err != nil {
			return err
		}
	}
	return nil
}

func validateKind(tool, field string, v any, kind fieldKind) error {
	switch kind {
	case kindString:
		if _, ok := v.(string); ok {
			return nil
		}
	case kindBool:
		if _, ok := v.(bool); ok {
			return nil
		}
	case kindNumber:
		if _, ok := v.(json.Number); ok {
			return nil
		}
	case kindObject:
		if _, ok := v.(map[string]any); ok {
			return nil
		}
	case kindStringArray:
		return validateArray(tool, field, v, func(x any) bool {
			_, ok := x.(string)
			return ok
		})
	case kindObjectArray:
		return validateArray(tool, field, v, func(x any) bool {
			_, ok := x.(map[string]any)
			return ok
		})
	case kindTodoArray:
		return validateTodoArray(tool, field, v)
	case kindEditArray:
		return validateEditArray(tool, field, v)
	}
	return fmt.Errorf("%s input field %q must be %s", toolName(tool), field, kind)
}

func validateArray(tool, field string, v any, each func(any) bool) error {
	xs, ok := v.([]any)
	if !ok {
		return fmt.Errorf("%s input field %q must be an array", toolName(tool), field)
	}
	for i, x := range xs {
		if !each(x) {
			return fmt.Errorf("%s input field %q[%d] has invalid type", toolName(tool), field, i)
		}
	}
	return nil
}

func validateEditArray(tool, field string, v any) error {
	return validateArray(tool, field, v, func(x any) bool {
		m, ok := x.(map[string]any)
		if !ok {
			return false
		}
		schema := toolSchema{
			required: map[string]fieldKind{"old_string": kindString, "new_string": kindString},
			optional: map[string]fieldKind{"replace_all": kindBool},
		}
		return validateObject(tool+".edits", m, schema) == nil
	})
}

func validateTodoArray(tool, field string, v any) error {
	return validateArray(tool, field, v, func(x any) bool {
		m, ok := x.(map[string]any)
		if !ok {
			return false
		}
		for _, k := range []string{"content", "status"} {
			if _, ok := m[k].(string); !ok {
				return false
			}
		}
		for k, v := range m {
			switch k {
			case "id", "content", "status", "activeForm", "priority":
				if _, ok := v.(string); !ok {
					return false
				}
			default:
				return false
			}
		}
		return true
	})
}

func decodeObject(s string) (map[string]any, error) {
	var m map[string]any
	if err := decodeJSON(s, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("edited input must be a JSON object")
	}
	return m, nil
}

func decodeJSON(s string, out any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid JSON: multiple JSON values")
	}
	return nil
}

func sameShape(a, b any) bool {
	switch ax := a.(type) {
	case map[string]any:
		bx, ok := b.(map[string]any)
		if !ok || len(ax) != len(bx) {
			return false
		}
		for k, av := range ax {
			bv, ok := bx[k]
			if !ok || !sameShape(av, bv) {
				return false
			}
		}
		return true
	case []any:
		bx, ok := b.([]any)
		if !ok {
			return false
		}
		if len(ax) == 0 || len(bx) == 0 {
			return len(ax) == len(bx)
		}
		return sameShape(ax[0], bx[0])
	case json.Number:
		_, ok := b.(json.Number)
		return ok
	default:
		return reflect.TypeOf(a) == reflect.TypeOf(b)
	}
}

func toolName(tool string) string {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "tool"
	}
	return tool
}
