// Package tui provides the interactive terminal surface for kaypoh.
package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gongahkia/kaypoh/internal/app"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/store"
)

const (
	tabDiscover = iota
	tabWatches
	tabSources
	tabEvents
	tabSettings
)

var tabNames = []string{"Discover", "Watches", "Sources", "Events", "Settings"}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	activeTab    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1)
	inactiveTab  = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	dividerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type model struct {
	service *app.Service
	ctx     context.Context
	width   int
	height  int
	tab     int
	sport   textinput.Model
	status  string
	busy    bool
	help    bool

	results []domain.SearchResult
	watches []domain.Watch
	sources []store.SourceRecord
	events  []domain.Event
	config  string
}

type dataMessage struct {
	results []domain.SearchResult
	watches []domain.Watch
	sources []store.SourceRecord
	events  []domain.Event
	err     error
}

type actionMessage struct {
	status string
	err    error
}

func Run(ctx context.Context, service *app.Service, input io.Reader, output io.Writer) error {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	initial := newModel(runContext, service)
	program := tea.NewProgram(initial, tea.WithInput(input), tea.WithOutput(output), tea.WithAltScreen())
	go func() {
		<-runContext.Done()
		program.Quit()
	}()
	_, err := program.Run()
	return err
}

func newModel(ctx context.Context, service *app.Service) model {
	sport := textinput.New()
	sport.Prompt = "Sport: "
	sport.SetValue("badminton")
	sport.CharLimit = 32
	sport.Width = 24
	return model{service: service, ctx: ctx, sport: sport, status: "Loading local state…"}
}

func (model model) Init() tea.Cmd {
	return model.load()
}

func (model model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		return model, nil
	case dataMessage:
		model.busy = false
		if message.err != nil {
			model.status = message.err.Error()
			return model, nil
		}
		model.results, model.watches, model.sources, model.events = message.results, message.watches, message.sources, message.events
		if len(model.results) == 0 {
			model.status = "No fresh availability matches. Press r to refresh permitted sources or add a local slot."
		} else {
			model.status = fmt.Sprintf("%d fresh match(es) for %s", len(model.results), model.sport.Value())
		}
		return model, nil
	case actionMessage:
		model.busy = false
		if message.err != nil {
			model.status = message.err.Error()
			return model, nil
		}
		model.status = message.status
		return model, model.load()
	case tea.KeyMsg:
		key := message.String()
		if model.tab == tabDiscover && model.sport.Focused() {
			switch key {
			case "ctrl+c":
				return model, tea.Quit
			case "esc":
				model.sport.Blur()
				return model, nil
			case "enter":
				model.sport.Blur()
				model.busy = true
				model.status = "Searching local availability…"
				return model, model.load()
			default:
				var command tea.Cmd
				model.sport, command = model.sport.Update(message)
				return model, command
			}
		}
		switch key {
		case "ctrl+c", "q":
			return model, tea.Quit
		case "tab", "right", "l":
			model.tab = (model.tab + 1) % len(tabNames)
			model.sport.Blur()
			return model, nil
		case "shift+tab", "left", "h":
			model.tab = (model.tab + len(tabNames) - 1) % len(tabNames)
			model.sport.Blur()
			return model, nil
		case "?":
			model.help = !model.help
			return model, nil
		case "r":
			model.busy = true
			model.status = "Refreshing permitted sources…"
			return model, model.refresh()
		case "e":
			if model.tab == tabWatches {
				model.busy = true
				model.status = "Evaluating watches…"
				return model, model.evaluate()
			}
		case "w":
			if model.tab == tabDiscover {
				model.busy = true
				return model, model.createWatch()
			}
		case "/":
			if model.tab == tabDiscover {
				model.sport.Focus()
			}
		case "enter":
			if model.tab == tabDiscover {
				model.busy = true
				model.status = "Searching local availability…"
				return model, model.load()
			}
		}
	}
	return model, nil
}

