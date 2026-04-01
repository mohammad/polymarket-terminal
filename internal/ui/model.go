package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"polymarket-terminal/internal/db"
	"polymarket-terminal/internal/orderbook"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type BookUpdateMsg struct {
	Bids []orderbook.Level
	Asks []orderbook.Level
	Hash string
}

type ConnectionMsg struct{ Connected bool }

type SwitchingMsg struct{}

type SyncStatusMsg struct {
	Message string
	Error   string
	Source  string
	Syncing bool
}

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
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
)

const (
	maxLevels  = 12
	staleAfter = 15 * time.Second
)

type Model struct {
	markets    []db.Market
	marketIdx  int
	switchIdx  int
	filtered   []int
	search     string
	bids       []orderbook.Level
	asks       []orderbook.Level
	hash       string
	connected  bool
	switching  bool
	lastUpdate time.Time
	switchMode bool
	statusMsg  string
	statusErr  string
	statusSrc  string
	width      int
	height     int
	onSwitch   func(assetID string)
	onRefresh  func()
}

func New(markets []db.Market, activeAssetID string, onSwitch func(assetID string), onRefresh func()) Model {
	m := Model{
		markets:   markets,
		onSwitch:  onSwitch,
		onRefresh: onRefresh,
	}
	for i, market := range markets {
		if market.AssetID == activeAssetID {
			m.marketIdx = i
			break
		}
	}
	m.rebuildFilter()
	return m
}

type tickMsg struct{}

func (m Model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case BookUpdateMsg:
		m.bids = msg.Bids
		m.asks = msg.Asks
		m.hash = msg.Hash
		m.switching = false
		m.statusErr = ""
		m.lastUpdate = time.Now()
	case SwitchingMsg:
		m.bids = nil
		m.asks = nil
		m.hash = ""
		m.switching = true
		m.lastUpdate = time.Time{}
	case ConnectionMsg:
		m.connected = msg.Connected
	case SyncStatusMsg:
		m.statusMsg = msg.Message
		m.statusErr = msg.Error
		m.statusSrc = msg.Source
		if msg.Syncing {
			m.switching = true
		} else if msg.Error != "" {
			m.switching = false
		}
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "s":
		if m.switchMode {
			m.closeSwitcher()
		} else {
			m.openSwitcher()
		}
	case "r":
		if !m.switchMode && m.onRefresh != nil {
			return m, func() tea.Msg {
				m.onRefresh()
				return nil
			}
		}
	case "esc":
		if m.switchMode {
			m.closeSwitcher()
		}
	case "up", "k":
		if m.switchMode && m.switchIdx > 0 {
			m.switchIdx--
		}
	case "down", "j":
		if m.switchMode && m.switchIdx < len(m.filtered)-1 {
			m.switchIdx++
		}
	case "enter":
		if m.switchMode {
			return m.commitSwitch()
		}
	case "backspace":
		if m.switchMode && len(m.search) > 0 {
			m.search = m.search[:len(m.search)-1]
			m.rebuildFilter()
		}
	default:
		if m.switchMode {
			for _, r := range msg.Runes {
				if isSearchRune(r) {
					m.search += string(r)
				}
			}
			m.rebuildFilter()
		}
	}
	return m, nil
}

func (m *Model) openSwitcher() {
	m.switchMode = true
	m.search = ""
	m.rebuildFilter()
	m.switchIdx = m.filteredIndexForMarket(m.marketIdx)
}

func (m *Model) closeSwitcher() {
	m.switchMode = false
	m.search = ""
	m.rebuildFilter()
	m.switchIdx = m.filteredIndexForMarket(m.marketIdx)
}

func (m Model) commitSwitch() (tea.Model, tea.Cmd) {
	m.switchMode = false
	if m.switchIdx >= len(m.filtered) {
		return m, nil
	}
	targetIdx := m.filtered[m.switchIdx]
	if targetIdx == m.marketIdx {
		return m, nil
	}
	m.marketIdx = targetIdx
	m.search = ""
	m.rebuildFilter()
	return m, m.switchCmd(m.markets[m.marketIdx].AssetID)
}

