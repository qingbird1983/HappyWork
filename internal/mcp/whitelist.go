package mcp

import (
	"encoding/json"
	"fmt"
)

// schemaKeys is the recursive whitelist from mcp.md §5. Unknown keywords are
// rejected rather than "repaired".
var schemaKeys = map[string]bool{
	"type": true, "properties": true, "required": true, "description": true,
	"title": true, "default": true, "enum": true, "items": true,
	"anyOf": true, "oneOf": true, "allOf": true, "const": true,
	"minItems": true, "maxItems": true, "minLength": true, "maxLength": true,
	"pattern": true, "format": true, "minimum": true, "maximum": true,
	"exclusiveMinimum": true, "exclusiveMaximum": true, "additionalProperties": true,
}

const (
	maxSchemaBytes = 64 << 10
	maxSchemaDepth = 12
	maxDescription = 4 << 10
)

// validateSchema enforces object root, size, depth and the key whitelist.
func validateSchema(raw json.RawMessage) error {
	if len(raw) > maxSchemaBytes {
		return fmt.Errorf("schema is %d bytes, limit is %d", len(raw), maxSchemaBytes)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("schema is not valid JSON: %w", err)
	}
	if err := walkSchema(v, 0); err != nil {
		return err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("schema root must be an object")
	}
	if root["type"] != "object" {
		return fmt.Errorf("schema root type must be object")
	}
	return nil
}

func walkSchema(v any, depth int) error {
	if depth > maxSchemaDepth {
		return fmt.Errorf("schema nesting exceeds %d", maxSchemaDepth)
	}
	switch typed := v.(type) {
	case map[string]any:
		for key, value := range typed {
			if !schemaKeys[key] {
				return fmt.Errorf("schema keyword %q is not allowed", key)
			}
			if key == "description" {
				text, ok := value.(string)
				if !ok {
					return fmt.Errorf("description must be a string")
				}
				if len(text) > maxDescription {
					return fmt.Errorf("description exceeds %d bytes", maxDescription)
				}
				continue
			}
			switch key {
			case "properties":
				props, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("properties must be an object")
				}
				for _, sub := range props {
					if err := walkSchema(sub, depth+1); err != nil {
						return err
					}
				}
			case "items", "additionalProperties":
				// additionalProperties may be a bool.
				if _, isBool := value.(bool); !isBool {
					if err := walkSchema(value, depth+1); err != nil {
						return err
					}
				}
			case "anyOf", "oneOf", "allOf":
				arr, ok := value.([]any)
				if !ok {
					return fmt.Errorf("%s must be an array", key)
				}
				for _, sub := range arr {
					if err := walkSchema(sub, depth+1); err != nil {
						return err
					}
				}
			}
		}
	case []any:
		for _, sub := range typed {
			if err := walkSchema(sub, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
