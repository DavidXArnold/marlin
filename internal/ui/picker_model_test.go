package ui

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/DavidXArnold/marlin/internal/config"
)

// --- modelItem ---

func TestModelItemTitle(t *testing.T) {
	item := modelItem{slug: "qwen25-72b", provider: "vllm", status: "working"}
	assert.Equal(t, "qwen25-72b", item.Title())
	assert.Equal(t, "vllm • working", item.Description())
	assert.Equal(t, "qwen25-72b", item.FilterValue())
}

// --- itemDelegate ---

func TestItemDelegateHeightSpacing(t *testing.T) {
	d := itemDelegate{}
	assert.Equal(t, 2, d.Height())
	assert.Equal(t, 0, d.Spacing())
}

func TestItemDelegateUpdate(t *testing.T) {
	d := itemDelegate{}
	cmd := d.Update(nil, nil)
	assert.Nil(t, cmd)
}

func TestItemDelegateRenderSelected(t *testing.T) {
	d := itemDelegate{}
	items := []list.Item{modelItem{slug: "qwen25-72b", provider: "vllm", status: "working"}}
	l := list.New(items, d, 60, 10)
	var buf bytes.Buffer
	d.Render(&buf, l, 0, items[0])
	assert.Contains(t, buf.String(), "qwen25-72b")
}

func TestItemDelegateRenderUnselected(t *testing.T) {
	d := itemDelegate{}
	items := []list.Item{
		modelItem{slug: "qwen25-72b", provider: "vllm", status: "working"},
		modelItem{slug: "llama-8b", provider: "vllm", status: "untested"},
	}
	l := list.New(items, d, 60, 10)
	var buf bytes.Buffer
	// Render index 1 (not selected — index 0 is selected by default)
	d.Render(&buf, l, 1, items[1])
	assert.Contains(t, buf.String(), "llama-8b")
}

// fakeListItem satisfies list.Item but is not a modelItem.
type fakeListItem struct{}

func (fakeListItem) Title() string       { return "fake" }
func (fakeListItem) Description() string { return "fake desc" }
func (fakeListItem) FilterValue() string { return "fake" }

func TestItemDelegateRenderWrongType(t *testing.T) {
	d := itemDelegate{}
	items := []list.Item{fakeListItem{}}
	l := list.New(items, d, 60, 10)
	var buf bytes.Buffer
	// fakeListItem is not a modelItem — type assertion fails — Render writes nothing.
	d.Render(&buf, l, 0, fakeListItem{})
	assert.Empty(t, buf.String())
}

// --- pickerModel ---

func TestPickerModelInit(t *testing.T) {
	pm := pickerModel{}
	assert.Nil(t, pm.Init())
}

func TestPickerModelViewQuitting(t *testing.T) {
	pm := pickerModel{quitting: true}
	assert.Empty(t, pm.View())
}

func TestPickerModelViewNotQuitting(t *testing.T) {
	items := []list.Item{modelItem{slug: "x"}}
	pm := pickerModel{list: list.New(items, itemDelegate{}, 40, 10)}
	assert.NotEmpty(t, pm.View())
}

func TestPickerModelUpdateQuit(t *testing.T) {
	items := []list.Item{modelItem{slug: "x"}}
	pm := pickerModel{list: list.New(items, itemDelegate{}, 40, 10)}
	updated, _ := pm.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.True(t, updated.(pickerModel).quitting)
}

func TestPickerModelUpdateEnter(t *testing.T) {
	items := []list.Item{modelItem{slug: "qwen25-72b"}}
	pm := pickerModel{list: list.New(items, itemDelegate{}, 40, 10)}
	updated, _ := pm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "qwen25-72b", updated.(pickerModel).selected)
}

func TestPickerModelUpdateWindowSize(t *testing.T) {
	items := []list.Item{modelItem{slug: "x"}}
	pm := pickerModel{list: list.New(items, itemDelegate{}, 40, 10)}
	_, cmd := pm.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	_ = cmd // may be nil or a batch
}

func TestPickModelSingleEntry(t *testing.T) {
	got, err := PickModel([]string{"only-one"}, nil, "", "")
	assert.NoError(t, err)
	assert.Equal(t, "only-one", got)
}

func TestPickModelWithCfgs(t *testing.T) {
	cfgs := []*config.ModelConfig{
		{Model: config.ModelMeta{Type: "vllm", Status: "working"}},
	}
	got, err := PickModel([]string{"qwen25-72b"}, cfgs, "", "")
	assert.NoError(t, err)
	assert.Equal(t, "qwen25-72b", got)
}

// --- confirmModel ---

func TestConfirmModelInit(t *testing.T) {
	c := confirmModel{prompt: "ok?"}
	assert.Nil(t, c.Init())
}

func TestConfirmModelViewDone(t *testing.T) {
	c := confirmModel{prompt: "ok?", done: true}
	assert.Empty(t, c.View())
}

func TestConfirmModelViewNotDone(t *testing.T) {
	c := confirmModel{prompt: "ok?"}
	v := c.View()
	assert.Contains(t, v, "ok?")
	assert.Contains(t, v, "[y/n]")
}

func TestConfirmModelUpdateUnknownKey(t *testing.T) {
	c := confirmModel{prompt: "ok?"}
	updated, _ := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	assert.False(t, updated.(confirmModel).done)
}
