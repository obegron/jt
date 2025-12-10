package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/textinput"
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

	searchBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#ca9ee6")).
			Padding(0, 1).
			Width(50)

	highlightStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#e5c890")).
			Foreground(lipgloss.Color("#232634"))

	currentMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#ef9f76")).
				Foreground(lipgloss.Color("#232634"))
)

const maxValueWidth = 80

type searchMatch struct {
	line int
	col  int
	text string
}

type model struct {
	viewport     viewport.Model
	content      []string // lines of content
	plainContent []string // content without ANSI codes for searching
	ready        bool
	contentWidth int
	width        int
	height       int
	searchMode   bool
	searchInput  textinput.Model
	searchTerm   string
	matches      []searchMatch
	currentMatch int
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
			m.viewport.SetContent(m.renderContent())
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 1
		}

	case tea.KeyMsg:
		if m.searchMode {
			switch msg.String() {
			case "esc":
				m.searchMode = false
				m.searchInput.Blur()
				return m, nil
			case "enter":
				m.searchTerm = m.searchInput.Value()
				m.findMatches()
				if len(m.matches) > 0 {
					m.currentMatch = 0
					m.jumpToMatch()
					m.searchMode = false
					m.searchInput.Blur()
				}
				m.viewport.SetContent(m.renderContent())
				return m, nil
			default:
				m.searchInput, cmd = m.searchInput.Update(msg)
				return m, cmd
			}
		} else {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "/":
				m.searchMode = true
				m.searchInput.Focus()
				m.searchInput.SetValue("")
				return m, textinput.Blink
			case "n":
				if len(m.matches) > 0 {
					m.currentMatch = (m.currentMatch + 1) % len(m.matches)
					m.jumpToMatch()
					m.viewport.SetContent(m.renderContent())
				}
				return m, nil
			case "N", "p":
				if len(m.matches) > 0 {
					m.currentMatch = (m.currentMatch - 1 + len(m.matches)) % len(m.matches)
					m.jumpToMatch()
					m.viewport.SetContent(m.renderContent())
				}
				return m, nil
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
	}

	// Pass all messages to the viewport for scrolling, etc.
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *model) findMatches() {
	m.matches = []searchMatch{}
	if m.searchTerm == "" {
		return
	}

	searchLower := strings.ToLower(m.searchTerm)
	for lineNum, line := range m.plainContent {
		lineLower := strings.ToLower(line)
		col := 0
		for {
			idx := strings.Index(lineLower[col:], searchLower)
			if idx == -1 {
				break
			}
			actualCol := col + idx
			m.matches = append(m.matches, searchMatch{
				line: lineNum,
				col:  actualCol,
				text: m.searchTerm,
			})
			col = actualCol + 1
		}
	}
}

// xml
func parseXML(input []byte) (interface{}, error) {
	decoder := xml.NewDecoder(bytes.NewReader(input))
	var result interface{}
	foundStartElement := false // New flag

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if se, ok := token.(xml.StartElement); ok {
			result = parseXMLElement(decoder, se)
			foundStartElement = true // Set flag
			break
		}
	}

	if !foundStartElement && result == nil { // If no start element found and result is still nil
		return nil, fmt.Errorf("no XML start element found") // Return an explicit error
	}

	return result, nil
}

func parseXMLElement(decoder *xml.Decoder, start xml.StartElement) interface{} {
	children := make(map[string][]interface{})
	var text strings.Builder
	hasAttributes := len(start.Attr) > 0

	// Handle attributes
	var attrs map[string]interface{}
	if hasAttributes {
		attrs = make(map[string]interface{})
		for _, attr := range start.Attr {
			attrs["@"+attr.Name.Local] = attr.Value
		}
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := token.(type) {
		case xml.StartElement:
			child := parseXMLElement(decoder, t)
			children[t.Name.Local] = append(children[t.Name.Local], child)
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			textContent := strings.TrimSpace(text.String())

			// If we have no children and no attributes, just return text
			if len(children) == 0 && !hasAttributes {
				if textContent != "" {
					return textContent
				}
				return ""
			}

			// Build result map
			result := make(map[string]interface{})

			// Add attributes first (prefixed with @)
			if hasAttributes {
				for k, v := range attrs {
					result[k] = v
				}
			}

			// Add children
			for key, values := range children {
				if len(values) == 1 {
					result[key] = values[0]
				} else {
					result[key] = values
				}
			}

			// Add text content if present
			if textContent != "" {
				result["#text"] = textContent
			}

			return result
		}
	}

	return nil
}

