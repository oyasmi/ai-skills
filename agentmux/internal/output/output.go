package output

import (
	"encoding/json"
	"io"
)

type Success struct {
	OK       bool   `json:"ok"`
	Command  string `json:"command"`
	Instance string `json:"instance,omitempty"`
	Reused   *bool  `json:"reused,omitempty"`
	Status   string `json:"status,omitempty"`
	Data     any    `json:"data,omitempty"`
}

type Failure struct {
	OK        bool   `json:"ok"`
	Command   string `json:"command"`
	Instance  string `json:"instance,omitempty"`
	ErrorCode string `json:"error_code"`
	Error     string `json:"error"`
}

func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
