package storage

import "encoding/json"

// MustJSON marshals v; on failure returns "null" so event persistence never
// blocks the caller on a marshalling edge case.
func MustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}