func (m *model) jumpToMatch() {
	if len(m.matches) == 0 {
		return
	}
	match := m.matches[m.currentMatch]
	m.viewport.SetYOffset(match.line)
}

func (m *model) renderContent() string {
	if m.searchTerm == "" {
		return strings.Join(m.content, "\n")
	}

	highlightedLines := make([]string, len(m.content))
	copy(highlightedLines, m.content)

	// Group matches by line for efficient highlighting
	matchesByLine := make(map[int][]searchMatch)
	for _, match := range m.matches {
		matchesByLine[match.line] = append(matchesByLine[match.line], match)
	}

	// Highlight each line with matches
	for lineNum, matches := range matchesByLine {
		if lineNum >= len(m.plainContent) {
			continue
		}
		line := m.plainContent[lineNum]

		// Sort matches by column to process left to right
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].col < matches[j].col
		})

		// Build highlighted line
		var result strings.Builder
		lastPos := 0

		for i, match := range matches {
			// Add text before match
			if match.col > lastPos {
				result.WriteString(line[lastPos:match.col])
			}

			// Add highlighted match
			matchText := line[match.col : match.col+len(m.searchTerm)]
			isCurrentMatch := false
			for j, currentMatch := range m.matches {
				if j == m.currentMatch && currentMatch.line == lineNum && currentMatch.col == match.col {
					isCurrentMatch = true
					break
				}
			}

			if isCurrentMatch {
				result.WriteString(currentMatchStyle.Render(matchText))
			} else {
				result.WriteString(highlightStyle.Render(matchText))
			}

			lastPos = match.col + len(m.searchTerm)

			// Add remaining text after last match
			if i == len(matches)-1 && lastPos < len(line) {
				result.WriteString(line[lastPos:])
			}
		}

		highlightedLines[lineNum] = result.String()
	}

	return strings.Join(highlightedLines, "\n")
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	var statusText string
	if m.searchTerm != "" && len(m.matches) > 0 {
		statusText = fmt.Sprintf(
			"↑↓/kj: vertical | ←→/hl: horizontal | g/G: jump | n/p: next/prev match | /: search | q: quit | Match: %d/%d | Line: %d/%d",
			m.currentMatch+1,
			len(m.matches),
			m.viewport.YOffset+1,
			len(m.content),
		)
	} else if m.searchTerm != "" {
		statusText = fmt.Sprintf(
			"↑↓/kj: vertical | ←→/hl: horizontal | g/G: jump | /: search | q: quit | No matches | Line: %d/%d",
			m.viewport.YOffset+1,
			len(m.content),
		)
	} else {
		statusText = fmt.Sprintf(
			"↑↓/kj: vertical | ←→/hl: horizontal | g/G: jump | /: search | q: quit | Line: %d/%d",
			m.viewport.YOffset+1,
			len(m.content),
		)
	}

	statusBar := statusBarStyle.Render(statusText)

	view := m.viewport.View() + "\n" + statusBar

	if m.searchMode {
		searchBox := searchBoxStyle.Render("Search: " + m.searchInput.View())

		// Place search box in center of screen
		view = lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			searchBox,
			lipgloss.WithWhitespaceChars(" "),
		)
		// Keep status bar at bottom
		view = view[:len(view)-len(statusBar)-1] + "\n" + statusBar
	}

	return view
}