func (model model) View() string {
	if model.width == 0 {
		return "Loading kaypoh…"
	}
	contentWidth := max(30, model.width-4)
	header := titleStyle.Render("kaypoh") + mutedStyle.Render("  Singapore courts, local-first")
	tabs := make([]string, 0, len(tabNames))
	for index, name := range tabNames {
		if index == model.tab {
			tabs = append(tabs, activeTab.Render(name))
		} else {
			tabs = append(tabs, inactiveTab.Render(name))
		}
	}
	body := model.body(contentWidth)
	status := statusStyle.Render(model.status)
	if strings.Contains(strings.ToLower(model.status), "error") || strings.Contains(strings.ToLower(model.status), "unavailable") {
		status = warningStyle.Render(model.status)
	}
	if model.busy {
		status = statusStyle.Render("Working… " + model.status)
	}
	footer := mutedStyle.Render("tab switch  / edit sport  enter search  r refresh  w watch  e evaluate  ? help  q quit")
	if model.help {
		footer = headerStyle.Render("Discover searches only local normalized slots. Refresh fetches only permitted sources. Watch events are idempotent; booking is never automated.")
	}
	return lipgloss.NewStyle().Padding(1, 2).Width(contentWidth + 4).Render(strings.Join([]string{header, strings.Join(tabs, " "), dividerStyle.Render(strings.Repeat("─", contentWidth)), body, dividerStyle.Render(strings.Repeat("─", contentWidth)), status, footer}, "\n"))
}

func (model model) body(width int) string {
	switch model.tab {
	case tabDiscover:
		return model.discoverView(width)
	case tabWatches:
		return model.watchesView(width)
	case tabSources:
		return model.sourcesView(width)
	case tabEvents:
		return model.eventsView(width)
	default:
		return model.settingsView()
	}
}

func (model model) discoverView(width int) string {
	lines := []string{model.sport.View(), mutedStyle.Render("Enter searches local availability. Press r to refresh only policy-permitted sources.")}
	if len(model.results) == 0 {
		return strings.Join(append(lines, "", warningStyle.Render("No current availability data for this search."), mutedStyle.Render("SportSG currently supplies venue discovery, not live court slots. Use `availability add` for data you are authorized to supply.")), "\n")
	}
	lines = append(lines, "", headerStyle.Render("SCORE   START             VENUE                              PRICE     TRAVEL"))
	for _, result := range model.results[:min(len(model.results), max(3, model.height-11))] {
		name := truncate(result.Venue.Name, max(16, width-58))
		price := "—"
		if result.Breakdown.CourtPriceCents != nil {
			price = fmt.Sprintf("S$%d.%02d", *result.Breakdown.CourtPriceCents/100, *result.Breakdown.CourtPriceCents%100)
		}
		travel := "—"
		if result.Breakdown.TotalTravelSeconds > 0 {
			travel = fmt.Sprintf("%dm", result.Breakdown.TotalTravelSeconds/60)
		}
		lines = append(lines, fmt.Sprintf("%5.1f   %-16s  %-*s  %-8s  %s", result.Breakdown.FinalScore, result.Slot.Start.In(singaporeLocation()).Format("02 Jan 15:04"), max(16, width-58), name, price, travel))
	}
	return strings.Join(lines, "\n")
}

func (model model) watchesView(width int) string {
	if len(model.watches) == 0 {
		return warningStyle.Render("No watches yet. Search a sport in Discover, then press w to create a local watch.")
	}
	lines := []string{headerStyle.Render("ENABLED  ONE SHOT  NAME                              SPORTS")}
	for _, watch := range model.watches[:min(len(model.watches), max(3, model.height-8))] {
		lines = append(lines, fmt.Sprintf("%-7t  %-8t  %-*s  %s", watch.Enabled, watch.OneShot, max(18, width-45), truncate(watch.Name, max(18, width-45)), strings.Join(watch.Query.Sports, ", ")))
	}
	return strings.Join(append(lines, "", mutedStyle.Render("Press e to evaluate enabled watches against local availability.")), "\n")
}

