package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func fakeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Alt: false}
}

func TestFuzzyMatchExact(t *testing.T) {
	names := []string{"qwen25-72b", "llama-3.1-8b", "mistral-7b"}
	got := FuzzyMatch("qwen25-72b", names)
	assert.Equal(t, []string{"qwen25-72b"}, got)
}

func TestFuzzyMatchPartial(t *testing.T) {
	names := []string{"qwen25-72b", "llama-3.1-8b", "mistral-7b"}
	got := FuzzyMatch("llama", names)
	assert.Contains(t, got, "llama-3.1-8b")
}

func TestFuzzyMatchEmpty(t *testing.T) {
	names := []string{"a", "b", "c"}
	got := FuzzyMatch("", names)
	assert.Equal(t, names, got)
}

func TestFuzzyMatchNoMatch(t *testing.T) {
	names := []string{"qwen25-72b", "llama-3.1-8b"}
	got := FuzzyMatch("gpt4", names)
	assert.Nil(t, got)
}

func TestAutoSlugFromModelID(t *testing.T) {
	cases := []struct{ input, want string }{
		{"Qwen/Qwen2.5-72B-Instruct-AWQ", "qwen2.5-72b-instruct-awq"},
		{"meta-llama/Llama-3.1-8B-Instruct", "llama-3.1-8b-instruct"},
		{"nvcr.io/nim/meta/llama:latest", "llama"},
		{"simple", "simple"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, AutoSlug(c.input), c.input)
	}
}

func setInput(w *wizardModel, step wizardStep, val string) {
	ti := w.inputs[step]
	ti.SetValue(val)
	w.inputs[step] = ti
}

func TestWizardBuildResult(t *testing.T) {
	w := newWizard()
	w.providerType = "vllm"
	setInput(&w, stepModelID, "Qwen/Qwen2.5-72B-Instruct-AWQ")
	setInput(&w, stepSlug, "qwen25-72b")
	setInput(&w, stepGPUMem, "0.90")
	setInput(&w, stepMaxLen, "131072")
	setInput(&w, stepServedNames, "gn100, qwen")
	setInput(&w, stepQuantization, "awq_marlin")
	setInput(&w, stepToolParser, "hermes")

	result := w.buildResult()
	assert.Equal(t, "qwen25-72b", result.Slug)
	assert.Equal(t, "Qwen/Qwen2.5-72B-Instruct-AWQ", result.Cfg.Model.ID)
	assert.Equal(t, 0.90, result.Cfg.Serve.GPUMemoryUtilization)
	assert.Equal(t, 131072, result.Cfg.Serve.MaxModelLen)
	assert.Equal(t, []string{"gn100", "qwen"}, result.Cfg.Serve.ServedModelName)
}

func TestWizardBuildResultNIM(t *testing.T) {
	w := newWizard()
	w.providerType = "nim"
	setInput(&w, stepImage, "nvcr.io/nim/meta/llama:latest")
	setInput(&w, stepSlug, "llama-nim")

	result := w.buildResult()
	assert.Equal(t, "llama-nim", result.Slug)
	assert.Equal(t, "nvcr.io/nim/meta/llama:latest", result.Cfg.Model.Image)
	assert.Empty(t, result.Cfg.Serve.Quantization)
}

func TestConfirmModelYes(t *testing.T) {
	// Test the model logic directly without launching a real terminal.
	m := confirmModel{prompt: "continue?"}
	updated, _ := m.Update(fakeKey("y"))
	cm := updated.(confirmModel)
	assert.True(t, cm.confirmed)
	assert.True(t, cm.done)
}

func TestConfirmModelNo(t *testing.T) {
	m := confirmModel{prompt: "continue?"}
	updated, _ := m.Update(fakeKey("n"))
	cm := updated.(confirmModel)
	assert.False(t, cm.confirmed)
	assert.True(t, cm.done)
}

func TestConfirmModelEsc(t *testing.T) {
	m := confirmModel{prompt: "continue?"}
	updated, _ := m.Update(fakeKey("esc"))
	cm := updated.(confirmModel)
	assert.False(t, cm.confirmed)
}

func TestPickModelSingleReturnsDirectly(t *testing.T) {
	result, err := PickModel([]string{"only-model"}, nil, "", "")
	assert.NoError(t, err)
	assert.Equal(t, "only-model", result)
}

func TestPickModelEmpty(t *testing.T) {
	_, err := PickModel([]string{}, nil, "", "")
	assert.Error(t, err)
}

func TestMaxHelper(t *testing.T) {
	assert.Equal(t, 5, max(3, 5))
	assert.Equal(t, 5, max(5, 3))
	assert.Equal(t, 0, max(0, 0))
}