func main() {
	format := flag.String("format", "table", "Output format table/html")
	details := flag.Bool("d", false, "Show details (caption)")
	maxWidth := flag.Int("w", maxValueWidth, "Maximum width for values")
	flag.Parse()

	input, selector := readInput()
	data, isMultiDoc := parseInput(input)
	data = applySelector(data, selector)

	render(data, *format, *details, *maxWidth, isMultiDoc)
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

func stripANSI(s string) string {
	// Simple ANSI code stripper for search purposes
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isSelector(s string) bool {
	if s == "." {
		return true
	}
	if len(s) >= 2 && s[0] == '.' {
		firstChar := s[1]
		return (firstChar >= 'a' && firstChar <= 'z') ||
			(firstChar >= 'A' && firstChar <= 'Z') ||
			firstChar == '['
	}
	return false
}

func stdinHasData() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func readStdin() []byte {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading from stdin:", err)
		os.Exit(1)
	}
	return input
}

func readFile(filepath string) []byte {
	input, err := os.ReadFile(filepath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading file:", err)
		os.Exit(1)
	}
	return input
}

func handleNoArgs() ([]byte, string) {
	if !stdinHasData() {
		fmt.Fprintln(os.Stderr, "Usage: cat data.json | jt [selector]")
		fmt.Fprintln(os.Stderr, "       jt <file> [selector]")
		os.Exit(1)
	}
	return readStdin(), "."
}

func handleOneArg(arg string) ([]byte, string) {
	if isFile(arg) {
		return readFile(arg), "."
	}
	if isSelector(arg) {
		if !stdinHasData() {
			fmt.Fprintln(os.Stderr, "Error: selector provided but no data piped to stdin")
			os.Exit(1)
		}
		return readStdin(), arg
	}
	fmt.Fprintf(os.Stderr, "Error: file not found: %s\n", arg)
	os.Exit(1)
	return nil, "" // Unreachable
}

func handleTwoOrMoreArgs(args []string) ([]byte, string) {
	return readFile(args[0]), args[1]
}

func readInput() ([]byte, string) {
	args := flag.Args()
	var input []byte
	var selector string

	switch len(args) {
	case 0:
		input, selector = handleNoArgs()
	case 1:
		input, selector = handleOneArg(args[0])
	default: // 2 or more
		input, selector = handleTwoOrMoreArgs(args)
	}

	if len(input) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no data to process")
		os.Exit(1)
	}

	return input, selector
}

func parseInput(input []byte) (interface{}, bool) {
	var data interface{}
	if err := json.Unmarshal(input, &data); err == nil {
		return data, false
	}

	if xmlData, err := parseXML(input); err == nil {
		return xmlData, false
	}

	decoder := yaml.NewDecoder(bytes.NewReader(input))
	var documents []interface{}
	for {
		var doc interface{}
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintln(os.Stderr, "Error: Input is not valid JSON or YAML.")
			os.Exit(1)
		}
		documents = append(documents, doc)
	}

	if len(documents) == 0 {
		return map[string]interface{}{}, false
	}

	if len(documents) == 1 {
		return documents[0], false
	}

	return documents, true
}

