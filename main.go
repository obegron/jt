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

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"
	"gopkg.in/yaml.v3"
)

var (
	keyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#c6d0f5"))
	stringStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a6d189"))
	boolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ea999c"))
	intStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
)

const maxValueWidth = 80

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
	margin: 5px;
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
	}

	// For HTML and Markdown, write directly without extra formatting
	if format == "html" {
		fmt.Print(output)
	} else {
		fmt.Println(output)
	}
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
	case "markdown":
		return tablewriter.NewTable(buf, tablewriter.WithRenderer(renderer.NewMarkdown()))
	case "html":
		cfg := renderer.HTMLConfig{
			TableClass:    "jt-table",
			EscapeContent: false,
		}
		return tablewriter.NewTable(buf, tablewriter.WithRenderer(renderer.NewHTML(cfg)))
	case "svg":
		return tablewriter.NewTable(buf, tablewriter.WithRenderer(renderer.NewSVG()))
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
	isTerminal := isatty.IsTerminal(os.Stdout.Fd())
	useColor := isTerminal && format == "table"

	switch v := data.(type) {
	case map[string]interface{}:
		if details {
			table.Caption(tw.Caption{Text: fmt.Sprintf("[-] Object, %d properties", len(v))})
		}

		// Sort keys for consistent output
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			val := v[key]
			value := formatValue(val, details, format, maxWidth)
			appendRow(table, key, value, val, useColor, format)
		}
	case []interface{}:
		if details {
			table.Caption(tw.Caption{Text: fmt.Sprintf("[-] Array, %d items", len(v))})
		}
		for i, item := range v {
			value := formatValue(item, details, format, maxWidth)
			appendRow(table, fmt.Sprintf("%d", i), value, item, useColor, format)
		}
	default:
		fmt.Println(data)
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
