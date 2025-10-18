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

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/viewport"
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
	width        int
	height       int
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-1)
			m.viewport.SetContent(strings.Join(m.content, "\n"))
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 1
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "l", "right":
			m.viewport.ScrollRight(5)
		case "h", "left":
			m.viewport.ScrollLeft(5)
		case "g", "home":
			m.viewport.GotoTop()
		case "G", "end":
			m.viewport.GotoBottom()
		}
	}

	// Pass all messages to the viewport for scrolling, etc.
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	statusBar := statusBarStyle.Render(fmt.Sprintf(
		"↑↓/kj: vertical | ←→/hl: horizontal | g/G: jump | q: quit | Line: %d/%d",
		m.viewport.YOffset+1,
		len(m.content),
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
			fmt.Fprintf(os.Stderr, "Error: cannot traverse into non-object at path '%s'",
				strings.Join(path[:i], "."))
			os.Exit(1)
		}

		val, exists := m[key]
		if !exists {
			fmt.Fprintf(os.Stderr, "Error: key '%s' not found in path '%s'",
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
	s = strings.ReplaceAll(s, "\r", "")

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