func (m Model) switchCmd(assetID string) tea.Cmd {
	if m.onSwitch == nil {
		return nil
	}
	return func() tea.Msg {
		m.onSwitch(assetID)
		return nil
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return " loading...\n"
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

	meta := ""
	if len(m.bids) > 0 && len(m.asks) > 0 {
		meta = dimStyle.Render(fmt.Sprintf("  bid %s  ask %s",
			m.bids[0].Price.StringFixed(4),
			m.asks[0].Price.StringFixed(4)))
	}
	title := fmt.Sprintf(" POLYMARKET  %s  %s%s", status, headerStyle.Render(label), meta)
	bar := strings.Repeat("─", max(m.width-2, 10))
	return title + "\n" + dimStyle.Render(" "+bar)
}

func (m Model) renderBook() string {
	const priceW, sizeW = 10, 12
	colHeader := dimStyle.Render(fmt.Sprintf("  %-*s  %s", priceW, "PRICE", "SIZE"))
	divider := dimStyle.Render("  " + strings.Repeat("─", priceW+sizeW+4))

	var b strings.Builder

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

	spread := "–"
	if len(m.bids) > 0 && len(m.asks) > 0 {
		spread = m.asks[0].Price.Sub(m.bids[0].Price).StringFixed(4)
	}
	b.WriteString(spreadStyle.Render(fmt.Sprintf("  ── SPREAD %-*s ──", priceW-4, spread)) + "\n")

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

	if m.switching {
		b.WriteString(dimStyle.Render("\n  syncing book...") + "\n")
	} else if !m.lastUpdate.IsZero() {
		age := time.Since(m.lastUpdate)
		line := fmt.Sprintf("\n  hash: %s...  updated: %.1fs ago", shortHash(m.hash), age.Seconds())
		if age > staleAfter {
			line += "  feed stale"
		}
		b.WriteString(dimStyle.Render(line) + "\n")
	}

	return b.String()
}

func (m Model) renderSwitcher() string {
	var b strings.Builder
	header := "  ── Markets (type to filter, ↑/↓ to navigate, enter to confirm) ──"
	if m.search != "" {
		header += "  search: " + m.search
	}
	b.WriteString(dimStyle.Render(header) + "\n")
	for i, idx := range m.filtered {
		mkt := m.markets[idx]
		shortID := mkt.AssetID
		if len(shortID) > 10 {
			shortID = shortID[:10] + "..."
		}
		line := fmt.Sprintf("  %s  %s", mkt.Label, dimStyle.Render("("+shortID+")"))
		if i == m.switchIdx {
			b.WriteString(selectedStyle.Render("▶"+line) + "\n")
		} else {
			b.WriteString(dimStyle.Render(" "+line) + "\n")
		}
	}
	return b.String()
}

func (m Model) renderHelp() string {
	parts := []string{"[s] switch", "[r] resync", "[↑/↓] navigate", "[esc] cancel", "[q] quit"}
	if m.statusErr != "" {
		parts = append(parts, errorStyle.Render(m.statusErr))
	} else if m.statusMsg != "" {
		parts = append(parts, m.statusMsg)
	}
	return helpStyle.Render(" "+strings.Join(parts, "  ")) + "\n"
}

func (m *Model) rebuildFilter() {
	m.filtered = m.filtered[:0]
	needle := strings.ToLower(strings.TrimSpace(m.search))
	for i, market := range m.markets {
		if needle == "" ||
			strings.Contains(strings.ToLower(market.Label), needle) ||
			strings.Contains(strings.ToLower(market.AssetID), needle) {
			m.filtered = append(m.filtered, i)
		}
	}
	if len(m.filtered) == 0 && len(m.markets) > 0 {
		m.filtered = append(m.filtered, m.marketIdx)
	}
	if m.switchIdx >= len(m.filtered) {
		m.switchIdx = len(m.filtered) - 1
	}
	if m.switchIdx < 0 {
		m.switchIdx = 0
	}
}

func (m Model) filteredIndexForMarket(marketIdx int) int {
	for i, idx := range m.filtered {
		if idx == marketIdx {
			return i
		}
	}
	return 0
}

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

func isSearchRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || r == '-' || r == '_'
}
