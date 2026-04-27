package main

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/euraika-labs/pan-agent/internal/marketplace"
	"github.com/euraika-labs/pan-agent/internal/paths"
)

// cmdSkill dispatches the `pan-agent skill <action> ...` subcommands.
//
// Producer-side (publishing a bundle):
//
//	keygen   Generate a publisher keypair, save seed to the profile.
//	build    Sign a directory tree into a marketplace bundle.
//
// Consumer-side (installing / inspecting a bundle):
//
//	verify   Validate signature + manifest layout, print metadata.
//	trust    list / pin / unpin pinned publishers.
func cmdSkill(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(
			"missing skill action — usage: pan-agent skill [verify|build|keygen|trust]")
	}
	action := args[0]
	rest := args[1:]
	switch action {
	case "verify":
		return cmdSkillVerify(rest)
	case "build":
		return cmdSkillBuild(rest)
	case "keygen":
		return cmdSkillKeygen(rest)
	case "trust":
		return cmdSkillTrust(rest)
	default:
		return fmt.Errorf("unknown skill action %q — usage: pan-agent skill [verify|build|keygen|trust]",
			action)
	}
}

// cmdSkillVerify validates a marketplace bundle on disk: parses the
// manifest, checks the signature, hashes every declared file, and
// reports the publisher fingerprint. Does NOT install — useful as a
// "what's in this bundle" command for power users who want to
// inspect a downloaded bundle before posting it to /v1/marketplace/install.
//
// Trust-set behaviour:
//
//   - With --strict: load the profile's trust file and require the
//     bundle's publisher to be pinned. Mirrors the install path.
//   - Without --strict (default): nil trust set, so the signature is
//     verified but ANY publisher is accepted. The publisher
//     fingerprint is printed so a curious user can decide whether
//     to pin it next.
func cmdSkillVerify(args []string) error {
	fs := flag.NewFlagSet("skill verify", flag.ContinueOnError)
	profile := fs.String("profile", "", "Profile name for trust-set lookup (--strict only)")
	strict := fs.Bool("strict", false, "Require the publisher to be in the profile's trust set")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf(
			"missing bundle path\n\nUsage: pan-agent skill verify [--strict] [--profile P] <bundle-path>")
	}
	bundlePath := fs.Arg(0)

	var trusted []ed25519.PublicKey
	if *strict {
		_, pubs, err := marketplace.LoadTrustSet(paths.MarketplaceTrustFile(*profile))
		if err != nil {
			return fmt.Errorf("load trust set: %w", err)
		}
		trusted = pubs
	}

	b, err := marketplace.LoadBundle(bundlePath, trusted)
	if err != nil {
		switch {
		case errors.Is(err, marketplace.ErrSignatureInvalid):
			return fmt.Errorf("verify failed: bundle signature invalid: %w", err)
		case errors.Is(err, marketplace.ErrUntrustedPublisher):
			return fmt.Errorf("verify failed: publisher not in trust set: %w", err)
		case errors.Is(err, marketplace.ErrBundleInvalid):
			return fmt.Errorf("verify failed: bundle layout invalid: %w", err)
		default:
			return fmt.Errorf("verify failed: %w", err)
		}
	}

	pub, _ := marketplace.ParsePublicKey(b.Manifest.PublicKeyHex)
	fmt.Printf("OK — bundle verified\n")
	fmt.Printf("  Schema:      %s\n", b.Manifest.Schema)
	fmt.Printf("  Name:        %s\n", b.Manifest.Name)
	fmt.Printf("  Version:     %s\n", b.Manifest.Version)
	if b.Manifest.Author != "" {
		fmt.Printf("  Author:      %s\n", b.Manifest.Author)
	}
	if b.Manifest.Description != "" {
		fmt.Printf("  Description: %s\n", b.Manifest.Description)
	}
	fmt.Printf("  Publisher:   %s\n", marketplace.FingerprintOf(pub))
	fmt.Printf("  Files:       %d\n", len(b.Manifest.Files))
	if *strict {
		fmt.Printf("  Trust mode:  strict (publisher pinned)\n")
	} else {
		fmt.Printf("  Trust mode:  permissive (signature only)\n")
	}
	return nil
}

