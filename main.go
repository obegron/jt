package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var (
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ca9ee6"))
	keyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#c6d0f5"))
	stringStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a6d189"))
	boolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ea999c"))
	intStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c6d0f5")).
			Background(lipgloss.Color("#414559")).
			Padding(0, 1)
)

const maxValueWidth = 80

type model struct {
	viewport     viewport.Model
	content      []string // lines of content
	ready        bool
	contentWidth int
	xOffset      int
	width        int
	height       int
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "h", "left":
			if m.xOffset > 0 {
				m.xOffset -= 5
				if m.xOffset < 0 {
					m.xOffset = 0
				}
				m.updateViewportContent()
			}
		case "l", "right":
			maxScroll := m.contentWidth - m.width
			if maxScroll > 0 && m.xOffset < maxScroll {
				m.xOffset += 5
				if m.xOffset > maxScroll {
					m.xOffset = maxScroll
				}
				m.updateViewportContent()
			}
		case "g", "home":
			m.viewport.GotoTop()
			m.xOffset = 0
			m.updateViewportContent()
		case "G", "end":
			m.viewport.GotoBottom()
		case "0":
			m.xOffset = 0
			m.updateViewportContent()
		case "$":
			maxScroll := m.contentWidth - m.width
			if maxScroll > 0 {
				m.xOffset = maxScroll
				m.updateViewportContent()
			}
		default:
			// Pass other keys to viewport for vertical scrolling
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-1)
			m.updateViewportContent()
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 1
			m.updateViewportContent()
		}
		return m, nil
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *model) updateViewportContent() {
	// Slice each line horizontally based on xOffset
	visibleLines := make([]string, len(m.content))
	for i, line := range m.content {
		visibleLines[i] = sliceLine(line, m.xOffset, m.width)
	}
	m.viewport.SetContent(strings.Join(visibleLines, "\n"))
}

func sliceLine(line string, offset, width int) string {
	// Use lipgloss.Width for accurate width calculation
	lineWidth := lipgloss.Width(line)
	
	// If line fits entirely, return as-is
	if offset == 0 && lineWidth <= width {
		return line
	}
	
	// If offset is beyond line length, return empty
	if offset >= lineWidth {
		return ""
	}
	
	// Extract the visible portion using rune-based slicing
	runes := []rune(line)
	var result strings.Builder
	var ansiBuffer strings.Builder
	inAnsi := false
	visiblePos := 0
	writtenChars := 0
	
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		
		// Detect ANSI escape sequence start
		if r == '\x1b' {
			inAnsi = true
			ansiBuffer.WriteRune(r)
			continue
		}
		
		// Continue collecting ANSI sequence
		if inAnsi {
			ansiBuffer.WriteRune(r)
			// ANSI sequence ends with a letter
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				// Write the complete ANSI sequence
				result.WriteString(ansiBuffer.String())
				ansiBuffer.Reset()
				inAnsi = false
			}
			continue
		}
		
		// Handle visible character
		if visiblePos >= offset && writtenChars < width {
			result.WriteRune(r)
			writtenChars++
		}
		visiblePos++
		
		if writtenChars >= width {
			break
		}
	}
	
	// Add any remaining ANSI codes
	if ansiBuffer.Len() > 0 {
		result.WriteString(ansiBuffer.String())
	}
	
	return result.String()
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Calculate visible range
	maxHScroll := m.contentWidth - m.width
	if maxHScroll < 0 {
		maxHScroll = 0
	}

	statusBar := statusBarStyle.Render(fmt.Sprintf(
		"↑↓/kj: vertical | ←→/hl: horizontal | g/G: top/bottom | 0/$: left/right | q: quit | Line: %d/%d | Col: %d/%d",
		m.viewport.YOffset+1,
		len(m.content),
		m.xOffset+1,
		m.contentWidth,
	))

	return m.viewport.View() + "\n" + statusBar
}

func main() {
	format := flag.String("format", "table", "Output format table/html")
	details := flag.Bool("d", false, "Show details (caption)")
	maxWidth := flag.Int("w", maxValueWidth, "Maximum width for values")
	flag.Parse()

	input, selector := readInput()
	data := parseInput(input)
	data = applySelector(data, selector)

	render(data, *format, *details, *maxWidth)
}

func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80 // default fallback
	}
	return width
}

