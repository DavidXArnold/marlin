package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DavidXArnold/marlin/internal/config"
)

// WizardResult is returned when the add wizard completes successfully.
type WizardResult struct {
	Slug string
	Cfg  *config.ModelConfig
}

type wizardStep int

const (
	stepProviderType wizardStep = iota
	stepModelID
	stepImage
	stepSlug
	stepQuantization
	stepGPUMem
	stepMaxLen
	stepServedNames
	stepToolParser
	stepNotes
	stepConfirmWizard
	stepDone
)

type wizardModel struct {
	step         wizardStep
	providerType config.ProviderType
	typeList     list.Model
	inputs       map[wizardStep]textinput.Model
	err          string
	result       *WizardResult
	quitting     bool
}

type typeItem struct{ label, desc string }

func (t typeItem) Title() string       { return t.label }
func (t typeItem) Description() string { return t.desc }
func (t typeItem) FilterValue() string { return t.label }

func newWizard() wizardModel {
	typeItems := []list.Item{
		typeItem{"vllm", "HuggingFace / NGC model loaded by vLLM (systemd)"},
		typeItem{"nim", "NVIDIA NIM container (Docker, TensorRT-LLM)"},
	}
	typeList := list.New(typeItems, list.NewDefaultDelegate(), 60, 6)
	typeList.Title = "Provider type"
	typeList.SetShowStatusBar(false)
	typeList.SetFilteringEnabled(false)

	mkInput := func(placeholder string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = 256
		return ti
	}

	inputs := map[wizardStep]textinput.Model{
		stepModelID:      mkInput("Qwen/Qwen2.5-72B-Instruct-AWQ"),
		stepImage:        mkInput("nvcr.io/nim/meta/llama-3.1-8b-instruct:latest"),
		stepSlug:         mkInput("qwen25-72b"),
		stepQuantization: mkInput("awq_marlin  (leave blank to omit)"),
		stepGPUMem:       mkInput("0.90"),
		stepMaxLen:       mkInput("0  (0 = auto)"),
		stepServedNames:  mkInput("gn100  (comma-separated aliases)"),
		stepToolParser:   mkInput("hermes  (leave blank to omit)"),
		stepNotes:        mkInput("optional notes"),
	}

	return wizardModel{
		step:     stepProviderType,
		typeList: typeList,
		inputs:   inputs,
	}
}

func (w wizardModel) Init() tea.Cmd {
	return nil
}

func (w wizardModel) currentPrompt() string {
	switch w.step {
	case stepProviderType:
		return "Select provider type (↑↓ to move, enter to select):"
	case stepModelID:
		return "HuggingFace / NGC model ID:"
	case stepImage:
		return "NIM container image:"
	case stepSlug:
		return "Local slug (filename, no spaces):"
	case stepQuantization:
		return "Quantization (awq_marlin, fp8, gptq…):"
	case stepGPUMem:
		return "GPU memory utilization (0.0–1.0):"
	case stepMaxLen:
		return "Max model length (0 = auto):"
	case stepServedNames:
		return "Served model names (comma-separated):"
	case stepToolParser:
		return "Tool call parser (hermes, llama3_json…):"
	case stepNotes:
		return "Notes (optional):"
	case stepConfirmWizard:
		return fmt.Sprintf("Save as %q? [y/n]", w.slugValue())
	}
	return ""
}

func (w wizardModel) slugValue() string {
	if v := w.inputs[stepSlug].Value(); v != "" {
		return v
	}
	return "model"
}

func (w *wizardModel) focusCurrent() {
	if ti, ok := w.inputs[w.step]; ok {
		ti.Focus()
		w.inputs[w.step] = ti
	}
}

func (w wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			w.quitting = true
			return w, tea.Quit

		case "enter":
			return w.advance()

		case "backspace":
			// handled by textinput below

		default:
			// auto-fill slug from model ID as user types
			if w.step == stepModelID {
				var cmd tea.Cmd
				ti := w.inputs[stepModelID]
				ti, cmd = ti.Update(msg)
				w.inputs[stepModelID] = ti
				slug := w.inputs[stepSlug]
				if slug.Value() == "" || slug.Value() == autoSlug(ti.Value()[:max(0, len(ti.Value())-1)]) {
					slug.SetValue(autoSlug(ti.Value()))
					w.inputs[stepSlug] = slug
				}
				return w, cmd
			}
		}
	}

	// Delegate to active component.
	if _, ok := w.inputs[w.step]; ok {
		var cmd tea.Cmd
		ti := w.inputs[w.step]
		ti, cmd = ti.Update(msg)
		w.inputs[w.step] = ti
		return w, cmd
	}
	if w.step == stepProviderType {
		var cmd tea.Cmd
		w.typeList, cmd = w.typeList.Update(msg)
		return w, cmd
	}

	return w, nil
}

