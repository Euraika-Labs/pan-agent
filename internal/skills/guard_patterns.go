package skills

import "regexp"

// patternDef is one security-scanner pattern.
type patternDef struct {
	Category string // exec|fs|net|creds|obfuscation|prompt_injection
	Label    string // human-readable label shown in findings
	Severity string // "block" | "warn"
	Re       *regexp.Regexp
}

// builtinPatterns is the package-level set of guard patterns. Compiled at init.
var builtinPatterns []patternDef

func init() {
	specs := []struct{ Category, Label, Severity, Pattern string }{
		// exec
		{"exec", "destructive rm -rf /", "block", `\brm\s+-rf\s+/($|\s|\*)`},
		{"exec", "mkfs on a device", "block", `\bmkfs\.[a-z0-9]+\s+/dev/`},
		{"exec", "dd to raw device", "block", `\bdd\s+.*of=/dev/`},
		{"exec", "bash forkbomb", "block", `:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`},
		{"exec", "powershell encoded command", "warn", `\b(powershell|pwsh)(\s|\.exe\s).*-(e|enc|encodedcommand)\b`},
		{"exec", "eval/exec of user data", "warn", `\b(eval|exec)\s*\(\s*(input|stdin|argv|sys\.argv)`},

		// fs
		{"fs", "path traversal fragment", "warn", `(\.\./){3,}`},
		{"fs", "python rmtree of root path", "block", `shutil\.rmtree\s*\(\s*['"]/`},
		{"fs", "wildcard os.remove", "warn", `os\.remove\s*\(.*\*`},
		{"fs", "chmod 000 directory", "warn", `\bchmod\s+000\b`},

		// net
		{"net", "curl/wget pipe to shell", "block", `\b(curl|wget)\s+[^\|]*\|\s*(bash|sh|zsh|fish|python|ruby|perl)\b`},
		{"net", "reverse shell via nc", "block", `\bnc(at)?\s+-[el]`},
		{"net", "hardcoded private IP URL", "warn", `https?://(10|127|172\.(1[6-9]|2\d|3[01])|192\.168)\.[\d.]+`},
		{"net", "python socket.connect", "warn", `\bsocket(\.socket\(\))?\s*\.\s*connect\s*\(`},
		{"net", "base64 url fetch-and-exec", "block", `(?s)base64.*decode.*\b(urllib|requests)\.(get|urlopen).*\bexec`},

		// creds
		{"creds", "private key header", "block", `-----BEGIN\s+(RSA|OPENSSH|PGP|EC|DSA)\s+PRIVATE\s+KEY-----`},
		{"creds", "AWS access key", "block", `\bAKIA[0-9A-Z]{16}\b`},
		{"creds", "OpenAI-style API key", "warn", `\bsk-[A-Za-z0-9]{20,}\b`},
		{"creds", "GitHub personal access token", "block", `\bghp_[A-Za-z0-9]{36}\b|\bgithub_pat_[A-Za-z0-9_]{22,}`},
		{"creds", "Slack bot token", "block", `\bxox[baprs]-[A-Za-z0-9-]{10,}\b`},

		// obfuscation
		{"obfuscation", "base64 decode then exec", "block", `(?s)base64\s*\.\s*(b64decode|decode)[^\n]{0,200}?\bexec\s*\(`},
		{"obfuscation", "hex-escape blob", "warn", `(\\x[0-9a-f]{2}){8,}`},
		{"obfuscation", "chr() concat", "warn", `(chr\(\d+\)\s*\+\s*){3,}`},
		{"obfuscation", "String.fromCharCode chain", "warn", `(fromCharCode\s*\(\s*\d+\s*\)\s*\+\s*){3,}`},

		// prompt injection
		{"prompt_injection", "ignore previous instructions", "block", `(?i)ignore\s+(all\s+|the\s+)?(previous|prior|above)\s+instructions`},
		{"prompt_injection", "disregard system prompt", "block", `(?i)disregard\s+the\s+system\s+prompt`},
		{"prompt_injection", "new persona injection", "warn", `(?i)you\s+are\s+now\s+(a\s+)?(different|new)\s+`},
		{"prompt_injection", "developer mode jailbreak", "block", `(?i)(developer|dan|jailbreak)\s+mode\s+enabled`},
		{"prompt_injection", "pseudo-tag system/assistant", "warn", `</?\s*(system|assistant|user)\s*>`},
		{"prompt_injection", "zero-width injection chars", "warn", `[\x{200b}-\x{200f}\x{202a}-\x{202e}\x{2066}-\x{2069}]`},
	}

	builtinPatterns = make([]patternDef, 0, len(specs))
	for _, s := range specs {
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			// Compilation failures are a programmer bug; skip the pattern at runtime.
			continue
		}
		builtinPatterns = append(builtinPatterns, patternDef{
			Category: s.Category,
			Label:    s.Label,
			Severity: s.Severity,
			Re:       re,
		})
	}
}
