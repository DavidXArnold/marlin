package ui

import (
	"bytes"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/DavidXArnold/marlin/internal/registry"
)

func fakeResult(id, reg string) registry.ModelInfo {
	return registry.ModelInfo{ID: id, Registry: reg, ParamsBillion: 7, Quantization: "awq"}
}

func makeSearchPicker(results []registry.ModelInfo) searchPickerModel {
	items := make([]list.Item, len(results))
	for i, r := range results {
		items[i] = newSearchResultItem(r, 0)
	}
	l := list.New(items, searchDelegate{}, 80, 20)
	return searchPickerModel{list: l}
}

// --- searchResultItem ---

func TestSearchResultItemFields(t *testing.T) {
	m := fakeResult("Qwen/Qwen2.5-7B", "huggingface")
	item := newSearchResultItem(m, 0)
	assert.Equal(t, "Qwen/Qwen2.5-7B", item.Title())
	assert.Equal(t, "Qwen/Qwen2.5-7B", item.FilterValue())
	assert.Contains(t, item.Description(), "huggingface")
	assert.Contains(t, item.Description(), "?") // freeVRAM=0 → unknown fit
}

func TestSearchResultItemFitLabels(t *testing.T) {
	m := fakeResult("x", "huggingface")
	est := m.EstimatedVRAMMB() // AWQ 7B ≈ 4,005 MiB

	comfortable := newSearchResultItem(m, est*2)
	assert.Contains(t, comfortable.Description(), "✓")

	tight := newSearchResultItem(m, uint64(float64(est)*1.05))
	assert.Contains(t, tight.Description(), "~")

	over := newSearchResultItem(m, est/2)
	assert.Contains(t, over.Description(), "✗")
}

// --- searchPickerModel ---

func TestSearchPickerQuit(t *testing.T) {
	pm := makeSearchPicker([]registry.ModelInfo{fakeResult("A", "huggingface")})
	updated, _ := pm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	assert.True(t, updated.(searchPickerModel).quitting)
}

func TestSearchPickerEsc(t *testing.T) {
	pm := makeSearchPicker([]registry.ModelInfo{fakeResult("A", "huggingface")})
	updated, _ := pm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, updated.(searchPickerModel).quitting)
}

func TestSearchPickerViewWhenQuitting(t *testing.T) {
	pm := searchPickerModel{quitting: true}
	assert.Equal(t, "", pm.View())
}

func TestPickSearchResultEmpty(t *testing.T) {
	_, err := PickSearchResult(nil, 0)
	assert.Error(t, err)
}

// --- actionMenuModel ---

func newTestActionMenu() actionMenuModel {
	return newActionMenuModel("mymodel", "https://example.com")
}

func TestActionMenuQuit(t *testing.T) {
	am := newTestActionMenu()
	updated, _ := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	result := updated.(actionMenuModel)
	assert.True(t, result.quitting)
	assert.Equal(t, SearchActionNone, result.action)
}

func TestActionMenuEsc(t *testing.T) {
	am := newTestActionMenu()
	updated, _ := am.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, updated.(actionMenuModel).quitting)
}

func TestActionMenuNavigate(t *testing.T) {
	am := newTestActionMenu()
	u1, _ := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 1, u1.(actionMenuModel).cursor)

	u2, _ := u1.(actionMenuModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 2, u2.(actionMenuModel).cursor)

	// can't go past last option
	u3, _ := u2.(actionMenuModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 2, u3.(actionMenuModel).cursor)

	// move back up
	u4, _ := u3.(actionMenuModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	assert.Equal(t, 1, u4.(actionMenuModel).cursor)
}

func TestActionMenuSelectBrowse(t *testing.T) {
	am := newTestActionMenu() // cursor=0 → Browse
	updated, _ := am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, SearchActionBrowse, updated.(actionMenuModel).action)
}

func TestActionMenuSelectAdd(t *testing.T) {
	am := newTestActionMenu()
	u1, _ := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // cursor→1
	updated, _ := u1.(actionMenuModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, SearchActionAdd, updated.(actionMenuModel).action)
}

