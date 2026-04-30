package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/euraika-labs/pan-agent/internal/config"
	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/version"
	"golang.org/x/term"
)

var errInputInterrupted = errors.New("input interrupted")

const (
	latestReleaseAPIURL = "https://api.github.com/repos/Euraika-Labs/pan-agent/releases/latest"
	latestReleaseURL    = "https://github.com/Euraika-Labs/pan-agent/releases/latest"
	updateCheckTTL      = 6 * time.Hour
)

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiBold   = "\x1b[1m"
	ansiAmber  = "\x1b[38;5;220m"
	ansiOrange = "\x1b[38;5;172m"
	ansiBlue   = "\x1b[38;5;75m"
	ansiRed    = "\x1b[38;5;203m"
	ansiGray   = "\x1b[38;5;245m"
)

type cliConfig struct {
	Profile  string
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
	Launch   string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%serror:%s %v\n", ansiRed, ansiReset, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			printHelp()
			return nil
		case "version", "--version", "-V":
			printVersion()
			return nil
		}
	}

	subcommand := "chat"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		printCommandHelp(subcommand)
		return nil
	}
	if subcommand == "configure" {
		return cmdConfigure(args)
	}

	fs := flag.NewFlagSet("pan", flag.ContinueOnError)
	profile := fs.String("profile", "default", "Pan-Agent profile name")
	model := fs.String("model", "", "model override")
	modelShort := fs.String("m", "", "model override")
	provider := fs.String("provider", "", "provider override")
	oneshot := fs.String("oneshot", "", "send one prompt and print only the response")
	oneshotShort := fs.String("z", "", "send one prompt and print only the response")
	noClear := fs.Bool("no-clear", false, "do not clear the terminal on startup")
	versionFlag := fs.Bool("version", false, "show version and exit")
	versionShort := fs.Bool("V", false, "show version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *versionFlag || *versionShort {
		printVersion()
		return nil
	}
	if fs.NArg() > 0 {
		if *oneshot == "" {
			*oneshot = strings.Join(fs.Args(), " ")
		}
	}

	modelOverride := firstNonEmpty(*model, *modelShort)
	cfg, err := resolveCLIConfig(*profile, modelOverride, *provider)
	if err != nil {
		return err
	}

	switch subcommand {
	case "chat":
		prompt := firstNonEmpty(*oneshot, *oneshotShort)
		if prompt != "" {
			return runOneshot(cfg, prompt)
		}
	case "model":
		printModel(cfg)
		return nil
	case "status":
		printStatus(cfg)
		return nil
	case "config":
		printConfig(cfg)
		return nil
	case "doctor":
		return runDoctor(cfg)
	case "update":
		return runUpdateCheck()
	case "help":
		printHelp()
		return nil
	default:
		if isPlannedCommand(subcommand) {
			fmt.Printf("pan %s is listed for compatibility, but is not implemented in the terminal CLI yet.\n", subcommand)
			fmt.Println("Use Pan Desktop for this workflow for now.")
			return nil
		}
		return fmt.Errorf("unknown command %q\n\nRun `pan help` for available commands", subcommand)
	}

	if !*noClear {
		clearScreen()
	}
	printShell(cfg, checkForUpdateNotice())
	return chatLoop(cfg)
}

