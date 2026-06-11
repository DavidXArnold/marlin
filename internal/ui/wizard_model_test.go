package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

// --- typeItem ---

func TestTypeItemMethods(t *testing.T) {
	item := typeItem{label: "vllm", desc: "HuggingFace model"}
	assert.Equal(t, "vllm", item.Title())
	assert.Equal(t, "HuggingFace model", item.Description())
	assert.Equal(t, "vllm", item.FilterValue())
}

// --- wizardModel basics ---

func TestWizardInit(t *testing.T) {
	w := newWizard()
	assert.Nil(t, w.Init())
}

func TestWizardSlugValue(t *testing.T) {
	w := newWizard()
	assert.Equal(t, "model", w.slugValue()) // empty input → default "model"

	setInput(&w, stepSlug, "my-slug")
	assert.Equal(t, "my-slug", w.slugValue())
}

func TestWizardFocusCurrent(t *testing.T) {
	w := newWizard()
	w.step = stepModelID
	w.focusCurrent() // should not panic
	assert.True(t, w.inputs[stepModelID].Focused())
}

func TestWizardCurrentPromptAllSteps(t *testing.T) {
	w := newWizard()
	steps := []wizardStep{
		stepProviderType, stepModelID, stepImage, stepSlug,
		stepQuantization, stepGPUMem, stepMaxLen, stepServedNames,
		stepToolParser, stepNotes, stepConfirmWizard,
	}
	for _, s := range steps {
		w.step = s
		assert.NotEmpty(t, w.currentPrompt(), "step %d should have a prompt", s)
	}
}

func TestWizardCurrentPromptDone(t *testing.T) {
	w := newWizard()
	w.step = stepDone
	assert.Empty(t, w.currentPrompt())
}

// --- View ---

func TestWizardViewQuitting(t *testing.T) {
	w := newWizard()
	w.quitting = true
	assert.Empty(t, w.View())
}

func TestWizardViewDone(t *testing.T) {
	w := newWizard()
	w.step = stepDone
	assert.Empty(t, w.View())
}

func TestWizardViewProviderType(t *testing.T) {
	w := newWizard()
	w.step = stepProviderType
	assert.NotEmpty(t, w.View())
}

func TestWizardViewTextInput(t *testing.T) {
	w := newWizard()
	w.step = stepModelID
	assert.NotEmpty(t, w.View())
}

func TestWizardViewConfirm(t *testing.T) {
	w := newWizard()
	w.step = stepConfirmWizard
	assert.NotEmpty(t, w.View())
}

func TestWizardViewWithError(t *testing.T) {
	w := newWizard()
	w.step = stepModelID
	w.err = "something went wrong"
	v := w.View()
	assert.Contains(t, v, "something went wrong")
}

// --- Update / advance ---

func TestWizardUpdateCtrlC(t *testing.T) {
	w := newWizard()
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.True(t, updated.(wizardModel).quitting)
}

func TestWizardUpdateEsc(t *testing.T) {
	w := newWizard()
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, updated.(wizardModel).quitting)
}

func TestWizardAdvanceProviderVLLM(t *testing.T) {
	w := newWizard()
	w.step = stepProviderType
	// typeList has vllm as first item; advance should move to stepModelID
	updated, _ := w.advance()
	assert.Equal(t, stepModelID, updated.(wizardModel).step)
}

func TestWizardAdvanceModelIDEmpty(t *testing.T) {
	w := newWizard()
	w.step = stepModelID
	updated, _ := w.advance()
	wm := updated.(wizardModel)
	assert.Equal(t, "model ID is required", wm.err)
	assert.Equal(t, stepModelID, wm.step)
}

func TestWizardAdvanceModelIDFilled(t *testing.T) {
	w := newWizard()
	w.step = stepModelID
	setInput(&w, stepModelID, "Qwen/Qwen2.5-72B")
	updated, _ := w.advance()
	assert.Equal(t, stepSlug, updated.(wizardModel).step)
}

func TestWizardAdvanceImageEmpty(t *testing.T) {
	w := newWizard()
	w.step = stepImage
	updated, _ := w.advance()
	assert.Equal(t, "image is required", updated.(wizardModel).err)
}

func TestWizardAdvanceImageFilled(t *testing.T) {
	w := newWizard()
	w.step = stepImage
	setInput(&w, stepImage, "nvcr.io/nim/meta/llama:latest")
	updated, _ := w.advance()
	assert.Equal(t, stepSlug, updated.(wizardModel).step)
}

func TestWizardAdvanceSlugEmpty(t *testing.T) {
	w := newWizard()
	w.step = stepSlug
	// clear the auto-filled slug
	setInput(&w, stepSlug, "")
	updated, _ := w.advance()
	assert.Equal(t, "slug is required", updated.(wizardModel).err)
}

func TestWizardAdvanceSlugVLLM(t *testing.T) {
	w := newWizard()
	w.step = stepSlug
	w.providerType = "vllm"
	setInput(&w, stepSlug, "my-model")
	updated, _ := w.advance()
	assert.Equal(t, stepQuantization, updated.(wizardModel).step)
}

func TestWizardAdvanceSlugNIM(t *testing.T) {
	w := newWizard()
	w.step = stepSlug
	w.providerType = "nim"
	setInput(&w, stepSlug, "my-nim")
	updated, _ := w.advance()
	assert.Equal(t, stepExtraEnv, updated.(wizardModel).step)
}

func TestWizardAdvanceQuantization(t *testing.T) {
	w := newWizard()
	w.step = stepQuantization
	updated, _ := w.advance()
	assert.Equal(t, stepGPUMem, updated.(wizardModel).step)
}