func (model model) sourcesView(width int) string {
	lines := []string{headerStyle.Render("SOURCE                         POLICY                    HEALTH")}
	for _, source := range model.sources[:min(len(model.sources), max(3, model.height-8))] {
		lines = append(lines, fmt.Sprintf("%-*s  %-24s  %s", max(18, width-52), truncate(source.Info.Name, max(18, width-52)), source.Info.Policy.Status, source.Health.State))
	}
	return strings.Join(append(lines, "", mutedStyle.Render("Disabled and credentialed sources are displayed explicitly; refresh never bypasses source policy.")), "\n")
}

func (model model) eventsView(width int) string {
	if len(model.events) == 0 {
		return warningStyle.Render("No watch events yet. Events appear after an enabled watch finds a new matching slot.")
	}
	lines := []string{headerStyle.Render("WHEN                 WATCH                         TYPE")}
	for _, event := range model.events[:min(len(model.events), max(3, model.height-8))] {
		lines = append(lines, fmt.Sprintf("%-19s  %-*s  %s", event.CreatedAt.In(singaporeLocation()).Format("02 Jan 15:04:05"), max(18, width-52), truncate(event.WatchID, max(18, width-52)), event.Type))
	}
	return strings.Join(lines, "\n")
}

func (model model) settingsView() string {
	return strings.Join([]string{headerStyle.Render("Local-first settings"), "", "Database: " + model.service.Config().DatabasePath, "Routing: " + model.service.RoutingStatus(), "API: " + model.service.Config().API.Address, "MCP writes: " + fmt.Sprintf("%t", model.service.Config().MCP.AllowWrites), "", mutedStyle.Render("Secrets are read only from configured references and are never displayed here.")}, "\n")
}

func (model model) load() tea.Cmd {
	return func() tea.Msg {
		if model.service == nil {
			return dataMessage{err: fmt.Errorf("TUI service is unavailable")}
		}
		sport := strings.TrimSpace(model.sport.Value())
		results, resultErr := model.service.Search(model.ctx, domain.Query{Sports: []string{sport}, MinimumDuration: time.Hour, Ranking: domain.RankBalanced})
		watches, watchesErr := model.service.Watches(model.ctx, false)
		sources, sourcesErr := model.service.Sources(model.ctx)
		events, eventsErr := model.service.Events(model.ctx, "", 100)
		if resultErr != nil {
			return dataMessage{err: resultErr}
		}
		if watchesErr != nil {
			return dataMessage{err: watchesErr}
		}
		if sourcesErr != nil {
			return dataMessage{err: sourcesErr}
		}
		if eventsErr != nil {
			return dataMessage{err: eventsErr}
		}
		return dataMessage{results: results, watches: watches, sources: sources, events: events}
	}
}

func (model model) refresh() tea.Cmd {
	return func() tea.Msg {
		results, err := model.service.Refresh(model.ctx, nil)
		if err != nil {
			return actionMessage{err: err}
		}
		states := make([]string, 0, len(results))
		for _, result := range results {
			states = append(states, result.SourceID+": "+result.State)
		}
		return actionMessage{status: "Refresh complete — " + strings.Join(states, ", ")}
	}
}

func (model model) evaluate() tea.Cmd {
	return func() tea.Msg {
		results, err := model.service.EvaluateWatches(model.ctx)
		if err != nil {
			return actionMessage{err: err}
		}
		created := 0
		for _, result := range results {
			created += result.EventsCreated
		}
		return actionMessage{status: fmt.Sprintf("Evaluated %d watch(es); created %d new event(s).", len(results), created)}
	}
}

func (model model) createWatch() tea.Cmd {
	return func() tea.Msg {
		sport := strings.TrimSpace(model.sport.Value())
		watch, err := model.service.CreateWatch(model.ctx, domain.Watch{Name: "TUI " + sport, Query: domain.Query{Sports: []string{sport}, MinimumDuration: time.Hour, Ranking: domain.RankBalanced}, Enabled: true})
		if err != nil {
			return actionMessage{err: err}
		}
		return actionMessage{status: "Created watch " + watch.ID}
	}
}

func singaporeLocation() *time.Location {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		return time.FixedZone("SGT", 8*60*60)
	}
	return location
}

func truncate(value string, width int) string {
	if width < 2 || len(value) <= width {
		return value
	}
	return value[:width-1] + "…"
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
