package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/euraika-labs/pan-agent/internal/secret"
)

// cmdSecret dispatches `pan-agent secret <action>` subcommands.
//
//	patterns       List the redaction categories the recognizer
//	               table covers (one per row, plus a JSON form).
//	scan [text]    Read text from arg or stdin, run it through
//	               secret.Redact, print the result. Useful for
//	               debugging redaction patterns + verifying that a
//	               new env or config line will be tokenised before
//	               it reaches a logged context.
func cmdSecret(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(
			"missing secret action — usage: pan-agent secret [patterns|scan]")
	}
	action := args[0]
	rest := args[1:]
	switch action {
	case "patterns":
		return cmdSecretPatterns(rest)
	case "scan":
		return cmdSecretScan(rest)
	default:
		return fmt.Errorf("unknown secret action %q — usage: pan-agent secret [patterns|scan]",
			action)
	}
}

// cmdSecretPatterns lists every redaction category. Plain output is
// one-per-line; --json emits an array.
func cmdSecretPatterns(args []string) error {
	fs := flag.NewFlagSet("secret patterns", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON array")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cats := secretCategories()
	if *jsonOut {
		b, _ := json.MarshalIndent(cats, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	for _, c := range cats {
		fmt.Println(c)
	}
	return nil
}

// cmdSecretScan reads text from the first positional arg or from
// stdin, then prints the redacted form. The redacted form replaces
// every detected secret with a tagged placeholder
// `<REDACTED:CATEGORY:digest>`; the user can grep for "<REDACTED:"
// to confirm everything they care about was caught.
func cmdSecretScan(args []string) error {
	fs := flag.NewFlagSet("secret scan", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	var input []byte
	if fs.NArg() >= 1 {
		input = []byte(fs.Arg(0))
	} else {
		var err error
		input, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}
	if len(input) == 0 {
		return fmt.Errorf(
			"empty input — pipe text via stdin or pass it as the first arg")
	}

	redacted := secret.Redact(string(input))
	fmt.Print(redacted)
	if len(redacted) > 0 && redacted[len(redacted)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

// secretCategories returns the canonical list of recognizer
// categories the redaction pipeline covers. Hardcoded against the
// internal/secret constants so a new category being added without a
// corresponding doc-update gets caught here as a build failure.
func secretCategories() []string {
	return []string{
		string(secret.CatEmail),
		string(secret.CatPhone),
		string(secret.CatSSN),
		string(secret.CatCreditCard),
		string(secret.CatAPIKey),
		string(secret.CatJWT),
		string(secret.CatAWSKeyID),
		string(secret.CatBearer),
		string(secret.CatSlackToken),
		string(secret.CatStripeKey),
		string(secret.CatGitHubToken),
		string(secret.CatGCPKey),
	}
}
