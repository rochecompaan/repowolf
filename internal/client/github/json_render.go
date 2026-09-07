package github

import (
	"bytes"
	"encoding/json"
)

type orderedJSONField struct {
	name  string
	value any
}

type orderedJSONObject []orderedJSONField

func selectJSONFields(object map[string]any, fields []string) orderedJSONObject {
	selected := make(orderedJSONObject, 0, len(fields))
	for _, field := range fields {
		item, exists := object[field]
		if !exists {
			item = absentJSONValue(field)
		}
		selected = append(selected, orderedJSONField{name: field, value: item})
	}
	return selected
}

func (object orderedJSONObject) MarshalJSON() ([]byte, error) {
	var encoded bytes.Buffer
	encoded.WriteByte('{')
	for index, field := range object {
		if index != 0 {
			encoded.WriteByte(',')
		}
		name, err := json.Marshal(field.name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(field.value)
		if err != nil {
			return nil, err
		}
		encoded.Write(name)
		encoded.WriteByte(':')
		encoded.Write(value)
	}
	encoded.WriteByte('}')
	return encoded.Bytes(), nil
}
