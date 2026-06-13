package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const pullFixture = `{"status":"Pulling from nvcr.io/nim/meta/llama3-8b","id":"latest"}
{"status":"Pulling fs layer","progressDetail":{},"id":"abc1"}
{"status":"Pulling fs layer","progressDetail":{},"id":"abc2"}
{"status":"Downloading","progressDetail":{"current":52428800,"total":1073741824},"id":"abc1"}
{"status":"Downloading","progressDetail":{"current":10485760,"total":524288000},"id":"abc2"}
{"status":"Pull complete","progressDetail":{},"id":"abc1"}
{"status":"Pull complete","progressDetail":{},"id":"abc2"}
{"status":"Digest: sha256:deadbeef"}
{"status":"Status: Downloaded newer image for nvcr.io/nim/meta/llama3-8b:latest"}
`

func TestStreamPullNonTTY(t *testing.T) {
	var buf bytes.Buffer
	StreamPull(strings.NewReader(pullFixture), &buf, false)
	out := buf.String()
	assert.Contains(t, out, "pulling nvcr.io/nim/meta/llama3-8b")
	assert.Contains(t, out, "pulled")
}

func TestStreamPullTTY(t *testing.T) {
	var buf bytes.Buffer
	StreamPull(strings.NewReader(pullFixture), &buf, true)
	out := buf.String()
	// TTY mode clears the line and prints a final "pulled" line.
	assert.Contains(t, out, "pulled nvcr.io/nim/meta/llama3-8b")
}

func TestStreamPullEmptyStream(t *testing.T) {
	var buf bytes.Buffer
	StreamPull(strings.NewReader(""), &buf, false)
	assert.Empty(t, buf.String()) // no header event → no output
}

func TestStreamPullAlreadyExists(t *testing.T) {
	fixture := `{"status":"Pulling from library/alpine","id":"latest"}
{"status":"Already exists","progressDetail":{},"id":"layer1"}
{"status":"Digest: sha256:abc"}
{"status":"Status: Image is up to date for alpine:latest"}
`
	var buf bytes.Buffer
	StreamPull(strings.NewReader(fixture), &buf, false)
	assert.Contains(t, buf.String(), "pulling library/alpine")
}

func TestFormatBytes(t *testing.T) {
	assert.Equal(t, "512 B", FormatBytes(512))
	assert.Equal(t, "1.0 KiB", FormatBytes(1024))
	assert.Equal(t, "1.5 MiB", FormatBytes(1536*1024))
	assert.Equal(t, "2.0 GiB", FormatBytes(2*1024*1024*1024))
	assert.Equal(t, "?", FormatBytes(0))
}

func TestIsWriterTTY(t *testing.T) {
	// bytes.Buffer is not a TTY.
	assert.False(t, IsWriterTTY(&bytes.Buffer{}))
}
