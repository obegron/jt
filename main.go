package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
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

func main() {
	format := flag.String("format", "table", "Output format (table, markdown, html, svg)")
	details := flag.Bool("d", false, "Show details (caption)")
	flag.Parse()

	var input []byte
	var err error
	var selector string

	args := flag.Args()
	if len(args) == 0 || (len(args) == 1 && strings.HasPrefix(args[0], ".")) {
		// Read from stdin
		info, err := os.Stdin.Stat()
		if err != nil {
			fmt.Println("Error reading from stdin:", err)
			os.Exit(1)
		}
		if (info.Mode() & os.ModeCharDevice) == 0 {
			input, err = io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Println("Error reading from stdin:", err)
				os.Exit(1)
			}
		}
	} else {
		// Read from file
		input, err = os.ReadFile(args[0])
		if err != nil {
			fmt.Println("Error reading file:", err)
			os.Exit(1)
		}
	}

	if len(args) > 0 && strings.HasPrefix(args[len(args)-1], ".") {
		selector = args[len(args)-1]
	} else {
		selector = "."
	}

	if len(input) == 0 {
		fmt.Println("Usage: cat data.json | jt [selector]")
		fmt.Println("       jt <file> [selector]")
		os.Exit(1)
	}

	var data interface{}
	if err := json.Unmarshal(input, &data); err != nil {
		if err := yaml.Unmarshal(input, &data); err != nil {
			fmt.Println("Error: Input is not valid JSON or YAML.")
			os.Exit(1)
		}
	}

	// Apply selector
	if selector != "." {
		key := strings.TrimPrefix(selector, ".")
		if m, ok := data.(map[string]interface{}); ok {
			if val, exists := m[key]; exists {
				data = val
			} else {
				fmt.Printf("Error: key '%s' not found\n", key)
				os.Exit(1)
			}
		} else {
			fmt.Println("Error: selector can only be applied to a map.")
			os.Exit(1)
		}
	}

	render(data, *format, *details)
}

func render(data interface{}, format string, details bool) {
	fmt.Println(renderRecursive(data, true, details, format))
}

func renderRecursive(data interface{}, isRoot bool, details bool, format string) string {
	var buf bytes.Buffer
	var table *tablewriter.Table

	switch format {
	case "table":
		table = tablewriter.NewTable(&buf,
			tablewriter.WithHeaderAlignment(tw.AlignLeft),
			tablewriter.WithRowAlignment(tw.AlignLeft),
			tablewriter.WithRendition(tw.Rendition{
				Borders: tw.Border{Left: tw.On, Right: tw.On, Top: tw.On, Bottom: tw.On},
				Settings: tw.Settings{
					Separators: tw.Separators{BetweenColumns: tw.On, BetweenRows: tw.On},
				},
			}),
		)
	case "markdown":
		table = tablewriter.NewTable(&buf, tablewriter.WithRenderer(renderer.NewMarkdown()))
	case "html":
		table = tablewriter.NewTable(&buf, tablewriter.WithRenderer(renderer.NewHTML(renderer.HTMLConfig{EscapeContent: true})))
	case "svg":
		table = tablewriter.NewTable(&buf, tablewriter.WithRenderer(renderer.NewSVG()))
	}

	appendData(table, data, true, details, format)
	table.Render()
	return buf.String()
}

func getStyle(val interface{}) lipgloss.Style {
	switch val.(type) {
	case bool:
		return boolStyle
	case string:
		return stringStyle
	case int:
		return intStyle
	}
	return keyStyle
}

func appendData(table *tablewriter.Table, data interface{}, recursive bool, details bool, format string) {
	isTerminal := isatty.IsTerminal(os.Stdout.Fd())

	switch v := data.(type) {
	case map[string]interface{}:
		if details {
			table.Caption(tw.Caption{Text: fmt.Sprintf("[-] Object, %d properties", len(v))})
		}
		for key, val := range v {
			value := ""
			if recursive {
				switch val.(type) {
				case map[string]interface{}, []interface{}:
					value = renderRecursive(val, false, details, format)
				default:
					value = fmt.Sprintf("%v", val)
				}
			} else {
				value = fmt.Sprintf("%v", val)
			}
			if isTerminal && format == "table" {
				table.Append([]string{keyStyle.Render(key), getStyle(val).Render(value)})
			} else {
				table.Append([]string{key, value})
			}
		}
	case []interface{}:
		if details {
			table.Caption(tw.Caption{Text: fmt.Sprintf("[-] Array, %d items", len(v))})
		}
		for i, item := range v {
			value := ""
			if recursive {
				switch item.(type) {
				case map[string]interface{}, []interface{}:
					value = renderRecursive(item, false, details, format)
				default:
					value = fmt.Sprintf("%v", item)
				}
			} else {
				value = fmt.Sprintf("%v", item)
			}
			if isTerminal && format == "table" {
				table.Append([]string{keyStyle.Render(fmt.Sprintf("%d", i)), getStyle(v).Render(value)})
			} else {
				table.Append([]string{fmt.Sprintf("%d", i), value})
			}
		}
	default:
		// For non-map/slice data, just print the value
		fmt.Println(data)
	}
}