func applySelector(data interface{}, selector string) interface{} {
	if selector == "." {
		return data
	}

	if docs, ok := data.([]interface{}); ok {
		trimmedSelector := strings.TrimPrefix(selector, ".")
		if !strings.HasPrefix(trimmedSelector, "[") {
			var results []interface{}
			for _, doc := range docs {
				results = append(results, applySelector(doc, selector))
			}
			return results
		}
	}

	// Normalize selector to handle array indexing
	selector = strings.ReplaceAll(strings.TrimPrefix(selector, "."), "[", ".[")
	path := strings.Split(selector, ".")

	current := data
	fullPath := ""
	for _, key := range path {
		if key == "" {
			continue
		}

		if fullPath == "" {
			fullPath = key
		} else {
			fullPath += "." + key
		}

		if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") {
			indexStr := strings.Trim(key, "[]")
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid array index '%s' in path '%s'\n", indexStr, fullPath)
				os.Exit(1)
			}

			arr, ok := current.([]interface{})
			if !ok {
				fmt.Fprintf(os.Stderr, "Error: cannot index into non-array at path '%s'\n", fullPath)
				os.Exit(1)
			}

			if index < 0 || index >= len(arr) {
				fmt.Fprintf(os.Stderr, "Error: index %d out of bounds for array at path '%s'\n", index, fullPath)
				os.Exit(1)
			}
			current = arr[index]
		} else {
			m, ok := current.(map[string]interface{})
			if !ok {
				fmt.Fprintf(os.Stderr, "Error: cannot traverse into non-object at path '%s'\n", fullPath)
				os.Exit(1)
			}

			val, exists := m[key]
			if !exists {
				fmt.Fprintf(os.Stderr, "Error: key '%s' not found in path '%s'\n", key, fullPath)
				os.Exit(1)
			}
			current = val
		}
	}

	return current
}

func render(data interface{}, format string, details bool, maxWidth int, isMultiDoc bool) {
	var output string
	docs, isSlice := data.([]interface{})

	if isMultiDoc && isSlice {
		var outputs []string
		for _, doc := range docs {
			outputs = append(outputs, renderRecursive(doc, details, format, maxWidth))
		}
		output = strings.Join(outputs, "\n")
	} else {
		output = renderRecursive(data, details, format, maxWidth)
	}

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
			plainLines := make([]string, len(lines))
			for i, line := range lines {
				plainLines[i] = stripANSI(line)
			}

			ti := textinput.New()
			ti.Placeholder = "Type to search..."
			ti.CharLimit = 100

			m := model{
				content:      lines,
				plainContent: plainLines,
				contentWidth: contentWidth,
				searchInput:  ti,
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
		handleSlice(table, v, details, format, maxWidth, useColor)
	case map[string]interface{}:
		handleMap(table, v, details, format, maxWidth, useColor)
	default:
		table.Append([]string{"value", truncateValue(fmt.Sprintf("%v", v), maxWidth)})
	}
}

func handleSlice(table *tablewriter.Table, v []interface{}, details bool, format string, maxWidth int, useColor bool) {
	if details {
		table.Caption(tw.Caption{Text: fmt.Sprintf("[-] array, %d items", len(v))})
	}
	if len(v) == 0 {
		return
	}

	headers := buildHeaders(v)
	table.Header(headers)

	for i, item := range v {
		if m, ok := item.(map[string]interface{}); ok {
			row := []string{}

			// Add index column with styling
			if useColor {
				row = append(row, keyStyle.Render(fmt.Sprintf("%d", i)))
			} else if format == "html" {
				row = append(row, fmt.Sprintf(`<span class="jt-key">%d</span>`, i))
			} else {
				row = append(row, fmt.Sprintf("%d", i))
			}

			// Add value columns with styling
			for _, key := range headers[1:] {
				val := m[key]
				value := formatValue(val, details, format, maxWidth)

				if useColor {
					row = append(row, getStyle(val).Render(value))
				} else if format == "html" {
					cssClass := getHTMLClass(val)
					row = append(row, fmt.Sprintf(`<span class="%s">%s</span>`, cssClass, value))
				} else {
					row = append(row, value)
				}
			}
			table.Append(row)
		} else {
			value := formatValue(item, details, format, maxWidth)
			appendRow(table, fmt.Sprintf("%d", i), value, item, useColor, format)
		}
	}
}

func handleMap(table *tablewriter.Table, v map[string]interface{}, details bool, format string, maxWidth int, useColor bool) {
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
}

func buildHeaders(v []interface{}) []string {
	headers := []string{"[key]"}
	if first, ok := v[0].(map[string]interface{}); ok {
		keys := make([]string, 0, len(first))
		for k := range first {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		headers = append(headers, keys...)
	}
	return headers
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