// cmdSkillTrust dispatches the trust-set management actions.
//
//	pan-agent skill trust list                    — show pinned publishers
//	pan-agent skill trust pin <pubkey-hex> [name] — add a publisher
//	pan-agent skill trust unpin <fingerprint>     — remove by fingerprint
func cmdSkillTrust(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(
			"missing trust action — usage: pan-agent skill trust [list|pin|unpin]")
	}
	action := args[0]
	rest := args[1:]
	switch action {
	case "list":
		return cmdSkillTrustList(rest)
	case "pin":
		return cmdSkillTrustPin(rest)
	case "unpin":
		return cmdSkillTrustUnpin(rest)
	default:
		return fmt.Errorf("unknown trust action %q — usage: pan-agent skill trust [list|pin|unpin]",
			action)
	}
}

func cmdSkillTrustList(args []string) error {
	fs := flag.NewFlagSet("skill trust list", flag.ContinueOnError)
	profile := fs.String("profile", "", "Profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ts, _, err := marketplace.LoadTrustSet(paths.MarketplaceTrustFile(*profile))
	if err != nil {
		return fmt.Errorf("load trust set: %w", err)
	}
	if len(ts.Publishers) == 0 {
		fmt.Println("(no pinned publishers)")
		return nil
	}
	for _, p := range ts.Publishers {
		fmt.Printf("%s  %s", p.Fingerprint, p.PublicKey)
		if p.Name != "" {
			fmt.Printf("  %s", p.Name)
		}
		fmt.Println()
	}
	return nil
}

func cmdSkillTrustPin(args []string) error {
	fs := flag.NewFlagSet("skill trust pin", flag.ContinueOnError)
	profile := fs.String("profile", "", "Profile name")
	name := fs.String("name", "", "Human-readable label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf(
			"missing public key\n\nUsage: pan-agent skill trust pin [--name N] [--profile P] <pubkey-hex>")
	}
	pub, err := marketplace.ParsePublicKey(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("public_key: %w", err)
	}
	path := paths.MarketplaceTrustFile(*profile)
	ts, _, err := marketplace.LoadTrustSet(path)
	if err != nil {
		return fmt.Errorf("load trust set: %w", err)
	}
	entry, added := marketplace.PinPublisher(ts, pub, *name, nowUnix())
	if !added {
		fmt.Printf("Already pinned: %s\n", entry.Fingerprint)
		return nil
	}
	if err := marketplace.SaveTrustSet(path, ts); err != nil {
		return fmt.Errorf("save trust set: %w", err)
	}
	fmt.Printf("Pinned: %s\n", entry.Fingerprint)
	return nil
}

func cmdSkillTrustUnpin(args []string) error {
	fs := flag.NewFlagSet("skill trust unpin", flag.ContinueOnError)
	profile := fs.String("profile", "", "Profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf(
			"missing fingerprint\n\nUsage: pan-agent skill trust unpin [--profile P] <fingerprint>")
	}
	fp := fs.Arg(0)
	path := paths.MarketplaceTrustFile(*profile)
	ts, _, err := marketplace.LoadTrustSet(path)
	if err != nil {
		return fmt.Errorf("load trust set: %w", err)
	}
	idx := -1
	for i, p := range ts.Publishers {
		if p.Fingerprint == fp {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("fingerprint %q not pinned", fp)
	}
	ts.Publishers = append(ts.Publishers[:idx], ts.Publishers[idx+1:]...)
	if err := marketplace.SaveTrustSet(path, ts); err != nil {
		return fmt.Errorf("save trust set: %w", err)
	}
	fmt.Printf("Unpinned: %s\n", fp)
	return nil
}

// nowUnix is split out so tests can stub time without touching every
// pin call site. Production stays one-line; tests override the
// package var if they need deterministic timestamps.
var nowUnix = func() int64 {
	return time.Now().Unix()
}
