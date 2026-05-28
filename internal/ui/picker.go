package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/DavidXArnold/marlin/internal/config"
)

var (
	titleStyle = lipgloss.NewStyle().MarginLeft(2).Bold(true)
	itemStyle  = lipgloss.NewStyle().PaddingLeft(4)
	selStyle   = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	dimStyle   = lipgloss.NewStyle().PaddingLeft(4).Foreground(lipgloss.Color("241"))
)

// modelItem is a single entry in the model picker list.
type modelItem struct {
	slug     string
	provider string
	status   string
}

func (m modelItem) Title() string       { return m.slug }
func (m modelItem) Description() string { return fmt.Sprintf("%s • %s", m.provider, m.status) }
func (m modelItem) FilterValue() string { return m.slug }

// itemDelegate renders list items with consistent styling.
type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 2 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(modelItem)
	if !ok {
		return
	}
	if index == m.Index() {
		_, _ = fmt.Fprint(w, selStyle.Render("> "+item.slug))
		_, _ = fmt.Fprint(w, "\n"+selStyle.Render("  "+item.Description()))
	} else {
		_, _ = fmt.Fprint(w, itemStyle.Render(item.slug))
		_, _ = fmt.Fprint(w, "\n"+dimStyle.Render(item.Description()))
	}
}

// pickerModel is the bubbletea model for the interactive model picker.
type pickerModel struct {
	list     list.Model
	selected string
	quitting bool
}

func (p pickerModel) Init() tea.Cmd { return nil }

func (p pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			p.quitting = true
			return p, tea.Quit
		case "enter":
			if item, ok := p.list.SelectedItem().(modelItem); ok {
				p.selected = item.slug
			}
			return p, tea.Quit
		}
	case tea.WindowSizeMsg:
		p.list.SetWidth(msg.Width)
		p.list.SetHeight(msg.Height - 4)
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p pickerModel) View() string {
	if p.quitting {
		return ""
	}
	return lipgloss.NewStyle().Margin(1, 2).Render(p.list.View())
}

// FuzzyMatch returns the slugs from names that best match query, ranked by score.
// If query exactly matches a slug it is returned alone.
func FuzzyMatch(query string, names []string) []string {
	if query == "" {
		return names
	}
	matches := fuzzy.Find(query, names)
	if len(matches) == 0 {
		return nil
	}
	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.Str
	}
	return result
}

// PickModel presents an interactive fuzzy-searchable list of model slugs and
// returns the user's selection. If names has exactly one entry it is returned
// directly. prefilter is an optional initial search string.
func PickModel(names []string, cfgs []*config.ModelConfig, prefilter string) (string, error) {
	if len(names) == 0 {
		return "", fmt.Errorf("no models found — run 'marlin add' to create one")
	}
	if len(names) == 1 {
		return names[0], nil
	}

	items := make([]list.Item, len(names))
	for i, slug := range names {
		item := modelItem{slug: slug, provider: "vllm", status: "untested"}
		if i < len(cfgs) && cfgs[i] != nil {
			item.provider = string(cfgs[i].Model.Type)
			item.status = string(cfgs[i].Model.Status)
		}
		items[i] = item
	}

	l := list.New(items, itemDelegate{}, 60, 20)
	l.Title = "Select a model"
	l.Styles.Title = titleStyle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	if prefilter != "" {
		l.SetFilterText(prefilter)
	}

	m, err := tea.NewProgram(pickerModel{list: l}, tea.WithAltScreen()).Run()
	if err != nil {
		return "", fmt.Errorf("picker: %w", err)
	}

	pm := m.(pickerModel)
	if pm.quitting || pm.selected == "" {
		return "", fmt.Errorf("cancelled")
	}
	return pm.selected, nil
}

// Confirm shows a simple y/n prompt and returns true if the user confirmed.
func Confirm(prompt string) (bool, error) {
	m, err := tea.NewProgram(confirmModel{prompt: prompt}).Run()
	if err != nil {
		return false, err
	}
	return m.(confirmModel).confirmed, nil
}

type confirmModel struct {
	prompt    string
	confirmed bool
	done      bool
}

func (c confirmModel) Init() tea.Cmd { return nil }

func (c confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch strings.ToLower(key.String()) {
		case "y":
			c.confirmed = true
			c.done = true
			return c, tea.Quit
		case "n", "ctrl+c", "esc":
			c.confirmed = false
			c.done = true
			return c, tea.Quit
		}
	}
	return c, nil
}

func (c confirmModel) View() string {
	if c.done {
		return ""
	}
	return lipgloss.NewStyle().Margin(1, 2).Render(c.prompt + " [y/n] ")
}
