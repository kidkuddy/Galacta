package anthropic

import "encoding/json"

// MarshalJSON merges Tools and ServerTools into one "tools" JSON array.
func (r MessageRequest) MarshalJSON() ([]byte, error) {
	// Use an alias to avoid infinite recursion.
	type Alias MessageRequest
	a := struct {
		Alias
		MergedTools json.RawMessage `json:"tools,omitempty"`
	}{
		Alias: (Alias)(r),
	}

	// If there are no server tools, marshal Tools normally.
	if len(r.ServerTools) == 0 {
		a.MergedTools = nil
		// Clear Tools on alias so the default tag doesn't double-emit.
		a.Alias.Tools = nil

		if len(r.Tools) > 0 {
			toolsJSON, err := json.Marshal(r.Tools)
			if err != nil {
				return nil, err
			}
			a.MergedTools = toolsJSON
		}
		return json.Marshal(a)
	}

	// Merge: marshal each Tool and each ServerTool as raw JSON, combine into one array.
	var merged []json.RawMessage

	for _, t := range r.Tools {
		b, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		merged = append(merged, b)
	}
	for _, st := range r.ServerTools {
		b, err := json.Marshal(st)
		if err != nil {
			return nil, err
		}
		merged = append(merged, b)
	}

	a.Alias.Tools = nil // prevent default tools field
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	a.MergedTools = mergedJSON

	return json.Marshal(a)
}
