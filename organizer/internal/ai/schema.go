package ai

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// JSONSchema represents a subset of JSON Schema specification suitable for LLM Structured Outputs.
type JSONSchema struct {
	Type        string                `json:"type"`
	Description string                `json:"description,omitempty"`
	Properties  map[string]JSONSchema `json:"properties,omitempty"`
	Items       *JSONSchema           `json:"items,omitempty"`
	Required    []string              `json:"required,omitempty"`
	// AdditionalProperties is emitted as `false` in strict mode, as a value
	// schema for map fields, or as `true` for interface{}-valued maps; it is
	// omitted otherwise.
	AdditionalProperties any      `json:"additionalProperties,omitempty"`
	Enum                 []string `json:"enum,omitempty"`
}

// GenerateStrictJSONSchema generates a strict JSON schema where all properties are required and additionalProperties is false.
func GenerateStrictJSONSchema(v any) (JSONSchema, error) {
	if v == nil {
		return JSONSchema{}, fmt.Errorf("cannot generate schema for nil value")
	}

	t := reflect.TypeOf(v)
	return generateSchemaFromType(t, true)
}

// GenerateOpenAPISchema generates a standard OpenAPI 3.0 compatible schema (typically used for Gemini).
func GenerateOpenAPISchema(v any) (JSONSchema, error) {
	if v == nil {
		return JSONSchema{}, fmt.Errorf("cannot generate schema for nil value")
	}

	t := reflect.TypeOf(v)
	return generateSchemaFromType(t, false)
}

func generateSchemaFromType(t reflect.Type, strict bool) (JSONSchema, error) {
	// Dereference pointers
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Struct:
		// time.Time has only unexported fields and would otherwise expand to
		// an empty object schema; serialize it as a string.
		if t == reflect.TypeOf(time.Time{}) {
			return JSONSchema{Type: "string"}, nil
		}
		return generateStructSchema(t, strict)
	case reflect.Slice, reflect.Array:
		elemSchema, err := generateSchemaFromType(t.Elem(), strict)
		if err != nil {
			return JSONSchema{}, err
		}
		return JSONSchema{
			Type:  "array",
			Items: &elemSchema,
		}, nil
	case reflect.String:
		return JSONSchema{Type: "string"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return JSONSchema{Type: "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return JSONSchema{Type: "number"}, nil
	case reflect.Bool:
		return JSONSchema{Type: "boolean"}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return JSONSchema{}, fmt.Errorf("unsupported map key type: %s (only string keys are allowed)", t.Key().Kind().String())
		}
		elem := t.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Interface {
			// interface{} values accept any JSON; dynamic keys stay permitted.
			return JSONSchema{Type: "object", AdditionalProperties: true}, nil
		}
		valueSchema, err := generateSchemaFromType(t.Elem(), strict)
		if err != nil {
			return JSONSchema{}, fmt.Errorf("map value: %w", err)
		}
		return JSONSchema{Type: "object", AdditionalProperties: valueSchema}, nil
	default:
		return JSONSchema{}, fmt.Errorf("unsupported type: %s", t.Kind().String())
	}
}

func generateStructSchema(t reflect.Type, strict bool) (JSONSchema, error) {
	properties := make(map[string]JSONSchema)
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Ignore unexported fields
		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		fieldName := field.Name
		tagParts := strings.Split(jsonTag, ",")
		if tagParts[0] != "" {
			fieldName = tagParts[0]
		}

		fieldSchema, err := generateSchemaFromType(field.Type, strict)
		if err != nil {
			return JSONSchema{}, fmt.Errorf("field %s: %w", field.Name, err)
		}

		if desc := field.Tag.Get("description"); desc != "" {
			fieldSchema.Description = desc
		}

		if enumTag := field.Tag.Get("enum"); enumTag != "" {
			enums := strings.Split(enumTag, ",")
			for idx, e := range enums {
				enums[idx] = strings.TrimSpace(e)
			}
			fieldSchema.Enum = enums
		}

		properties[fieldName] = fieldSchema

		// In strict mode or by default without omitempty, field is required
		if strict {
			required = append(required, fieldName)
		} else {
			// In non-strict mode, check omitempty
			isOmitEmpty := false
			for _, p := range tagParts[1:] {
				if p == "omitempty" {
					isOmitEmpty = true
					break
				}
			}
			if !isOmitEmpty {
				required = append(required, fieldName)
			}
		}
	}

	schema := JSONSchema{
		Type:       "object",
		Properties: properties,
	}
	if strict {
		schema.AdditionalProperties = false
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema, nil
}