func (w wizardModel) advance() (tea.Model, tea.Cmd) {
	switch w.step {
	case stepProviderType:
		if item, ok := w.typeList.SelectedItem().(typeItem); ok {
			w.providerType = config.ProviderType(item.label)
		}
		if w.providerType == config.ProviderNIM {
			w.step = stepImage
		} else {
			w.step = stepModelID
		}
		w.focusCurrent()

	case stepImage:
		if w.inputs[stepImage].Value() == "" {
			w.err = "image is required"
			return w, nil
		}
		// auto-suggest slug from image last segment
		slug := w.inputs[stepSlug]
		if slug.Value() == "" {
			slug.SetValue(autoSlug(w.inputs[stepImage].Value()))
			w.inputs[stepSlug] = slug
		}
		w.step = stepSlug
		w.focusCurrent()

	case stepModelID:
		if w.inputs[stepModelID].Value() == "" {
			w.err = "model ID is required"
			return w, nil
		}
		w.step = stepSlug
		w.focusCurrent()

	case stepSlug:
		if w.inputs[stepSlug].Value() == "" {
			w.err = "slug is required"
			return w, nil
		}
		w.err = ""
		if w.providerType == config.ProviderNIM {
			w.step = stepNotes
		} else {
			w.step = stepQuantization
		}
		w.focusCurrent()

	case stepQuantization:
		w.step = stepGPUMem
		w.focusCurrent()

	case stepGPUMem:
		if _, err := strconv.ParseFloat(w.inputs[stepGPUMem].Value(), 64); err != nil {
			w.err = "must be a decimal (e.g. 0.90)"
			return w, nil
		}
		w.err = ""
		w.step = stepMaxLen
		w.focusCurrent()

	case stepMaxLen:
		w.step = stepServedNames
		w.focusCurrent()

	case stepServedNames:
		w.step = stepToolParser
		w.focusCurrent()

	case stepToolParser:
		w.step = stepNotes
		w.focusCurrent()

	case stepNotes:
		w.step = stepConfirmWizard

	case stepConfirmWizard:
		w.step = stepDone
		w.result = w.buildResult()
		return w, tea.Quit
	}

	return w, nil
}

func (w wizardModel) View() string {
	if w.quitting {
		return ""
	}
	if w.step == stepDone {
		return ""
	}

	style := lipgloss.NewStyle().Margin(1, 2)
	prompt := titleStyle.Render(w.currentPrompt())

	var body string
	if w.step == stepProviderType {
		body = w.typeList.View()
	} else if w.step == stepConfirmWizard {
		body = ""
	} else if ti, ok := w.inputs[w.step]; ok {
		body = ti.View()
	}

	errLine := ""
	if w.err != "" {
		errLine = "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗ "+w.err)
	}

	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("\n  esc to cancel • enter to continue")

	return style.Render(prompt + "\n\n" + body + errLine + hint)
}

func (w wizardModel) buildResult() *WizardResult {
	slug := w.inputs[stepSlug].Value()

	gpuMem, _ := strconv.ParseFloat(strings.TrimSpace(w.inputs[stepGPUMem].Value()), 64)
	maxLen, _ := strconv.Atoi(strings.TrimSpace(w.inputs[stepMaxLen].Value()))

	var servedNames []string
	for _, s := range strings.Split(w.inputs[stepServedNames].Value(), ",") {
		if t := strings.TrimSpace(s); t != "" {
			servedNames = append(servedNames, t)
		}
	}

	m := &config.ModelConfig{
		Model: config.ModelMeta{
			Type:   w.providerType,
			ID:     strings.TrimSpace(w.inputs[stepModelID].Value()),
			Image:  strings.TrimSpace(w.inputs[stepImage].Value()),
			Status: config.StatusUntested,
			Notes:  strings.TrimSpace(w.inputs[stepNotes].Value()),
		},
	}

	if w.providerType == config.ProviderVLLM {
		m.Serve = config.ServeConfig{
			Quantization:         strings.TrimSpace(w.inputs[stepQuantization].Value()),
			GPUMemoryUtilization: gpuMem,
			MaxModelLen:          maxLen,
			ServedModelName:      servedNames,
			ToolCallParser:       strings.TrimSpace(w.inputs[stepToolParser].Value()),
		}
	}

	return &WizardResult{Slug: slug, Cfg: m}
}

// RunAddWizard launches the interactive add wizard and returns the result.
func RunAddWizard() (*WizardResult, error) {
	wiz := newWizard()
	wiz.focusCurrent()

	m, err := tea.NewProgram(wiz, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, fmt.Errorf("wizard: %w", err)
	}

	wm := m.(wizardModel)
	if wm.quitting || wm.result == nil {
		return nil, fmt.Errorf("cancelled")
	}
	return wm.result, nil
}

// autoSlug derives a filesystem-safe slug from a model ID or image path.
func autoSlug(input string) string {
	// Use last path segment, strip tag
	base := input
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			base = base[i+1:]
			break
		}
	}
	for i := range base {
		if base[i] == ':' {
			base = base[:i]
			break
		}
	}
	// lowercase, replace non-alnum with dash
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
