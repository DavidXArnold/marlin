package cmd

import (
	"encoding/json"
	"fmt"
	"io"
)

// outputFormat holds the current --output flag value; set by root's persistent flag.
var outputFormat string

// writeJSON encodes v to w as indented JSON followed by a newline.
func writeJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// writeJSONLine encodes v to w as compact JSON followed by a newline.
func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
