package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/tools"
)

// cmdTools dispatches the `pan-agent tools <action>` subcommands.
//
//	pan-agent tools list           List every registered tool.
//	pan-agent tools describe <n>   Print the JSON Schema parameters for a tool.
//
// "tools" is a sibling of "skill" — both are introspection surfaces
// for power users who don't want to spin up the desktop UI.
func cmdTools(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(
			"missing tools action — usage: pan-agent tools [list|describe]")
	}
	action := args[0]
	rest := args[1:]
	switch action {
	case "list":
		return cmdToolsList(rest)
	case "describe":
		return cmdToolsDescribe(rest)
	default:
		return fmt.Errorf("unknown tools action %q — usage: pan-agent tools [list|describe]",
			action)
	}
}

// cmdToolsList enumerates registered tools and prints
// "<name>\t<description>" lines, sorted by name. --json emits a
// machine-readable array for scripting.
func cmdToolsList(args []string) error {
	fs := flag.NewFlagSet("tools list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON array instead of plain text")
	if err := fs.Parse(args); err != nil {
		return err
	}

	all := tools.All()
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	if *jsonOut {
		type toolView struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		out := make([]toolView, 0, len(names))
		for _, n := range names {
			t := all[n]
			out = append(out, toolView{
				Name: t.Name(), Description: t.Description(),
			})
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	if len(names) == 0 {
		fmt.Println("(no tools registered)")
		return nil
	}
	for _, n := range names {
		t := all[n]
		// Single-line description: collapse internal newlines + trim
		// so the columnar layout stays clean. Long descriptions are
		// truncated to 100 chars with " …" so the user knows there's
		// more to see (via `tools describe`).
		desc := strings.ReplaceAll(t.Description(), "\n", " ")
		desc = strings.Join(strings.Fields(desc), " ")
		const max = 100
		if len(desc) > max {
			desc = desc[:max] + " …"
		}
		fmt.Printf("%-22s  %s\n", n, desc)
	}
	return nil
}

// cmdToolsDescribe prints the full description + JSON Schema
// Parameters block for one tool. Useful when wiring up a new
// invocation site or debugging a "tool said X failed" loop.
func cmdToolsDescribe(args []string) error {
	fs := flag.NewFlagSet("tools describe", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf(
			"missing tool name — usage: pan-agent tools describe <name>")
	}
	name := fs.Arg(0)
	t, ok := tools.Get(name)
	if !ok {
		return fmt.Errorf("no such tool %q (try `pan-agent tools list`)", name)
	}

	// Pretty-print the schema if it's valid JSON; fall back to
	// raw output otherwise (a bug in the tool author's schema
	// shouldn't break the CLI).
	var pretty interface{}
	rawSchema := t.Parameters()
	if err := json.Unmarshal(rawSchema, &pretty); err == nil {
		if b, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			rawSchema = b
		}
	}

	fmt.Printf("Tool:        %s\n", t.Name())
	fmt.Printf("Description: %s\n", t.Description())
	fmt.Printf("\nParameters (JSON Schema):\n%s\n", rawSchema)
	return nil
}
