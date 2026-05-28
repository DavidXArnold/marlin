package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DavidXArnold/marlin/internal/registry"
	"github.com/DavidXArnold/marlin/internal/sysinfo"
)

// SearchAction indicates what the user chose after selecting a search result.
type SearchAction int

const (
	SearchActionNone   SearchAction = iota
	SearchActionBrowse              // open model URL in browser
	SearchActionAdd                 // create a model profile
)

// searchResultItem wraps a ModelInfo for the picker list.
type searchResultItem struct {
	info registry.ModelInfo
	desc string
}

func newSearchResultItem(m registry.ModelInfo, freeVRAM uint64) searchResultItem {
	vram := "unknown"
	if mb := m.EstimatedVRAMMB(); mb > 0 {
		vram = sysinfo.FormatMB(mb)
	}
	fit := srFitLabel(m.EstimatedVRAMMB(), freeVRAM)
	upd := srFormatUpdated(m.LastUpdated)
	desc := fmt.Sprintf("[%s] VRAM: %s %s  %s", m.Registry, vram, fit, upd)
	return searchResultItem{info: m, desc: desc}
}

func (s searchResultItem) Title() string       { return s.info.ID }
func (s searchResultItem) Description() string { return s.desc }
func (s searchResultItem) FilterValue() string { return s.info.ID }

type searchDelegate struct{}

func (d searchDelegate) Height() int                             { return 2 }
func (d searchDelegate) Spacing() int                            { return 0 }
func (d searchDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d searchDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	sr, ok := item.(searchResultItem)
	if !ok {
		return
	}
	if index == m.Index() {
		_, _ = fmt.Fprint(w, selStyle.Render("> "+sr.info.ID))
		_, _ = fmt.Fprint(w, "\n"+selStyle.Render("  "+sr.desc))
	} else {
		_, _ = fmt.Fprint(w, itemStyle.Render(sr.info.ID))
		_, _ = fmt.Fprint(w, "\n"+dimStyle.Render(sr.desc))
	}
}

// searchPickerModel is the bubbletea model for the search result list.
type searchPickerModel struct {
	list     list.Model
	selected *registry.ModelInfo
	quitting bool
}

func (p searchPickerModel) Init() tea.Cmd { return nil }

func (p searchPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			p.quitting = true
			return p, tea.Quit
		case "enter":
			if item, ok := p.list.SelectedItem().(searchResultItem); ok {
				info := item.info
				p.selected = &info
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

func (p searchPickerModel) View() string {
	if p.quitting {
		return ""
	}
	return lipgloss.NewStyle().Margin(1, 2).Render(p.list.View())
}

// PickSearchResult shows an interactive list of search results and returns the
// selected model, or nil if the user cancelled. Returns an error only on failure.
func PickSearchResult(results []registry.ModelInfo, freeVRAM uint64) (*registry.ModelInfo, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no results to pick from")
	}

	items := make([]list.Item, len(results))
	for i, r := range results {
		items[i] = newSearchResultItem(r, freeVRAM)
	}

	l := list.New(items, searchDelegate{}, 80, 20)
	l.Title = "Select a model  (↑↓ navigate • / filter • enter select • q quit)"
	l.Styles.Title = titleStyle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)

	m, err := tea.NewProgram(searchPickerModel{list: l}, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, fmt.Errorf("search picker: %w", err)
	}

	pm := m.(searchPickerModel)
	if pm.quitting || pm.selected == nil {
		return nil, nil
	}
	return pm.selected, nil
}

// actionMenuModel is the bubbletea model for the post-selection action menu.
type actionMenuModel struct {
	modelID  string
	url      string
	options  []string
	cursor   int
	action   SearchAction
	quitting bool
}

func newActionMenuModel(modelID, url string) actionMenuModel {
	return actionMenuModel{
		modelID: modelID,
		url:     url,
		options: []string{"Open in browser", "Add as model profile", "Cancel"},
	}
}

func (a actionMenuModel) Init() tea.Cmd { return nil }

func (a actionMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			a.quitting = true
			return a, tea.Quit
		case "up", "k":
			if a.cursor > 0 {
				a.cursor--
			}
		case "down", "j":
			if a.cursor < len(a.options)-1 {
				a.cursor++
			}
		case "enter", " ":
			switch a.cursor {
			case 0:
				a.action = SearchActionBrowse
			case 1:
				a.action = SearchActionAdd
			default:
				a.quitting = true
			}
			return a, tea.Quit
		}
	}
	return a, nil
}

func (a actionMenuModel) View() string {
	if a.quitting {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(a.modelID) + "\n")
	if a.url != "" {
		sb.WriteString(dimStyle.Render(a.url) + "\n")
	}
	sb.WriteString("\n")
	for i, opt := range a.options {
		if i == a.cursor {
			sb.WriteString(selStyle.Render("> "+opt) + "\n")
		} else {
			sb.WriteString(itemStyle.Render("  "+opt) + "\n")
		}
	}
	sb.WriteString("\n" + dimStyle.Render("↑↓ navigate • enter select • q cancel"))
	return lipgloss.NewStyle().Margin(1, 2).Render(sb.String())
}

// SearchActionMenu shows a post-selection menu for the given model and returns
// the user's chosen action. Returns SearchActionNone if cancelled.
func SearchActionMenu(modelID, url string) (SearchAction, error) {
	m := newActionMenuModel(modelID, url)
	result, err := tea.NewProgram(m).Run()
	if err != nil {
		return SearchActionNone, fmt.Errorf("action menu: %w", err)
	}
	am := result.(actionMenuModel)
	if am.quitting {
		return SearchActionNone, nil
	}
	return am.action, nil
}

// ModelURL returns the canonical web URL for a registry model.
func ModelURL(m registry.ModelInfo) string {
	switch m.Registry {
	case "huggingface":
		return "https://huggingface.co/" + m.ID
	case "ngc":
		// NGC catalog URL — strip any tag suffix from the image path
		return "https://catalog.ngc.nvidia.com/"
	}
	return ""
}

func srFitLabel(estimatedMB, freeVRAMMB uint64) string {
	if estimatedMB == 0 || freeVRAMMB == 0 {
		return "?"
	}
	ratio := float64(estimatedMB) / float64(freeVRAMMB)
	switch {
	case ratio <= 0.80:
		return "✓"
	case ratio <= 1.0:
		return "~"
	default:
		return "✗"
	}
}

func srFormatUpdated(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	days := int(time.Since(t).Hours() / 24)
	switch {
	case days == 0:
		return "today"
	case days < 7:
		return fmt.Sprintf("%dd ago", days)
	case days < 30:
		return fmt.Sprintf("%dw ago", days/7)
	case days < 365:
		return fmt.Sprintf("%dmo ago", days/30)
	default:
		return fmt.Sprintf("%dy ago", days/365)
	}
}
