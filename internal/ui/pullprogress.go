package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// IsWriterTTY returns true if w is an *os.File connected to a character device.
func IsWriterTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

type pullEvent struct {
	Status  string       `json:"status"`
	ID      string       `json:"id"`
	Details *pullDetails `json:"progressDetail"`
}

type pullDetails struct {
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
}

// StreamPull reads a Docker ImagePull response and renders compact progress to w.
// When tty is true it overwrites a single line in place; otherwise it prints two
// lines (start + completion).
func StreamPull(r io.Reader, w io.Writer, tty bool) {
	type layerState struct {
		current, total int64
		done           bool
	}
	layers := map[string]*layerState{}
	scanner := bufio.NewScanner(r)
	var image string
	var headerPrinted bool

	for scanner.Scan() {
		var evt pullEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}

		// First event: "Pulling from <image>"
		if !headerPrinted && strings.HasPrefix(evt.Status, "Pulling from ") {
			image = strings.TrimPrefix(evt.Status, "Pulling from ")
			if tty {
				_, _ = fmt.Fprintf(w, "pulling %s ...", image)
			} else {
				_, _ = fmt.Fprintf(w, "pulling %s ...\n", image)
			}
			headerPrinted = true
			continue
		}

		// "Already exists" / "Status: Image is up to date" — nothing to download
		if strings.HasPrefix(evt.Status, "Status:") || evt.Status == "Already exists" {
			continue
		}

		if evt.ID == "" {
			continue
		}

		ls := layers[evt.ID]
		if ls == nil {
			ls = &layerState{}
			layers[evt.ID] = ls
		}

		switch evt.Status {
		case "Downloading":
			if evt.Details != nil {
				ls.current = evt.Details.Current
				ls.total = evt.Details.Total
			}
		case "Pull complete":
			ls.done = true
			if evt.Details != nil && evt.Details.Total > 0 {
				ls.current = evt.Details.Total
				ls.total = evt.Details.Total
			}
		}

		if !tty || !headerPrinted {
			continue
		}

		var totCur, totTot int64
		var nDone int
		for _, l := range layers {
			totCur += l.current
			totTot += l.total
			if l.done {
				nDone++
			}
		}

		sizeStr := ""
		if totTot > 0 {
			sizeStr = fmt.Sprintf("  %s / %s", FormatBytes(totCur), FormatBytes(totTot))
		}
		_, _ = fmt.Fprintf(w, "\rpulling %s ...%s  [%d/%d layers]   ",
			image, sizeStr, nDone, len(layers))
	}

	if !headerPrinted {
		return
	}
	if tty {
		_, _ = fmt.Fprintf(w, "\r\033[K") // clear the progress line
		_, _ = fmt.Fprintf(w, "pulled %s\n", image)
	} else {
		_, _ = fmt.Fprintf(w, "pulled %s\n", image)
	}
}

// FormatBytes formats a byte count as a human-readable string (KiB, MiB, GiB, …).
func FormatBytes(b int64) string {
	if b <= 0 {
		return "?"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
