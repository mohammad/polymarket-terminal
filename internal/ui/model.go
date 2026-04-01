package ui

import (
	"fmt"
	"strings"
	"time"

	"polymarket-terminal/internal/db"
	"polymarket-terminal/internal/orderbook"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── External messages (sent from other goroutines via tea.Program.Send) ──────

// BookUpdateMsg is sent by the reconciler whenever the orderbook changes.
type BookUpdateMsg struct {
	Bids []orderbook.Level
	Asks []orderbook.Level
	Hash string
}

// ConnectionMsg is sent by the WS client when connection state changes.
type ConnectionMsg struct{ Connected bool }

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	liveStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	deadStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	askStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	bidStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("83"))
	spreadStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("228")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	borderColor   = lipgloss.Color("237")
)

const maxLevels = 12

// ── Model ─────────────────────────────────────────────────────────────────────

// Model is the bubbletea model for the terminal UI.
type Model struct {
	markets     []db.Market
	marketIdx   int
	bids        []orderbook.Level
	asks        []orderbook.Level
	hash        string
	connected   bool
	lastUpdate  time.Time
	switchMode  bool
	width       int
	height      int
	onSwitch    func(assetID string)
}

// New returns an initial Model. onSwitch is called when the user picks a market.
func New(markets []db.Market, onSwitch func(assetID string)) Model {
	return Model{
		markets:  markets,
		onSwitch: onSwitch,
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case BookUpdateMsg:
		m.bids = msg.Bids
		m.asks = msg.Asks
		m.hash = msg.Hash
		m.lastUpdate = time.Now()
	case ConnectionMsg:
		m.connected = msg.Connected
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "s":
		m.switchMode = !m.switchMode

	case "esc":
		m.switchMode = false

	case "up", "k":
		if m.switchMode && m.marketIdx > 0 {
			m.marketIdx--
			m.doSwitch()
		}

	case "down", "j":
		if m.switchMode && m.marketIdx < len(m.markets)-1 {
			m.marketIdx++
			m.doSwitch()
		}

	case "enter":
		if m.switchMode {
			m.switchMode = false
		}
	}
	return m, nil
}

func (m Model) doSwitch() {
	if m.onSwitch != nil && m.marketIdx < len(m.markets) {
		m.onSwitch(m.markets[m.marketIdx].AssetID)
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return " loading…\n"
	}

	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderBook())
	if m.switchMode {
		b.WriteString("\n")
		b.WriteString(m.renderSwitcher())
	}
	b.WriteString("\n")
	b.WriteString(m.renderHelp())
	return b.String()
}

func (m Model) renderHeader() string {
	status := deadStyle.Render("● DISC")
	if m.connected {
		status = liveStyle.Render("● LIVE")
	}
	label := "–"
	if m.marketIdx < len(m.markets) {
		label = m.markets[m.marketIdx].Label
	}
	title := fmt.Sprintf(" POLYMARKET  %s  %s", status, headerStyle.Render(label))
	bar := strings.Repeat("─", max(m.width-2, 10))
	return title + "\n" + dimStyle.Render(" "+bar)
}

func (m Model) renderBook() string {
	const priceW, sizeW = 10, 12
	colHeader := dimStyle.Render(fmt.Sprintf("  %-*s  %s", priceW, "PRICE", "SIZE"))
	divider := dimStyle.Render("  " + strings.Repeat("─", priceW+sizeW+4))

	var b strings.Builder

	// Asks — display top N from high→low (reverse asks slice)
	asks := m.asks
	if len(asks) > maxLevels {
		asks = asks[len(asks)-maxLevels:]
	}
	b.WriteString(dimStyle.Render("  ASKS") + "\n")
	b.WriteString(colHeader + "\n")
	b.WriteString(divider + "\n")
	for i := len(asks) - 1; i >= 0; i-- {
		l := asks[i]
		b.WriteString(askStyle.Render(
			fmt.Sprintf("  %-*s  %s", priceW, l.Price.StringFixed(4), l.Size.StringFixed(2))) + "\n")
	}

	// Spread
	spread := "–"
	if len(m.bids) > 0 && len(m.asks) > 0 {
		spread = m.asks[0].Price.Sub(m.bids[0].Price).StringFixed(4)
	}
	b.WriteString(spreadStyle.Render(fmt.Sprintf("  ── SPREAD %-*s ──", priceW-4, spread)) + "\n")

	// Bids — top N high→low
	bids := m.bids
	if len(bids) > maxLevels {
		bids = bids[:maxLevels]
	}
	b.WriteString(divider + "\n")
	b.WriteString(colHeader + "\n")
	b.WriteString(dimStyle.Render("  BIDS") + "\n")
	for _, l := range bids {
		b.WriteString(bidStyle.Render(
			fmt.Sprintf("  %-*s  %s", priceW, l.Price.StringFixed(4), l.Size.StringFixed(2))) + "\n")
	}

	// Status line
	if !m.lastUpdate.IsZero() {
		age := time.Since(m.lastUpdate)
		b.WriteString(dimStyle.Render(
			fmt.Sprintf("\n  hash: %s…  updated: %.1fs ago", shortHash(m.hash), age.Seconds())) + "\n")
	}

	return b.String()
}

func (m Model) renderSwitcher() string {
	var b strings.Builder
	b.WriteString(dimStyle.Render("  ── Markets (↑/↓ to navigate, enter to confirm) ──") + "\n")
	for i, mkt := range m.markets {
		shortID := mkt.AssetID
		if len(shortID) > 10 {
			shortID = shortID[:10] + "…"
		}
		line := fmt.Sprintf("  %s  %s", mkt.Label, dimStyle.Render("("+shortID+")"))
		if i == m.marketIdx {
			b.WriteString(selectedStyle.Render("▶"+line) + "\n")
		} else {
			b.WriteString(dimStyle.Render(" "+line) + "\n")
		}
	}
	return b.String()
}

func (m Model) renderHelp() string {
	parts := []string{"[s] switch", "[↑/↓] navigate", "[esc] cancel", "[q] quit"}
	return helpStyle.Render(" "+strings.Join(parts, "  ")) + "\n"
}

// ── helpers ───────────────────────────────────────────────────────────────────

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