func getContentWidth(content string) int {
	maxWidth := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		// Use lipgloss.Width for accurate width calculation
		width := lipgloss.Width(line)
		if width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
}

func readInput() ([]byte, string) {
	args := flag.Args()
	var input []byte
	var err error
	var selector string

	if len(args) == 0 || (len(args) == 1 && strings.HasPrefix(args[0], ".")) {
		info, err := os.Stdin.Stat()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading from stdin:", err)
			os.Exit(1)
		}
		if (info.Mode() & os.ModeCharDevice) == 0 {
			input, err = io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading from stdin:", err)
				os.Exit(1)
			}
		}
	} else {
		input, err = os.ReadFile(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading file:", err)
			os.Exit(1)
		}
	}

	if len(args) > 0 && strings.HasPrefix(args[len(args)-1], ".") {
		selector = args[len(args)-1]
	} else {
		selector = "."
	}

	if len(input) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: cat data.json | jt [selector]")
		fmt.Fprintln(os.Stderr, "       jt <file> [selector]")
		os.Exit(1)
	}

	return input, selector
}

func parseInput(input []byte) interface{} {
	var data interface{}
	if err := json.Unmarshal(input, &data); err != nil {
		if err := yaml.Unmarshal(input, &data); err != nil {
			fmt.Fprintln(os.Stderr, "Error: Input is not valid JSON or YAML.")
			os.Exit(1)
		}
	}
	return data
}

func applySelector(data interface{}, selector string) interface{} {
	if selector == "." {
		return data
	}

	// Split the selector path (e.g., ".nested.key1" -> ["nested", "key1"])
	path := strings.Split(strings.TrimPrefix(selector, "."), ".")

	current := data
	for i, key := range path {
		m, ok := current.(map[string]interface{})
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: cannot traverse into non-object at path '%s'\n",
				strings.Join(path[:i], "."))
			os.Exit(1)
		}

		val, exists := m[key]
		if !exists {
			fmt.Fprintf(os.Stderr, "Error: key '%s' not found in path '%s'\n",
				key, strings.Join(path[:i+1], "."))
			os.Exit(1)
		}

		current = val
	}

	return current
}

func render(data interface{}, format string, details bool, maxWidth int) {
	output := renderRecursive(data, details, format, maxWidth)

	// For HTML, add CSS styling at the beginning
	if format == "html" {
		fmt.Println(`<style>
.jt-table {
	border-collapse: collapse;
	background-color: #303446;
	border: 1px solid #414559;
	margin: 2px;
}
.jt-table th {
	text-align: center;
	color: #ca9ee6;
	font-weight: bold;
}
.jt-table td {
	border: 1px solid #414559;
	padding: 8px;
	text-align: left;
}
.jt-key { color: #c6d0f5; }
.jt-string { color: #a6d189; }
.jt-bool { color: #ea999c; }
.jt-number { color: #ffffff; }
.jt-nested { color: #c6d0f5; }
</style>`)
		fmt.Print(output)
		return
	}

	// Check if we should use interactive viewer
	if format == "table" && isTerminal() {
		termWidth := getTerminalWidth()
		contentWidth := getContentWidth(output)

		// Use interactive viewer if content is wider than terminal
		if contentWidth > termWidth {
			lines := strings.Split(output, "\n")
			m := model{
				content:      lines,
				contentWidth: contentWidth,
			}
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error running interactive viewer: %v\n", err)
				// Fallback to regular output
				fmt.Println(output)
			}
			return
		}
	}

	// Regular output for non-interactive cases
	fmt.Println(output)
}

func renderRecursive(data interface{}, details bool, format string, maxWidth int) string {
	var buf bytes.Buffer
	table := createTable(&buf, format)

	appendData(table, data, details, format, maxWidth)
	table.Render()

	return buf.String()
}

func createTable(buf *bytes.Buffer, format string) *tablewriter.Table {
	switch format {
	case "html":
		cfg := renderer.HTMLConfig{
			HeaderClass:   "jt-header",
			TableClass:    "jt-table",
			EscapeContent: false,
		}
		return tablewriter.NewTable(buf, tablewriter.WithRenderer(renderer.NewHTML(cfg)))
	default: // table
		return tablewriter.NewTable(buf,
			tablewriter.WithHeaderAlignment(tw.AlignLeft),
			tablewriter.WithRowAlignment(tw.AlignLeft),
			tablewriter.WithRendition(tw.Rendition{
				Borders: tw.Border{Left: tw.On, Right: tw.On, Top: tw.On, Bottom: tw.On},
				Settings: tw.Settings{
					Separators: tw.Separators{BetweenColumns: tw.On, BetweenRows: tw.On},
				},
			}),
		)
	}
}