func TestActionMenuSelectCancel(t *testing.T) {
	am := newTestActionMenu()
	u1, _ := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	u2, _ := u1.(actionMenuModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // cursor→2
	updated, _ := u2.(actionMenuModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	result := updated.(actionMenuModel)
	assert.Equal(t, SearchActionNone, result.action)
	assert.True(t, result.quitting)
}

func TestActionMenuViewWhenQuitting(t *testing.T) {
	am := actionMenuModel{quitting: true}
	assert.Equal(t, "", am.View())
}

// --- ModelURL ---

func TestModelURL(t *testing.T) {
	hf := registry.ModelInfo{ID: "Qwen/Qwen2.5-72B", Registry: "huggingface"}
	assert.Equal(t, "https://huggingface.co/Qwen/Qwen2.5-72B", ModelURL(hf))

	ngc := registry.ModelInfo{ID: "nim/meta/llama", Registry: "ngc"}
	assert.Contains(t, ModelURL(ngc), "ngc.nvidia.com")

	unknown := registry.ModelInfo{ID: "x", Registry: "other"}
	assert.Equal(t, "", ModelURL(unknown))
}

// --- srFormatUpdated ---

func TestSrFormatUpdated(t *testing.T) {
	assert.Equal(t, "unknown", srFormatUpdated(time.Time{}))
	assert.Equal(t, "today", srFormatUpdated(time.Now()))
	assert.Contains(t, srFormatUpdated(time.Now().AddDate(0, 0, -3)), "d ago")
	assert.Contains(t, srFormatUpdated(time.Now().AddDate(0, 0, -14)), "w ago")
	assert.Contains(t, srFormatUpdated(time.Now().AddDate(0, -2, 0)), "mo ago")
	assert.Contains(t, srFormatUpdated(time.Now().AddDate(-2, 0, 0)), "y ago")
}

// --- srFitLabel ---

func TestSrFitLabel(t *testing.T) {
	assert.Equal(t, "?", srFitLabel(0, 1000))
	assert.Equal(t, "?", srFitLabel(1000, 0))
	assert.Equal(t, "✓", srFitLabel(800, 1000))
	assert.Equal(t, "~", srFitLabel(900, 1000))
	assert.Equal(t, "✗", srFitLabel(1100, 1000))
}

// --- Init methods ---

func TestSearchPickerInit(t *testing.T) {
	pm := makeSearchPicker(nil)
	assert.Nil(t, pm.Init())
}

func TestActionMenuInit(t *testing.T) {
	am := newTestActionMenu()
	assert.Nil(t, am.Init())
}

// --- View methods (non-quitting) ---

func TestSearchPickerViewNonQuitting(t *testing.T) {
	pm := makeSearchPicker([]registry.ModelInfo{fakeResult("A", "huggingface")})
	view := pm.View()
	assert.NotEmpty(t, view)
}

func TestActionMenuViewNonQuitting(t *testing.T) {
	am := newTestActionMenu()
	view := am.View()
	assert.Contains(t, view, "mymodel")
	assert.Contains(t, view, "Open in browser")
}

// --- searchDelegate ---

func TestSearchDelegateHeightSpacing(t *testing.T) {
	d := searchDelegate{}
	assert.Equal(t, 2, d.Height())
	assert.Equal(t, 0, d.Spacing())
}

func TestSearchDelegateUpdate(t *testing.T) {
	d := searchDelegate{}
	cmd := d.Update(nil, nil)
	assert.Nil(t, cmd)
}

func TestSearchDelegateRenderSelected(t *testing.T) {
	items := []list.Item{newSearchResultItem(fakeResult("model-a", "huggingface"), 0)}
	l := list.New(items, searchDelegate{}, 80, 10)
	var buf bytes.Buffer
	searchDelegate{}.Render(&buf, l, 0, items[0])
	assert.Contains(t, buf.String(), "model-a")
}

func TestSearchDelegateRenderNonSelected(t *testing.T) {
	r1 := newSearchResultItem(fakeResult("model-a", "huggingface"), 0)
	r2 := newSearchResultItem(fakeResult("model-b", "huggingface"), 0)
	items := []list.Item{r1, r2}
	l := list.New(items, searchDelegate{}, 80, 10)
	var buf bytes.Buffer
	// render index 1 while selected index is 0 → non-selected path
	searchDelegate{}.Render(&buf, l, 1, items[1])
	assert.Contains(t, buf.String(), "model-b")
}

func TestSearchDelegateRenderBadType(t *testing.T) {
	items := []list.Item{newSearchResultItem(fakeResult("x", "huggingface"), 0)}
	l := list.New(items, searchDelegate{}, 80, 10)
	var buf bytes.Buffer
	// pass a wrong item type — should produce no output
	searchDelegate{}.Render(&buf, l, 0, modelItem{slug: "wrong"})
	assert.Empty(t, buf.String())
}

// --- WindowSizeMsg ---

func TestSearchPickerWindowSize(t *testing.T) {
	pm := makeSearchPicker([]registry.ModelInfo{fakeResult("A", "huggingface")})
	updated, _ := pm.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	_ = updated.(searchPickerModel) // should not panic
}
