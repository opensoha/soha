package sohacli

import (
	"fmt"
	"io"
	"strings"
)

type commandDoc struct {
	Command  string
	Usage    string
	Purpose  string
	Examples []string
}

func runDocs(args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("docs", args, rt)
	format := fs.String("format", "markdown", "documentation output format: markdown")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "", "markdown":
		return writeCommandDocsMarkdown(rt.Out)
	default:
		return fmt.Errorf("unsupported docs format %q", *format)
	}
}

func writeCommandDocsMarkdown(destination io.Writer) error {
	out := newCheckedWriter(destination)
	out.Println("# Soha CLI Command Reference")
	out.Println()
	out.Println("Generated with `soha docs --format markdown`.")
	out.Println()
	out.Println("## Automation contract")
	out.Println()
	out.Println("Structured results are written to stdout. Diagnostics and prompts are written to stderr. Exit codes are stable: `0` success/help, `1` validation, execution, API, or terminal operation failure, `2` missing/unknown top-level command or malformed flag, and `130` interrupted execution.")
	out.Println()
	out.Println("| Command | Usage | Purpose | Examples |")
	out.Println("| --- | --- | --- | --- |")
	for _, doc := range commandDocsFromSpecs() {
		out.Printf(
			"| `%s` | `%s` | %s | %s |\n",
			markdownCell(doc.Command),
			markdownCell(doc.Usage),
			markdownCell(doc.Purpose),
			markdownExamples(doc.Examples),
		)
	}
	return out.Err()
}

func commandDocsFromSpecs() []commandDoc {
	docs := make([]commandDoc, 0, len(topLevelCommandSpecs))
	for _, spec := range topLevelCommandSpecs {
		docs = appendCommandDocs(docs, "", spec)
	}
	return docs
}

func appendCommandDocs(docs []commandDoc, parent string, spec commandSpec) []commandDoc {
	command := strings.TrimSpace(strings.TrimSpace(parent + " " + spec.Name))
	if strings.TrimSpace(spec.Usage) != "" {
		docs = append(docs, commandDoc{
			Command:  command,
			Usage:    spec.Usage,
			Purpose:  sentence(spec.Summary),
			Examples: append([]string(nil), spec.Examples...),
		})
	}
	for _, subcommand := range spec.Subcommands {
		docs = appendCommandDocs(docs, command, subcommand)
	}
	return docs
}

func sentence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasSuffix(value, ".") {
		return value
	}
	return value + "."
}

func markdownExamples(examples []string) string {
	if len(examples) == 0 {
		return ""
	}
	out := make([]string, 0, len(examples))
	for _, example := range examples {
		out = append(out, "`"+markdownCell(example)+"`")
	}
	return strings.Join(out, "<br>")
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", `\|`)
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}