func resolveCLIConfig(profile, modelOverride, providerOverride string) (cliConfig, error) {
	mc := config.GetModelConfig(profile)
	provider := mc.Provider
	if providerOverride != "" {
		provider = providerOverride
	}
	if provider == "" {
		provider = "auto"
	}

	model := mc.Model
	if modelOverride != "" {
		model = modelOverride
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	baseURL := mc.BaseURL
	if baseURL == "" && provider != "auto" {
		if u, err := llm.BaseURL(provider); err == nil {
			baseURL = u
		}
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	env, err := config.ReadProfileEnv(profile)
	if err != nil {
		return cliConfig{}, fmt.Errorf("read profile env: %w", err)
	}

	apiKey := firstNonEmpty(
		providerKey(env, provider),
		env["REGOLO_API_KEY"],
		env["OPENROUTER_API_KEY"],
		env["OPENAI_API_KEY"],
		env["ANTHROPIC_API_KEY"],
		env["GROQ_API_KEY"],
		env["API_KEY"],
		os.Getenv("REGOLO_API_KEY"),
		os.Getenv("OPENROUTER_API_KEY"),
		os.Getenv("OPENAI_API_KEY"),
		os.Getenv("ANTHROPIC_API_KEY"),
		os.Getenv("GROQ_API_KEY"),
		os.Getenv("API_KEY"),
	)

	return cliConfig{
		Profile:  profile,
		Provider: provider,
		Model:    model,
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Launch:   launchName(),
	}, nil
}

func launchName() string {
	exe, err := os.Executable()
	if err != nil {
		return "pan"
	}
	name := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	if name == "" || strings.EqualFold(name, "pan") {
		return "pan"
	}
	return name
}

func agentLabel(cfg cliConfig) string {
	if strings.EqualFold(cfg.Launch, "pan") {
		return "PAN"
	}
	return cfg.Launch
}

func providerKey(env map[string]string, provider string) string {
	switch strings.ToLower(provider) {
	case "regolo":
		return env["REGOLO_API_KEY"]
	case "openrouter":
		return env["OPENROUTER_API_KEY"]
	case "anthropic":
		return env["ANTHROPIC_API_KEY"]
	case "groq":
		return env["GROQ_API_KEY"]
	case "openai", "auto":
		return env["OPENAI_API_KEY"]
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func isPlannedCommand(command string) bool {
	switch command {
	case "setup", "gateway", "skills", "tools", "sessions", "logs":
		return true
	default:
		return false
	}
}

func cmdConfigure(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printCommandHelp("configure")
		return nil
	}
	if args[0] != "profile" {
		return fmt.Errorf("unknown configure target %q\n\nUsage: pan configure profile <name>", args[0])
	}
	if len(args) < 2 {
		return fmt.Errorf("missing alias name\n\nUsage: pan configure profile <name>")
	}
	alias := strings.TrimSpace(args[1])
	if err := validateAlias(alias); err != nil {
		return err
	}
	return configureAlias(alias)
}

func validateAlias(alias string) error {
	if alias == "" {
		return fmt.Errorf("alias name cannot be empty")
	}
	if strings.EqualFold(alias, "pan") || strings.EqualFold(alias, "pan.exe") {
		return fmt.Errorf("alias %q is reserved", alias)
	}
	for _, r := range alias {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("alias %q can only contain letters, numbers, dash, and underscore", alias)
	}
	return nil
}

func configureAlias(alias string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current pan executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve current pan executable: %w", err)
	}

	previous, _ := readAliasName()
	if previous != "" && !strings.EqualFold(previous, alias) {
		removeAlias(previous)
	}

	for _, dir := range aliasDirs() {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create alias dir %s: %w", dir, err)
		}
		target := filepath.Join(dir, alias+".exe")
		marker := target + ".pan-alias"
		if _, err := os.Stat(target); err == nil {
			_, markerErr := os.Stat(marker)
			same, sameErr := sameFileContent(exe, target)
			if markerErr != nil && (sameErr != nil || !same) {
				return fmt.Errorf("%s already exists and was not created by pan", target)
			}
		}
		if err := copyFile(exe, target); err != nil {
			return fmt.Errorf("install alias %s: %w", target, err)
		}
		if err := os.WriteFile(marker, []byte("pan alias\n"), 0644); err != nil {
			return fmt.Errorf("write alias marker %s: %w", marker, err)
		}
	}
	if err := writeAliasName(alias); err != nil {
		return err
	}
	fmt.Printf("Configured Pan Agent alias: %s\n", alias)
	fmt.Printf("You can now run `%s` or `pan` from a new terminal.\n", alias)
	return nil
}

func aliasDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dirs := []string{filepath.Join(home, ".local", "bin")}
	if _, err := os.Stat(filepath.Join(home, "bin")); err == nil {
		dirs = append(dirs, filepath.Join(home, "bin"))
	}
	return dirs
}

func aliasStateFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".pan-agent")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "cli-alias"), nil
}

func readAliasName() (string, error) {
	path, err := aliasStateFile()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func writeAliasName(alias string) error {
	path, err := aliasStateFile()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(alias+"\n"), 0644)
}