func TestWizardAdvanceGPUMemBad(t *testing.T) {
	w := newWizard()
	w.step = stepGPUMem
	setInput(&w, stepGPUMem, "notanumber")
	updated, _ := w.advance()
	assert.Equal(t, "must be a decimal (e.g. 0.90)", updated.(wizardModel).err)
}

func TestWizardAdvanceGPUMemGood(t *testing.T) {
	w := newWizard()
	w.step = stepGPUMem
	setInput(&w, stepGPUMem, "0.90")
	updated, _ := w.advance()
	assert.Equal(t, stepMaxLen, updated.(wizardModel).step)
}

func TestWizardAdvanceMaxLen(t *testing.T) {
	w := newWizard()
	w.step = stepMaxLen
	updated, _ := w.advance()
	assert.Equal(t, stepServedNames, updated.(wizardModel).step)
}

func TestWizardAdvanceServedNames(t *testing.T) {
	w := newWizard()
	w.step = stepServedNames
	updated, _ := w.advance()
	assert.Equal(t, stepToolParser, updated.(wizardModel).step)
}

func TestWizardAdvanceToolParser(t *testing.T) {
	w := newWizard()
	w.step = stepToolParser
	updated, _ := w.advance()
	assert.Equal(t, stepNotes, updated.(wizardModel).step)
}

func TestWizardAdvanceNotes(t *testing.T) {
	w := newWizard()
	w.step = stepNotes
	updated, _ := w.advance()
	assert.Equal(t, stepConfirmWizard, updated.(wizardModel).step)
}

func TestWizardAdvanceConfirm(t *testing.T) {
	w := newWizard()
	w.step = stepConfirmWizard
	w.providerType = "vllm"
	setInput(&w, stepSlug, "my-model")
	setInput(&w, stepGPUMem, "0.90")
	updated, _ := w.advance()
	wm := updated.(wizardModel)
	assert.Equal(t, stepDone, wm.step)
	assert.NotNil(t, wm.result)
}

func TestWizardUpdateModelIDAutoSlug(t *testing.T) {
	w := newWizard()
	w.step = stepModelID

	// Simulate typing a character into stepModelID — should auto-fill slug.
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")})
	wm := updated.(wizardModel)
	// slug should have been auto-derived from the partial input
	_ = wm.inputs[stepSlug].Value() // just confirm it doesn't panic
}

func TestWizardUpdateEnterDelegatesToAdvance(t *testing.T) {
	w := newWizard()
	w.step = stepModelID
	setInput(&w, stepModelID, "SomeModel/ID")
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, stepSlug, updated.(wizardModel).step)
}

func TestWizardUpdateNonModelIDTextInput(t *testing.T) {
	w := newWizard()
	w.step = stepSlug
	setInput(&w, stepSlug, "")
	w.focusCurrent() // focus the input so it accepts keystrokes
	// Typing a character at stepSlug (not stepModelID) hits the textinput delegate path.
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	val := updated.(wizardModel).inputs[stepSlug].Value()
	assert.Contains(t, val, "m")
}

func TestWizardUpdateProviderTypeDelegate(t *testing.T) {
	w := newWizard()
	w.step = stepProviderType
	// Any non-special key at the provider type step should be forwarded to the list.
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, stepProviderType, updated.(wizardModel).step)
}

func TestWizardUpdateBackspace(t *testing.T) {
	w := newWizard()
	w.step = stepSlug
	setInput(&w, stepSlug, "abc")
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	_ = updated.(wizardModel).inputs[stepSlug].Value() // confirm no panic
}

func TestWizardAdvanceExtraEnv(t *testing.T) {
	w := newWizard()
	w.step = stepExtraEnv
	w.providerType = config.ProviderNIM
	updated, _ := w.advance()
	assert.Equal(t, stepNotes, updated.(wizardModel).step)
}

func TestWizardBuildResultNIMWithExtraEnv(t *testing.T) {
	w := newWizard()
	w.providerType = config.ProviderNIM
	setInput(&w, stepSlug, "nim-test")
	setInput(&w, stepImage, "nvcr.io/nim/meta/llama:latest")
	setInput(&w, stepExtraEnv, "FOO=bar, BAZ=qux, invalid, KEY=val")
	setInput(&w, stepGPUMem, "0.90")

	result := w.buildResult()
	require.NotNil(t, result)
	assert.Equal(t, "nim-test", result.Slug)
	assert.Equal(t, config.ProviderNIM, result.Cfg.Model.Type)
	assert.Equal(t, []string{"FOO=bar", "BAZ=qux", "KEY=val"}, result.Cfg.Serve.ExtraEnv)
}

func TestWizardBuildResultNIMEmptyExtraEnv(t *testing.T) {
	w := newWizard()
	w.providerType = config.ProviderNIM
	setInput(&w, stepSlug, "nim-test")
	setInput(&w, stepImage, "nvcr.io/nim/meta/llama:latest")
	setInput(&w, stepExtraEnv, "")
	setInput(&w, stepGPUMem, "0.90")

	result := w.buildResult()
	require.NotNil(t, result)
	assert.Nil(t, result.Cfg.Serve.ExtraEnv)
}

func TestAutoSlugNoSlash(t *testing.T) {
	assert.Equal(t, "llama", AutoSlug("llama"))
}

func TestAutoSlugWithColon(t *testing.T) {
	assert.Equal(t, "llama", AutoSlug("llama:latest"))
}

func TestWizardCurrentPromptExtraEnv(t *testing.T) {
	w := newWizard()
	w.step = stepExtraEnv
	assert.Contains(t, w.currentPrompt(), "Extra container env vars")
}
