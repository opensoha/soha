package sohacli

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

func runCompletion(args []string, rt Runtime) error {
	shell := "bash"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		shell = strings.TrimSpace(args[0])
	}
	switch shell {
	case "bash":
		_, err := fmt.Fprint(rt.Out, buildBashCompletionScript())
		return err
	case "zsh":
		_, err := fmt.Fprint(rt.Out, "#compdef soha\n"+buildBashCompletionScript())
		return err
	case "fish":
		_, err := fmt.Fprint(rt.Out, buildFishCompletionScript())
		return err
	case "powershell", "pwsh":
		_, err := fmt.Fprint(rt.Out, buildPowerShellCompletionScript())
		return err
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}

func buildFishCompletionScript() string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "complete -c soha -f")
	for _, spec := range topLevelCommandSpecs {
		fmt.Fprintf(&buf, "complete -c soha -n '__fish_use_subcommand' -a '%s' -d '%s'\n", fishQuote(joinWords(append([]string{spec.Name}, spec.Aliases...))), fishQuote(spec.Summary))
	}
	paths := completionPaths()
	for _, path := range completionPathNames(paths) {
		parts := strings.Fields(path)
		fmt.Fprintf(&buf, "complete -c soha -n '__fish_seen_subcommand_from %s' -a '%s'\n", parts[len(parts)-1], fishQuote(joinWords(paths[path])))
	}
	return buf.String()
}

func buildPowerShellCompletionScript() string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "Register-ArgumentCompleter -Native -CommandName soha -ScriptBlock {")
	fmt.Fprintln(&buf, "  param($wordToComplete, $commandAst, $cursorPosition)")
	fmt.Fprintln(&buf, "  $tokens = @($commandAst.CommandElements | ForEach-Object { $_.Extent.Text })")
	fmt.Fprintln(&buf, "  if ($wordToComplete -and $tokens.Count -gt 1) { $tokens = @($tokens[0..($tokens.Count - 2)]) }")
	fmt.Fprintln(&buf, "  $path = if ($tokens.Count -gt 1) { $tokens[1..($tokens.Count - 1)] -join ' ' } else { '' }")
	fmt.Fprintln(&buf, "  $items = switch ($path) {")
	paths := completionPaths()
	for _, path := range completionPathNames(paths) {
		fmt.Fprintf(&buf, "    '%s' { @('%s') }\n", powerShellQuote(path), strings.Join(powerShellQuoteWords(paths[path]), "', '"))
	}
	fmt.Fprintf(&buf, "    default { @('%s') }\n", strings.Join(powerShellQuoteWords(topLevelCommandNames(true)), "', '"))
	fmt.Fprintln(&buf, "  }")
	fmt.Fprintln(&buf, "  $items | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }")
	fmt.Fprintln(&buf, "}")
	return buf.String()
}

func completionPaths() map[string][]string {
	paths := make(map[string][]string)
	var visit func(string, commandSpec)
	visit = func(parent string, spec commandSpec) {
		path := strings.TrimSpace(parent + " " + spec.Name)
		if words := spec.completionWords(); len(words) > 0 {
			paths[path] = words
		}
		for _, child := range spec.Subcommands {
			visit(path, child)
		}
	}
	for _, spec := range topLevelCommandSpecs {
		visit("", spec)
	}
	return paths
}

func completionPathNames(paths map[string][]string) []string {
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fishQuote(value string) string {
	return strings.ReplaceAll(value, "'", "\\'")
}

func powerShellQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func powerShellQuoteWords(words []string) []string {
	out := make([]string, len(words))
	for i, word := range words {
		out[i] = powerShellQuote(word)
	}
	return out
}

func buildBashCompletionScript() string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "#!/usr/bin/env bash")
	fmt.Fprintln(&buf, "_soha_completion() {")
	fmt.Fprintln(&buf, "  local cur prev commands")
	fmt.Fprintln(&buf, "  COMPREPLY=()")
	fmt.Fprintln(&buf, "  cur=\"${COMP_WORDS[COMP_CWORD]}\"")
	fmt.Fprintln(&buf, "  prev=\"${COMP_WORDS[COMP_CWORD-1]}\"")
	fmt.Fprintf(&buf, "  commands=%q\n", joinWords(topLevelCommandNames(true)))
	fmt.Fprintln(&buf, "  case \"${COMP_WORDS[1]}\" in")
	for _, spec := range topLevelCommandSpecs {
		words := spec.completionWords()
		if len(words) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "    %s)\n", spec.Name)
		writeCompletionSubcommands(&buf, spec)
		fmt.Fprintf(&buf, "      COMPREPLY=($(compgen -W %q -- \"$cur\"))\n", joinWords(words))
		fmt.Fprintln(&buf, "      return 0")
		fmt.Fprintln(&buf, "      ;;")
	}
	fmt.Fprintln(&buf, "  esac")
	fmt.Fprintln(&buf, "  COMPREPLY=($(compgen -W \"$commands\" -- \"$cur\"))")
	fmt.Fprintln(&buf, "}")
	fmt.Fprintln(&buf, "complete -F _soha_completion soha")
	return buf.String()
}

func writeCompletionSubcommands(buf *bytes.Buffer, spec commandSpec) {
	if len(spec.Subcommands) == 0 {
		return
	}
	for _, subcommand := range spec.Subcommands {
		words := subcommand.completionWords()
		if len(words) == 0 {
			continue
		}
		fmt.Fprintf(buf, "      if [[ \"${COMP_WORDS[2]}\" == %q ]]; then\n", subcommand.Name)
		fmt.Fprintf(buf, "        COMPREPLY=($(compgen -W %q -- \"$cur\"))\n", joinWords(words))
		fmt.Fprintln(buf, "        return 0")
		fmt.Fprintln(buf, "      fi")
	}
}
