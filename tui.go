package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// statsProvider is implemented by AppProxy and TunerProxy via embedded backendRouter.
type statsProvider interface {
	Stats() ProxyStats
}

type tickMsg time.Time

const (
	sidebarInnerWidth = 22
)

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69")).
			Bold(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75")).
			Bold(true)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	greenDot = lipgloss.NewStyle().Foreground(lipgloss.Color("76")).Render("●")

	sidebarStyle = lipgloss.NewStyle().
			Width(sidebarInnerWidth).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	logBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	levelStyles = map[slog.Level]lipgloss.Style{
		slog.LevelDebug: lipgloss.NewStyle().Foreground(lipgloss.Color("69")),
		slog.LevelInfo:  lipgloss.NewStyle().Foreground(lipgloss.Color("76")),
		slog.LevelWarn:  lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		slog.LevelError: lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	}
)

type tuiModel struct {
	proxy     statsProvider
	stats     ProxyStats
	showDebug bool
	vp        viewport.Model
	ready     bool
	width     int
	height    int
}

func newTuiModel(proxy statsProvider) tuiModel {
	return tuiModel{
		proxy:     proxy,
		showDebug: true,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpW, vpH := m.logViewportSize()
		if !m.ready {
			m.vp = viewport.New(vpW, vpH)
			m.ready = true
		} else {
			m.vp.Width = vpW
			m.vp.Height = vpH
		}

	case tickMsg:
		m.stats = m.proxy.Stats()
		cmds = append(cmds, tick())

	case logMsg:
		m.vp.SetContent(m.renderLogContent())
		m.vp.GotoBottom()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "d":
			m.showDebug = !m.showDebug
			m.vp.SetContent(m.renderLogContent())
		}
	}

	var vpCmd tea.Cmd
	m.vp, vpCmd = m.vp.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m tuiModel) logViewportSize() (w, h int) {
	sidebarW := lipgloss.Width(sidebarStyle.Render(""))
	// gap(1) + log border left(1) + log border right(1) = 3
	w = m.width - sidebarW - 3
	if w < 10 {
		w = 10
	}
	h = m.height - 2 // log border top(1) + bottom(1)
	if h < 1 {
		h = 1
	}
	return
}

func (m tuiModel) renderSidebar() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("HDHomeRun Proxy") + "\n\n")

	b.WriteString(labelStyle.Render("MODE") + "\n")
	b.WriteString(valueStyle.Render(m.stats.Name) + "\n\n")

	b.WriteString(labelStyle.Render("CONNECTIONS") + "\n")
	b.WriteString(fmt.Sprintf("UDP   %s\n", valueStyle.Render(fmt.Sprintf("%d", m.stats.ActiveUDP))))
	b.WriteString(fmt.Sprintf("Dial  %s\n", valueStyle.Render(fmt.Sprintf("%d", m.stats.ActiveDial))))
	b.WriteString(fmt.Sprintf("Total %s\n", valueStyle.Render(fmt.Sprintf("%d", m.stats.ActiveUDP+m.stats.ActiveDial))))

	if m.stats.DirectHDHRIP != "" || m.stats.TunarrConfigured {
		b.WriteString("\n" + labelStyle.Render("BACKENDS") + "\n")
		if m.stats.DirectHDHRIP != "" {
			b.WriteString(greenDot + " HDHR " + dimStyle.Render(m.stats.DirectHDHRIP) + "\n")
		}
		if m.stats.TunarrConfigured {
			b.WriteString(greenDot + " Tunarr " + dimStyle.Render(fmt.Sprintf(":%d", m.stats.TunarrPort)) + "\n")
		}
	}

	debugLabel := "off"
	if m.showDebug {
		debugLabel = "on"
	}
	b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("[q]quit [d]debug(%s)", debugLabel)))

	return sidebarStyle.Render(b.String())
}

func (m tuiModel) renderLogContent() string {
	var lines []string
	for _, e := range getLogEntries() {
		if e.Level == slog.LevelDebug && !m.showDebug {
			continue
		}
		ls, ok := levelStyles[e.Level]
		if !ok {
			ls = valueStyle
		}
		levelStr := ls.Render(fmt.Sprintf("%-5s", e.Level.String()))
		ts := dimStyle.Render(e.Time.Format("15:04:05"))
		line := fmt.Sprintf("%s %s %s", ts, levelStr, e.Msg)
		if e.Attrs != "" {
			line += " " + dimStyle.Render(e.Attrs)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) View() string {
	if !m.ready {
		return "Initializing..."
	}
	sidebar := m.renderSidebar()
	// gap(1) + log border left(1) + log border right(1) = 3
	logBorderInnerW := m.width - lipgloss.Width(sidebar) - 3
	if logBorderInnerW < 10 {
		logBorderInnerW = 10
	}
	logPane := logBorderStyle.Width(logBorderInnerW).Height(m.vp.Height).Render(m.vp.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", logPane)
}

// runWithTUI starts the proxy in a goroutine, runs the Bubble Tea program in
// the current goroutine, then cancels the context when the TUI exits.
func runWithTUI(ctx context.Context, cancel context.CancelFunc, proxy statsProvider, runFn func() error) {
	p := tea.NewProgram(newTuiModel(proxy), tea.WithAltScreen())
	slog.SetDefault(slog.New(newTuiHandler(p)))

	go func() {
		if err := runFn(); err != nil {
			slog.Error("Proxy error", "err", err)
		}
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
	}
	cancel()
}
