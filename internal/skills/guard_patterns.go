package skills

import "regexp"

// patternDef is one security-scanner pattern.
type patternDef struct {
	Category string // exec|fs|net|creds|obfuscation|prompt_injection
	Label    string // human-readable label shown in findings
	Severity string // "block" | "warn"
	Re       *regexp.Regexp
	// CaseSensitive=true means this pattern matches the RAW line (useful
	// for credentials like `AKIA...` and `ghp_...` that are uppercase by
	// specification). CaseSensitive=false (default) matches the normalized
	// line (lowercase + NFKC + zero-width stripped) so attackers cannot
	// bypass by altering case or sneaking in bidi/zero-width chars.
	CaseSensitive bool
}

// builtinPatterns is the package-level set of guard patterns. Compiled at init.
var builtinPatterns []patternDef

func init() {
	specs := []struct {
		Category, Label, Severity, Pattern string
		CaseSensitive                      bool
	}{
		// exec — normalized-line matching (lowercase + NFKC).
		// Match rm with any combination of -r / -R / -f / --recursive / --force
		// in any order, followed by the root path or an /etc/, /usr/, /System/
		// target. Normalization lowercases before matching, so `RM -RF /` hits.
		{"exec", "destructive rm -rf /", "block",
			`\brm\s+(-[rf]+\s*|--(recursive|force)\s+){1,}/($|\s|\*|etc|usr|bin|sbin|boot|system|library)`, false},
		{"exec", "mkfs on a device", "block", `\bmkfs(\.[a-z0-9]+)?\s+.*/dev/`, false},
		{"exec", "dd to raw device", "block", `\bdd\s+.*of=\s*/dev/`, false},
		{"exec", "format drive", "block", `\bformat\s+[a-z]:\s*/fs`, false},
		{"exec", "bash forkbomb", "block", `:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`, false},
		{"exec", "powershell encoded command", "warn", `\b(powershell|pwsh)(\s|\.exe\s).*-(e|enc|encodedcommand)\b`, false},
		{"exec", "eval/exec of user data", "warn", `\b(eval|exec)\s*\(\s*(input|stdin|argv|sys\.argv)`, false},

		// fs
		{"fs", "path traversal fragment", "warn", `(\.\./){3,}`, false},
		{"fs", "python rmtree of root path", "block", `shutil\.rmtree\s*\(\s*['"]/`, false},
		{"fs", "wildcard os.remove", "warn", `os\.remove\s*\(.*\*`, false},
		{"fs", "chmod 000 directory", "warn", `\bchmod\s+000\b`, false},

		// net
		{"net", "curl/wget pipe to shell", "block", `\b(curl|wget)\s+[^\|]*\|\s*(bash|sh|zsh|fish|python|ruby|perl)\b`, false},
		{"net", "reverse shell via nc", "block", `\bnc(at)?\s+-[el]`, false},
		{"net", "hardcoded private IP URL", "warn", `https?://(10|127|172\.(1[6-9]|2\d|3[01])|192\.168)\.[\d.]+`, false},
		{"net", "python socket.connect", "warn", `\bsocket(\.socket\(\))?\s*\.\s*connect\s*\(`, false},
		{"net", "base64 url fetch-and-exec", "block", `(?s)base64.*decode.*\b(urllib|requests)\.(get|urlopen).*\bexec`, false},

		// creds — case-sensitive: real credentials follow their upstream casing,
		// and a lowercase match would false-positive on prose like "aws access key".
		{"creds", "private key header", "block", `-----BEGIN\s+(RSA|OPENSSH|PGP|EC|DSA)\s+PRIVATE\s+KEY-----`, true},
		{"creds", "AWS access key", "block", `\bAKIA[0-9A-Z]{16}\b`, true},
		{"creds", "OpenAI-style API key", "warn", `\bsk-[A-Za-z0-9]{20,}\b`, true},
		{"creds", "GitHub personal access token", "block", `\bghp_[A-Za-z0-9]{36}\b|\bgithub_pat_[A-Za-z0-9_]{22,}`, true},
		{"creds", "Slack bot token", "block", `\bxox[baprs]-[A-Za-z0-9-]{10,}\b`, true},

		// obfuscation
		{"obfuscation", "base64 decode then exec", "block", `(?s)base64\s*\.\s*(b64decode|decode)[^\n]{0,200}?\bexec\s*\(`, false},
		{"obfuscation", "hex-escape blob", "warn", `(\\x[0-9a-f]{2}){8,}`, false},
		{"obfuscation", "chr() concat", "warn", `(chr\(\d+\)\s*\+\s*){3,}`, false},
		{"obfuscation", "String.fromCharCode chain", "warn", `(fromcharcode\s*\(\s*\d+\s*\)\s*\+\s*){3,}`, false},

		// prompt injection — case-sensitive irrelevant since normalization already lowercases.
		{"prompt_injection", "ignore previous instructions", "block", `ignore\s+(all\s+|the\s+)?(previous|prior|above)\s+instructions`, false},
		{"prompt_injection", "disregard system prompt", "block", `disregard\s+the\s+system\s+prompt`, false},
		{"prompt_injection", "new persona injection", "warn", `you\s+are\s+now\s+(a\s+)?(different|new)\s+`, false},
		{"prompt_injection", "developer mode jailbreak", "block", `(developer|dan|jailbreak)\s+mode\s+enabled`, false},
		{"prompt_injection", "pseudo-tag system/assistant", "warn", `</?\s*(system|assistant|user)\s*>`, false},
		// Zero-width / bidi injection chars: the guard normalizer strips these
		// before matching (they would not reach any normalized-mode pattern),
		// so detect them against the raw line.
		{"prompt_injection", "zero-width injection chars", "warn", `[\x{200b}-\x{200f}\x{202a}-\x{202e}\x{2066}-\x{2069}]`, true},
	}

	builtinPatterns = make([]patternDef, 0, len(specs))
	for _, s := range specs {
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			// Compilation failures are a programmer bug; skip the pattern at runtime.
			continue
		}
		builtinPatterns = append(builtinPatterns, patternDef{
			Category:      s.Category,
			Label:         s.Label,
			Severity:      s.Severity,
			Re:            re,
			CaseSensitive: s.CaseSensitive,
		})
	}
}