func truncateValue(s string, maxWidth int) string {
	// Replace newlines with spaces for single-line display
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")

	// Collapse multiple spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}

	s = strings.TrimSpace(s)

	if len(s) <= maxWidth {
		return s
	}

	return s[:maxWidth-3] + "..."
}

func formatValue(val interface{}, details bool, format string, maxWidth int) string {
	switch v := val.(type) {
	case map[string]interface{}, []interface{}:
		nested := renderRecursive(val, details, format, maxWidth)
		// For HTML, ensure nested table stays as single value (no newlines that could split it)
		if format == "html" {
			// Remove newlines to keep nested table in one cell
			nested = strings.ReplaceAll(nested, "\n", "")
			return nested
		}
		return nested
	default:
		value := fmt.Sprintf("%v", v)
		// Escape HTML entities for primitive values in HTML format
		if format == "html" {
			value = escapeHTML(value)
		}
		return truncateValue(value, maxWidth)
	}
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func appendData(table *tablewriter.Table, data interface{}, details bool, format string, maxWidth int) {
	useColor := isTerminal() && format == "table"

	switch v := data.(type) {
	case []interface{}:
		if details {
			table.Caption(tw.Caption{Text: fmt.Sprintf("[-] array, %d items", len(v))})
		}
		if len(v) == 0 {
			return
		}

		// Build header row
		headers := []string{"[key]"}
		if first, ok := v[0].(map[string]interface{}); ok {
			keys := make([]string, 0, len(first))
			for k := range first {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			headers = append(headers, keys...)
		}
		table.Header(headers)

		for i, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				row := []string{fmt.Sprintf("%d", i)}
				for _, key := range headers[1:] {
					val := m[key]
					value := formatValue(val, details, format, maxWidth)

					if useColor {
						row = append(row, getStyle(val).Render(value))
					} else if format == "html" {
						class := getHTMLClass(val)
						row = append(row, fmt.Sprintf(`<span class="%s">%s</span>`, class, value))
					} else {
						row = append(row, value)
					}
				}
				if useColor {
					row[0] = keyStyle.Render(row[0])
				} else if format == "html" {
					row[0] = fmt.Sprintf(`<span class="jt-key">%s</span>`, row[0])
				}
				table.Append(row)
			} else {
				value := formatValue(item, details, format, maxWidth)
				appendRow(table, fmt.Sprintf("%d", i), value, item, useColor, format)
			}
		}

	case map[string]interface{}:
		if details {
			table.Caption(tw.Caption{Text: fmt.Sprintf("[-] object, %d properties", len(v))})
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			val := v[key]
			value := formatValue(val, details, format, maxWidth)
			appendRow(table, key, value, val, useColor, format)
		}

	default:
		table.Append([]string{"value", truncateValue(fmt.Sprintf("%v", v), maxWidth)})
	}
}

func appendRow(table *tablewriter.Table, key, value string, originalVal interface{}, useColor bool, format string) {
	if useColor {
		table.Append([]string{
			keyStyle.Render(key),
			getStyle(originalVal).Render(value),
		})
	} else if format == "html" {
		// Add color styling via CSS classes for HTML output
		cssClass := getHTMLClass(originalVal)

		styledKey := fmt.Sprintf(`<span class="jt-key">%s</span>`, key)
		styledValue := fmt.Sprintf(`<span class="%s">%s</span>`, cssClass, value)

		table.Append([]string{styledKey, styledValue})
	} else {
		table.Append([]string{key, value})
	}
}

func getHTMLClass(val interface{}) string {
	switch val.(type) {
	case bool:
		return "jt-bool"
	case string:
		return "jt-string"
	case int, int64, float64:
		return "jt-number"
	case map[string]interface{}, []interface{}:
		return "jt-nested"
	}
	return "jt-key"
}

func getStyle(val interface{}) lipgloss.Style {
	switch val.(type) {
	case bool:
		return boolStyle
	case string:
		return stringStyle
	case int, int64, float64:
		return intStyle
	}
	return keyStyle
}