func removeAlias(alias string) {
	for _, dir := range aliasDirs() {
		target := filepath.Join(dir, alias+".exe")
		marker := target + ".pan-alias"
		if _, err := os.Stat(marker); err == nil {
			_ = os.Remove(target)
			_ = os.Remove(marker)
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func sameFileContent(a, b string) (bool, error) {
	ainfo, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	binfo, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	if ainfo.Size() != binfo.Size() {
		return false, nil
	}

	af, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer af.Close()

	bf, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer bf.Close()

	abuf := make([]byte, 32*1024)
	bbuf := make([]byte, 32*1024)
	for {
		an, aerr := af.Read(abuf)
		bn, berr := bf.Read(bbuf)
		if an != bn || string(abuf[:an]) != string(bbuf[:bn]) {
			return false, nil
		}
		if errors.Is(aerr, io.EOF) && errors.Is(berr, io.EOF) {
			return true, nil
		}
		if aerr != nil && !errors.Is(aerr, io.EOF) {
			return false, aerr
		}
		if berr != nil && !errors.Is(berr, io.EOF) {
			return false, berr
		}
	}
}

func printHelp() {
	fmt.Print(`usage: pan [-h] [--version] [-z PROMPT] [-m MODEL] [--provider PROVIDER] [--profile PROFILE] [--no-clear]
           {chat,model,status,config,configure,doctor,version,help,setup,gateway,skills,tools,sessions,logs,update} ...

Pan Agent - AI assistant with desktop and terminal chat interfaces

positional arguments:
  {chat,model,status,config,configure,doctor,version,help,setup,gateway,skills,tools,sessions,logs,update}
                        Command to run
    chat                Interactive terminal chat with Pan Agent
    model               Show the active model and provider
    status              Show local CLI/provider status
    config              Show active profile configuration paths and values
    configure           Configure terminal CLI behavior, including command aliases
    doctor              Check basic configuration and dependencies
    version             Show version information
    help                Show this help message
    setup               Open the desktop settings flow (not implemented in CLI yet)
    gateway             Messaging gateway management (use Pan Desktop for now)
    skills              Skills management (available in Pan Desktop)
    tools               Tool configuration (available in Pan Desktop)
    sessions            Session history management (available in Pan Desktop)
    logs                View logs (not implemented in CLI yet)
    update              Download and install the latest Pan Agent CLI release

options:
  -h, --help            show this help message and exit
  --version, -V         show version and exit
  -z, --oneshot PROMPT  one-shot mode: send a single prompt and print only the final response
  -m, --model MODEL     model override for this invocation
  --provider PROVIDER   provider override for this invocation
  --profile PROFILE     profile name to use (default: default)
  --no-clear            do not clear the terminal before interactive chat

Examples:
    pan                         Start interactive chat
    pan chat                    Start interactive chat
    pan -z "Hello"              Single prompt mode
    pan chat -z "Hello"         Single prompt mode through chat command
    pan -m gpt-4o-mini          Start chat with a model override
    pan --provider regolo       Start chat with a provider override
    pan model                   Show active model
    pan status                  Show CLI/provider status
    pan config                  Show active config
    pan configure profile hello Create or replace the terminal alias 'hello'
    pan doctor                  Run basic checks
    pan update                  Download and install the latest CLI release
    pan version                 Show version

For more help on a command:
    pan <command> --help
`)
}

func printCommandHelp(command string) {
	switch command {
	case "chat":
		fmt.Print(`usage: pan chat [-z PROMPT] [-m MODEL] [--provider PROVIDER] [--profile PROFILE] [--no-clear]

Interactive terminal chat with Pan Agent.

Examples:
    pan chat
    pan chat -z "Write a short haiku"
    pan chat -m gpt-oss-120b
`)
	case "model":
		fmt.Print("usage: pan model [--profile PROFILE]\n\nShows the active model, provider, and base URL.\n")
	case "status":
		fmt.Print("usage: pan status [--profile PROFILE]\n\nShows CLI status and whether a provider key is configured.\n")
	case "config":
		fmt.Print("usage: pan config [--profile PROFILE]\n\nShows the active profile configuration used by the CLI.\n")
	case "configure":
		fmt.Print(`usage: pan configure profile <name>

Creates or replaces a terminal alias for Pan Agent.

Examples:
    pan configure profile hello
    hello

Running the command again with another name removes the previous Pan-created
alias and installs the new one:
    pan configure profile agent
`)
	case "doctor":
		fmt.Print("usage: pan doctor [--profile PROFILE]\n\nRuns basic configuration checks.\n")
	case "update":
		fmt.Print("usage: pan update\n\nDownloads and installs the latest standalone Pan Agent CLI binary for this platform.\n")
	default:
		fmt.Printf("No detailed help for %q yet.\n\nRun `pan help` for the command list.\n", command)
	}
}

func printVersion() {
	fmt.Printf("pan %s (pan-agent %s, commit %s, built %s)\n", version.Version, version.Version, version.Commit, version.Date)
}

func printModel(cfg cliConfig) {
	fmt.Printf("model:    %s\n", cfg.Model)
	fmt.Printf("provider: %s\n", cfg.Provider)
	fmt.Printf("base_url: %s\n", cfg.BaseURL)
}

func printStatus(cfg cliConfig) {
	fmt.Println("Pan Agent CLI")
	fmt.Printf("version:  %s\n", version.Version)
	fmt.Printf("profile:  %s\n", cfg.Profile)
	fmt.Printf("provider: %s\n", cfg.Provider)
	fmt.Printf("model:    %s\n", cfg.Model)
	if cfg.APIKey == "" {
		fmt.Println("api_key:  missing")
	} else {
		fmt.Println("api_key:  configured")
	}
}

func printConfig(cfg cliConfig) {
	fmt.Printf("profile:  %s\n", cfg.Profile)
	fmt.Printf("provider: %s\n", cfg.Provider)
	fmt.Printf("model:    %s\n", cfg.Model)
	fmt.Printf("base_url: %s\n", cfg.BaseURL)
}

func runDoctor(cfg cliConfig) error {
	ok := true
	check := func(name string, pass bool, detail string) {
		status := "ok"
		if !pass {
			status = "fail"
			ok = false
		}
		fmt.Printf("%-24s %s", name, status)
		if detail != "" {
			fmt.Printf("  %s", detail)
		}
		fmt.Println()
	}

	check("profile", cfg.Profile != "", cfg.Profile)
	check("model", cfg.Model != "", cfg.Model)
	check("base_url", cfg.BaseURL != "", cfg.BaseURL)
	check("api_key", cfg.APIKey != "", "configured for remote providers")
	if !ok {
		return fmt.Errorf("doctor found configuration issues")
	}
	return nil
}

type updateInfo struct {
	LatestVersion string    `json:"latest_version"`
	URL           string    `json:"url"`
	AssetName     string    `json:"asset_name"`
	AssetURL      string    `json:"asset_url"`
	CheckedAt     time.Time `json:"checked_at"`
}

type githubRelease struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runUpdateCheck() error {
	fmt.Printf("%sUpdating Pan Agent...%s\n\n", ansiAmber, ansiReset)
	fmt.Println("-> Fetching latest release...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := fetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("could not check for updates: %w", err)
	}
	if !isVersionNewer(info.LatestVersion, version.Version) {
		fmt.Printf("Pan Agent is up to date (%s).\n", version.Version)
		return nil
	}
	if info.AssetURL == "" {
		return fmt.Errorf("Pan Agent %s is available, but no standalone CLI binary was found for %s/%s\nDownload it here: %s", info.LatestVersion, runtime.GOOS, runtime.GOARCH, info.URL)
	}
	fmt.Printf("-> Found Pan Agent %s\n", info.LatestVersion)
	fmt.Printf("-> Downloading %s...\n", info.AssetName)
	downloadCtx, cancelDownload := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancelDownload()
	path, err := downloadUpdateAsset(downloadCtx, info)
	if err != nil {
		return err
	}
	fmt.Println("-> Installing update...")
	if err := installDownloadedCLI(path); err != nil {
		return err
	}
	writeUpdateCache(info)
	return nil
}

func checkForUpdateNotice() string {
	info, err := cachedLatestRelease()
	if err != nil || !isVersionNewer(info.LatestVersion, version.Version) {
		return ""
	}
	return fmt.Sprintf("Pan Agent %s is available. Run `pan update` to update Pan Desktop and the terminal CLI.", info.LatestVersion)
}

func cachedLatestRelease() (updateInfo, error) {
	if info, ok := readUpdateCache(); ok {
		return info, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	info, err := fetchLatestRelease(ctx)
	if err != nil {
		return updateInfo{}, err
	}
	writeUpdateCache(info)
	return info, nil
}

func readUpdateCache() (updateInfo, bool) {
	path, err := updateCachePath()
	if err != nil {
		return updateInfo{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updateInfo{}, false
	}
	var info updateInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return updateInfo{}, false
	}
	if info.LatestVersion == "" || time.Since(info.CheckedAt) > updateCheckTTL {
		return updateInfo{}, false
	}
	return info, true
}

func writeUpdateCache(info updateInfo) {
	path, err := updateCachePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func updateCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pan-agent", "update-check.json"), nil
}

func fetchLatestRelease(ctx context.Context) (updateInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPIURL, nil)
	if err != nil {
		return updateInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pan-agent/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return updateInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return updateInfo{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var rel githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&rel); err != nil {
		return updateInfo{}, err
	}
	latest := normalizeVersion(rel.TagName)
	if latest == "" {
		return updateInfo{}, fmt.Errorf("latest release did not include a version tag")
	}
	url := rel.HTMLURL
	if url == "" {
		url = latestReleaseURL
	}
	assetName := releaseAssetName(latest)
	assetURL := ""
	for _, asset := range rel.Assets {
		if asset.Name == assetName {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}
	return updateInfo{
		LatestVersion: latest,
		URL:           url,
		AssetName:     assetName,
		AssetURL:      assetURL,
		CheckedAt:     time.Now(),
	}, nil
}

func releaseAssetName(releaseVersion string) string {
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			return fmt.Sprintf("pan-%s-windows-x64.exe", releaseVersion)
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			return fmt.Sprintf("pan-%s-linux-x64", releaseVersion)
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return fmt.Sprintf("pan-%s-macos-arm64", releaseVersion)
		case "amd64":
			return fmt.Sprintf("pan-%s-macos-x64", releaseVersion)
		}
	}
	return ""
}

func downloadUpdateAsset(ctx context.Context, info updateInfo) (string, error) {
	if info.AssetURL == "" {
		return "", fmt.Errorf("missing download URL for %s", info.AssetName)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.AssetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "pan-agent/"+version.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed: GitHub returned %s", resp.Status)
	}

	current, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(current), ".pan-update-*")
	if err != nil {
		return "", fmt.Errorf("create update file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write update file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close update file: %w", err)
	}
	if stat, err := os.Stat(current); err == nil {
		_ = os.Chmod(tmpPath, stat.Mode().Perm())
	} else {
		_ = os.Chmod(tmpPath, 0o755)
	}
	cleanup = false
	return tmpPath, nil
}

func installDownloadedCLI(downloadPath string) error {
	current, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return fmt.Errorf("resolve current executable path: %w", err)
	}
	if runtime.GOOS == "windows" {
		return installDownloadedCLIWindows(downloadPath, current)
	}
	if err := os.Rename(downloadPath, current); err != nil {
		return fmt.Errorf("replace %s: %w", current, err)
	}
	fmt.Println("✓ Pan Agent CLI updated. Open a new terminal and run `pan version`.")
	return nil
}

func installDownloadedCLIWindows(downloadPath, targetPath string) error {
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("pan-update-%d.ps1", os.Getpid()))
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Write-Host '-> Waiting for pan.exe to exit...'
Wait-Process -Id %d -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 300
Move-Item -LiteralPath %s -Destination %s -Force
Write-Host '✓ Pan Agent CLI updated. Open a new terminal and run pan version.'
Remove-Item -LiteralPath $MyInvocation.MyCommand.Path -Force -ErrorAction SilentlyContinue
`, os.Getpid(), powershellQuote(downloadPath), powershellQuote(targetPath))
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return fmt.Errorf("write Windows update script: %w", err)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Windows update script: %w", err)
	}
	fmt.Println("✓ Update downloaded. The installer will replace pan.exe after this command exits.")
	return nil
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func isVersionNewer(candidate, current string) bool {
	c := parseVersion(candidate)
	v := parseVersion(current)
	for i := 0; i < len(c) && i < len(v); i++ {
		if c[i] > v[i] {
			return true
		}
		if c[i] < v[i] {
			return false
		}
	}
	return false
}

func parseVersion(raw string) [3]int {
	var out [3]int
	parts := strings.Split(normalizeVersion(raw), ".")
	for i := 0; i < len(parts) && i < len(out); i++ {
		part := parts[i]
		if idx := strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }); idx >= 0 {
			part = part[:idx]
		}
		n, _ := strconv.Atoi(part)
		out[i] = n
	}
	return out
}

func normalizeVersion(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(strings.ToLower(raw)), "v")
}

func printShell(cfg cliConfig, updateNotice string) {
	fmt.Printf("%s%s\n", ansiAmber, banner())
	fmt.Printf("%s", ansiReset)
	rule()
	fmt.Printf(" %s%s AGENT%s %s%s%s\n", ansiBold+ansiAmber, strings.ToUpper(agentLabel(cfg)), ansiReset, ansiGray, version.Version, ansiReset)
	if updateNotice != "" {
		fmt.Printf(" %supdate%s   %s\n", ansiAmber, ansiReset, updateNotice)
	}
	fmt.Printf(" %sprofile%s  %s\n", ansiAmber, ansiReset, cfg.Profile)
	fmt.Printf(" %smodel%s    %s\n", ansiAmber, ansiReset, cfg.Model)
	fmt.Printf(" %sprovider%s %s\n", ansiAmber, ansiReset, cfg.Provider)
	fmt.Printf(" %sbase%s     %s\n", ansiAmber, ansiReset, cfg.BaseURL)
	if cfg.APIKey == "" {
		fmt.Printf(" %skey%s      %smissing%s\n", ansiAmber, ansiReset, ansiRed, ansiReset)
	} else {
		fmt.Printf(" %skey%s      %sconfigured%s\n", ansiAmber, ansiReset, ansiBlue, ansiReset)
	}
	rule()
	fmt.Printf("%sType a message and press Enter. /help for commands. Ctrl+C clears input/cancels replies; press twice to exit.%s\n", ansiGray, ansiReset)
}

func banner() string {
	return `██████╗  █████╗ ███╗   ██╗     █████╗  ██████╗ ███████╗███╗   ██╗████████╗
██╔══██╗██╔══██╗████╗  ██║    ██╔══██╗██╔════╝ ██╔════╝████╗  ██║╚══██╔══╝
██████╔╝███████║██╔██╗ ██║    ███████║██║  ███╗█████╗  ██╔██╗ ██║   ██║
██╔═══╝ ██╔══██║██║╚██╗██║    ██╔══██║██║   ██║██╔══╝  ██║╚██╗██║   ██║
██║     ██║  ██║██║ ╚████║    ██║  ██║╚██████╔╝███████╗██║ ╚████║   ██║
╚═╝     ╚═╝  ╚═╝╚═╝  ╚═══╝    ╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝  ╚═══╝   ╚═╝`
}

func rule() {
	fmt.Printf("%s%s%s\n", ansiOrange, strings.Repeat("─", 96), ansiReset)
}

func clearScreen() {
	fmt.Print("\x1b[2J\x1b[H")
}

func chatLoop(cfg cliConfig) error {
	client := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	var history []llm.Message
	fmt.Print("\x1b[?2004h")
	defer fmt.Print("\x1b[?2004l")
	input, err := newInputReader(os.Stdin)
	if err != nil {
		return err
	}
	defer input.Close()
	interrupts := &interruptTracker{}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	for {
		fmt.Printf("\n%s›%s ", ansiAmber, ansiReset)
		block, ok, err := input.ReadBlock(sigCh)
		if errors.Is(err, errInputInterrupted) {
			if interrupts.ShouldExit() {
				fmt.Println("\nbye")
				return nil
			}
			fmt.Printf("\r\x1b[2K%sinput cleared. Press Ctrl+C again to exit.%s\n", ansiGray, ansiReset)
			continue
		}
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println()
			return nil
		}
		interrupts.Reset()

		line := strings.Trim(block, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		isPastedBlock := strings.Contains(line, "\n")
		if !isPastedBlock {
			line = strings.TrimSpace(line)
			if handled, quit := handleCommand(line, cfg, &history); handled {
				if quit {
					return nil
				}
				continue
			}
		} else if handled, quit := handleCommandLines(line, cfg, &history); handled {
			if quit {
				return nil
			}
			continue
		}

		history = append(history, llm.Message{Role: "user", Content: line})
		fmt.Printf("%s%s%s ", ansiAmber, agentLabel(cfg), ansiReset)

		ctx, cancel := context.WithCancel(context.Background())
		streamDone := make(chan struct{})
		streamQuit := make(chan struct{}, 1)
		go func() {
			for {
				select {
				case <-input.Interrupts():
					if interrupts.ShouldExit() {
						select {
						case streamQuit <- struct{}{}:
						default:
						}
						cancel()
						return
					}
					fmt.Printf("\n%sreply cancelled. Press Ctrl+C again to exit.%s\n", ansiGray, ansiReset)
					cancel()
				case <-sigCh:
					if interrupts.ShouldExit() {
						select {
						case streamQuit <- struct{}{}:
						default:
						}
						cancel()
						return
					}
					fmt.Printf("\n%sreply cancelled. Press Ctrl+C again to exit.%s\n", ansiGray, ansiReset)
					cancel()
				case <-streamDone:
					return
				}
			}
		}()

		start := time.Now()
		stream, err := client.ChatStream(ctx, history, nil)
		if err != nil {
			cancel()
			close(streamDone)
			if wasStreamQuit(streamQuit) {
				fmt.Println("\nbye")
				return nil
			}
			history = history[:len(history)-1]
			fmt.Printf("%srequest failed:%s %v\n", ansiRed, ansiReset, err)
			continue
		}

		var assistant strings.Builder
		for ev := range stream {
			switch ev.Type {
			case "chunk":
				fmt.Print(ev.Content)
				assistant.WriteString(ev.Content)
			case "error":
				fmt.Printf("\n%smodel error:%s %s\n", ansiRed, ansiReset, ev.Error)
			}
		}

		cancel()
		close(streamDone)
		if wasStreamQuit(streamQuit) {
			fmt.Println("\nbye")
			return nil
		}
		interrupts.Reset()
		fmt.Printf("%s\n[%s in %.1fs]%s\n", ansiDim, cfg.Model, time.Since(start).Seconds(), ansiReset)

		if assistant.Len() > 0 {
			history = append(history, llm.Message{Role: "assistant", Content: assistant.String()})
		}
	}
}

type inputReader struct {
	lines      <-chan string
	errs       <-chan error
	interrupts <-chan struct{}
	restore    func() error
}

type interruptTracker struct {
	mu   sync.Mutex
	last time.Time
}

func (t *interruptTracker) ShouldExit() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if !t.last.IsZero() && now.Sub(t.last) <= 1500*time.Millisecond {
		t.last = time.Time{}
		return true
	}
	t.last = now
	return false
}

func (t *interruptTracker) Reset() {
	t.mu.Lock()
	t.last = time.Time{}
	t.mu.Unlock()
}

func wasStreamQuit(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func newInputReader(in *os.File) (*inputReader, error) {
	lines := make(chan string, 256)
	errs := make(chan error, 1)
	interrupts := make(chan struct{}, 4)
	restore := func() error { return nil }

	if term.IsTerminal(int(in.Fd())) {
		state, err := term.MakeRaw(int(in.Fd()))
		if err != nil {
			return nil, fmt.Errorf("enable raw terminal input: %w", err)
		}
		restore = func() error {
			return term.Restore(int(in.Fd()), state)
		}
		go readRawTerminalInput(in, lines, errs, interrupts)
		return &inputReader{
			lines:      lines,
			errs:       errs,
			interrupts: interrupts,
			restore:    restore,
		}, nil
	}

	go func() {
		defer close(lines)
		defer close(errs)
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		errs <- scanner.Err()
	}()
	return &inputReader{
		lines:      lines,
		errs:       errs,
		interrupts: interrupts,
		restore:    restore,
	}, nil
}

func (r *inputReader) Close() {
	if r.restore != nil {
		_ = r.restore()
	}
}

func (r *inputReader) Interrupts() <-chan struct{} {
	return r.interrupts
}

func readRawTerminalInput(in *os.File, lines chan<- string, errs chan<- error, interrupts chan<- struct{}) {
	defer close(lines)
	defer close(errs)
	defer close(interrupts)

	var line strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				errs <- nil
			} else {
				errs <- err
			}
			return
		}
		if n == 0 {
			continue
		}

		switch b := buf[0]; b {
		case 0x03:
			line.Reset()
			fmt.Print("\r\x1b[2K")
			select {
			case interrupts <- struct{}{}:
			default:
			}
		case 0x1b:
			seq, err := readEscapeSequence(in)
			if err != nil {
				if errors.Is(err, io.EOF) {
					errs <- nil
				} else {
					errs <- err
				}
				return
			}
			if seq == "[200~" {
				paste, err := readBracketedPaste(in)
				if err != nil {
					if errors.Is(err, io.EOF) {
						errs <- nil
					} else {
						errs <- err
					}
					return
				}
				line.Reset()
				paste = normalizePastedText(paste)
				fmt.Print(paste)
				if !strings.HasSuffix(paste, "\n") {
					fmt.Print("\r\n")
				}
				lines <- paste
			}
		case '\r', '\n':
			fmt.Print("\r\n")
			lines <- line.String()
			line.Reset()
		case 0x08, 0x7f:
			current := line.String()
			if len(current) == 0 {
				continue
			}
			current = current[:len(current)-1]
			line.Reset()
			line.WriteString(current)
			fmt.Print("\b \b")
		default:
			if b < 0x20 {
				continue
			}
			line.WriteByte(b)
			_, _ = os.Stdout.Write(buf[:1])
		}
	}
}

func readEscapeSequence(in *os.File) (string, error) {
	var seq strings.Builder
	buf := make([]byte, 1)
	for seq.Len() < 16 {
		n, err := in.Read(buf)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		seq.WriteByte(buf[0])
		if (buf[0] >= 'A' && buf[0] <= 'Z') || (buf[0] >= 'a' && buf[0] <= 'z') || buf[0] == '~' {
			break
		}
	}
	return seq.String(), nil
}

func readBracketedPaste(in *os.File) (string, error) {
	const end = "\x1b[201~"
	var paste strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		paste.WriteByte(buf[0])
		current := paste.String()
		if strings.HasSuffix(current, end) {
			return strings.TrimSuffix(current, end), nil
		}
	}
}

func normalizePastedText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Trim(text, "\n")
}

func (r *inputReader) ReadBlock(interrupts <-chan os.Signal) (string, bool, error) {
	var first string
	select {
	case line, ok := <-r.lines:
		if !ok {
			return "", false, <-r.errs
		}
		first = line
	case <-r.interrupts:
		return "", true, errInputInterrupted
	case <-interrupts:
		return "", true, errInputInterrupted
	}

	lines := []string{first}
	timer := time.NewTimer(90 * time.Millisecond)
	defer timer.Stop()

	for {
		select {
		case line, ok := <-r.lines:
			if !ok {
				return strings.Join(lines, "\n"), true, <-r.errs
			}
			lines = append(lines, line)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(90 * time.Millisecond)
		case <-r.interrupts:
			return "", true, errInputInterrupted
		case <-interrupts:
			return "", true, errInputInterrupted
		case <-timer.C:
			return strings.Join(lines, "\n"), true, nil
		}
	}
}

func handleCommandLines(block string, cfg cliConfig, history *[]llm.Message) (handled bool, quit bool) {
	lines := strings.Split(block, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "/") && line != "exit" && line != "quit" {
			return false, false
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if handled, quit := handleCommand(line, cfg, history); handled {
			if quit {
				return true, true
			}
			continue
		}
		return false, false
	}
	return true, false
}

func runOneshot(cfg cliConfig, prompt string) error {
	client := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	ctx := context.Background()
	stream, err := client.ChatStream(ctx, []llm.Message{{Role: "user", Content: prompt}}, nil)
	if err != nil {
		return err
	}
	for ev := range stream {
		switch ev.Type {
		case "chunk":
			fmt.Print(ev.Content)
		case "error":
			return errors.New(ev.Error)
		}
	}
	fmt.Println()
	return nil
}

func handleCommand(line string, cfg cliConfig, history *[]llm.Message) (handled bool, quit bool) {
	switch strings.ToLower(line) {
	case "/exit", "/quit", "exit", "quit":
		fmt.Println("bye")
		return true, true
	case "/clear":
		*history = nil
		clearScreen()
		printShell(cfg, "")
		return true, false
	case "/", "/help":
		printInChatCommands()
		return true, false
	case "/model":
		fmt.Printf("%s%s%s via %s%s%s\n", ansiAmber, cfg.Model, ansiReset, ansiGray, cfg.BaseURL, ansiReset)
		return true, false
	case "/profile":
		fmt.Printf("%s%s%s\n", ansiAmber, cfg.Profile, ansiReset)
		return true, false
	default:
		return false, false
	}
}

func printInChatCommands() {
	fmt.Printf("%sCommands%s\n", ansiAmber, ansiReset)
	fmt.Println("  /          show commands")
	fmt.Println("  /help      show commands")
	fmt.Println("  /model     show active model")
	fmt.Println("  /profile   show active profile")
	fmt.Println("  /clear     clear screen and reset chat history")
	fmt.Println("  /exit      quit")
	fmt.Println()
	fmt.Println("Paste multi-line logs or stack traces directly; fast pasted lines are sent as one message.")
	fmt.Println("Ctrl+C clears input or cancels the current reply; press Ctrl+C twice to exit.")
}
