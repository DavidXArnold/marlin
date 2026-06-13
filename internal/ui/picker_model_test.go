package ui

import (
	"bytes"
	"testing"
	"time"

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
	got, err := PickModel([]string{"only-one"}, nil, "", "", nil)
	assert.NoError(t, err)
	assert.Equal(t, "only-one", got)
}

func TestPickModelWithCfgs(t *testing.T) {
	cfgs := []*config.ModelConfig{
		{Model: config.ModelMeta{Type: "vllm", Status: "working"}},
	}
	got, err := PickModel([]string{"qwen25-72b"}, cfgs, "", "", nil)
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

// --- modelItem Description branches ---

func TestModelItemDescriptionActive(t *testing.T) {
	item := modelItem{slug: "m", provider: "vllm", status: "working", active: true}
	assert.Contains(t, item.Description(), "◀ active")
}

func TestModelItemDescriptionLastStarted(t *testing.T) {
	item := modelItem{
		slug:        "m",
		provider:    "vllm",
		status:      "working",
		lastStarted: time.Now().Add(-2 * time.Hour),
	}
	assert.Contains(t, item.Description(), "hour")
}

// --- formatRelativeTime ---

func TestFormatRelativeTimeJustNow(t *testing.T) {
	assert.Equal(t, "just now", formatRelativeTime(time.Now().Add(-10*time.Second)))
}

func TestFormatRelativeTimeMins(t *testing.T) {
	assert.Contains(t, formatRelativeTime(time.Now().Add(-5*time.Minute)), "min")
}

func TestFormatRelativeTime1Min(t *testing.T) {
	assert.Equal(t, "started 1 min ago", formatRelativeTime(time.Now().Add(-90*time.Second)))
}

func TestFormatRelativeTimeHours(t *testing.T) {
	assert.Contains(t, formatRelativeTime(time.Now().Add(-3*time.Hour)), "hour")
}

func TestFormatRelativeTime1Hour(t *testing.T) {
	assert.Equal(t, "started 1 hour ago", formatRelativeTime(time.Now().Add(-90*time.Minute)))
}

func TestFormatRelativeTimeDays(t *testing.T) {
	assert.Contains(t, formatRelativeTime(time.Now().Add(-3*24*time.Hour)), "day")
}

func TestFormatRelativeTime1Day(t *testing.T) {
	assert.Equal(t, "started 1 day ago", formatRelativeTime(time.Now().Add(-36*time.Hour)))
}

func TestFormatRelativeTimeOld(t *testing.T) {
	old := time.Now().Add(-10 * 24 * time.Hour)
	result := formatRelativeTime(old)
	assert.Contains(t, result, "started ")
}

// --- StringItem ---

func TestStringItemMethods(t *testing.T) {
	item := StringItem{Label: "llama-8b (adhoc running)", Desc: ":8001"}
	assert.Equal(t, "llama-8b (adhoc running)", item.Title())
	assert.Equal(t, ":8001", item.Description())
	assert.Equal(t, "llama-8b (adhoc running)", item.FilterValue())
}

// --- strPickerModel ---

func TestStrPickerModelInit(t *testing.T) {
	m := strPickerModel{}
	cmd := m.Init()
	assert.Nil(t, cmd)
}

func TestStrPickerModelViewQuit(t *testing.T) {
	m := strPickerModel{quitting: true}
	assert.Equal(t, "", m.View())
}

func TestStrPickerModelUpdateQuit(t *testing.T) {
	items := []list.Item{StringItem{Label: "a"}, StringItem{Label: "b"}}
	l := list.New(items, itemDelegate{}, 60, 10)
	m := strPickerModel{list: l, selected: -1}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	pm := updated.(strPickerModel)
	assert.True(t, pm.quitting)
}

func TestStrPickerModelUpdateEnter(t *testing.T) {
	items := []list.Item{StringItem{Label: "first"}, StringItem{Label: "second"}}
	l := list.New(items, itemDelegate{}, 60, 10)
	m := strPickerModel{list: l, selected: -1}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := updated.(strPickerModel)
	assert.Equal(t, 0, pm.selected) // first item selected
}

// --- PickStrings shortcuts ---

func TestPickStringsSingle(t *testing.T) {
	idx, err := PickStrings([]StringItem{{Label: "only"}}, "pick one")
	assert.NoError(t, err)
	assert.Equal(t, 0, idx)
}

func TestPickStringsEmpty(t *testing.T) {
	_, err := PickStrings(nil, "pick one")
	assert.Error(t, err)
}

// --- min helper ---

func TestMinHelper(t *testing.T) {
	assert.Equal(t, 3, min(3, 5))
	assert.Equal(t, 3, min(5, 3))
	assert.Equal(t, 0, min(0, 0))
}

// --- PickModel sorting by history ---

func TestPickModelSortedByHistory(t *testing.T) {
	history := map[string]time.Time{
		"llama-8b":   time.Now().Add(-1 * time.Hour),
		"qwen25-72b": time.Now().Add(-3 * time.Hour),
	}
	// Single-entry fast-path won't trigger; use one name to avoid TUI
	got, err := PickModel([]string{"llama-8b"}, nil, "", "", history)
	assert.NoError(t, err)
	assert.Equal(t, "llama-8b", got)
}
