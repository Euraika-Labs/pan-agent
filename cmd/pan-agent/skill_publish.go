package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/euraika-labs/pan-agent/internal/marketplace"
	"github.com/euraika-labs/pan-agent/internal/paths"
)

// Producer-side CLI for WS#13.C marketplace bundles. Pairs with
// the consumer-side verify/trust commands in skill.go.
//
//	pan-agent skill keygen [--profile P] [--force]
//	  Mint an Ed25519 publisher keypair, save the seed (hex,
//	  mode 0o600) under MarketplacePublisherSeedFile(profile),
//	  print the public-key hex + fingerprint to stdout. --force
//	  overwrites an existing seed file.
//
//	pan-agent skill build <dir> [--profile P] [--name N]
//	                            [--version V] [--author A]
//	                            [--description D]
//	  Walk <dir>, hash every file, write a signed manifest.json
//	  in place. The seed file from keygen is consumed implicitly;
//	  the resulting bundle is ready to ship to a marketplace +
//	  install via `pan-agent skill verify` / the install endpoint.

// cmdSkillKeygen mints a fresh publisher keypair and persists the
// seed to disk. The producer keeps the seed; the public-key hex
// is what they publish + ask consumers to pin.
func cmdSkillKeygen(args []string) error {
	fs := flag.NewFlagSet("skill keygen", flag.ContinueOnError)
	profile := fs.String("profile", "", "Profile name")
	force := fs.Bool("force", false, "Overwrite an existing seed file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path := paths.MarketplacePublisherSeedFile(*profile)
	if !*force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf(
				"seed file already exists at %s — pass --force to overwrite (this invalidates every bundle previously signed by this key)",
				path)
		}
	}

	kp, err := marketplace.GenerateKeypair()
	if err != nil {
		return fmt.Errorf("generate keypair: %w", err)
	}
	defer kp.ZeroPrivate()

	seedHex := hex.EncodeToString(kp.Seed())
	if err := os.WriteFile(path, []byte(seedHex), 0o600); err != nil {
		return fmt.Errorf("write seed file: %w", err)
	}

	fmt.Printf("Publisher keypair generated.\n")
	fmt.Printf("  Public key:  %s\n", kp.PublicKeyHex())
	fmt.Printf("  Fingerprint: %s\n", kp.Fingerprint())
	fmt.Printf("  Seed file:   %s\n", path)
	fmt.Printf("\n")
	fmt.Printf("Pin this publisher on consumer machines via:\n")
	fmt.Printf("  pan-agent skill trust pin --name <label> %s\n", kp.PublicKeyHex())
	return nil
}

// cmdSkillBuild walks a directory, signs the resulting manifest, and
// writes it in place. Consumes the seed file from keygen.
func cmdSkillBuild(args []string) error {
	fs := flag.NewFlagSet("skill build", flag.ContinueOnError)
	profile := fs.String("profile", "", "Profile name (locates the seed file)")
	name := fs.String("name", "", "Skill name (overrides any in SKILL.md frontmatter)")
	version := fs.String("version", "", "Semver version (required)")
	author := fs.String("author", "", "Author label")
	description := fs.String("description", "", "Description (overrides SKILL.md frontmatter)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf(
			"missing source directory — usage: pan-agent skill build [--name N --version V] <dir>")
	}
	if *version == "" {
		return fmt.Errorf("--version required")
	}
	src := fs.Arg(0)

	kp, err := loadPublisherKeypair(*profile)
	if err != nil {
		return err
	}
	defer kp.ZeroPrivate()

	// If --name wasn't supplied, fall back to the directory's basename
	// — a hand-authored bundle dir is conventionally named after the
	// skill it contains, so this is usually right.
	if *name == "" {
		st, err := os.Stat(src)
		if err == nil && st.IsDir() {
			*name = filepathBase(src)
		}
	}
	if *name == "" {
		return fmt.Errorf("--name required (could not infer from path %q)", src)
	}

	m, err := marketplace.WriteBundle(src, *name, *version, *author, *description, kp,
		marketplace.BuildOptions{Skip: marketplace.SkipDotfilesAndBuildArtefacts})
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}

	fmt.Printf("Bundle built + signed.\n")
	fmt.Printf("  Name:        %s\n", m.Name)
	fmt.Printf("  Version:     %s\n", m.Version)
	if m.Author != "" {
		fmt.Printf("  Author:      %s\n", m.Author)
	}
	fmt.Printf("  Files:       %d\n", len(m.Files))
	fmt.Printf("  Publisher:   %s\n", kp.Fingerprint())
	fmt.Printf("  Manifest:    %s/manifest.json\n", src)
	return nil
}

// loadPublisherKeypair reads the producer's seed file from disk and
// reconstructs the keypair. Returns a clear error when the seed
// file is missing so the user knows to run keygen first.
func loadPublisherKeypair(profile string) (*marketplace.Keypair, error) {
	path := paths.MarketplacePublisherSeedFile(profile)
	body, err := os.ReadFile(path) //nolint:gosec // path is paths.MarketplacePublisherSeedFile, not user-controlled
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf(
				"no publisher seed at %s — run `pan-agent skill keygen` first",
				path)
		}
		return nil, fmt.Errorf("read seed: %w", err)
	}
	seed, err := hex.DecodeString(string(body))
	if err != nil {
		return nil, fmt.Errorf("seed file %s is corrupt: %w", path, err)
	}
	kp, err := marketplace.FromSeed(seed)
	if err != nil {
		return nil, fmt.Errorf("seed file %s: %w", path, err)
	}
	return kp, nil
}

// filepathBase is filepath.Base inlined to avoid pulling path/filepath
// at the top of this file just for one call. The skill.go file
// already imports filepath transitively, but this keeps the import
// list here minimal + makes the unit test for filepathBase trivial.
func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
