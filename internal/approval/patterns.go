// Package approval detects dangerous and catastrophic shell commands.
// All patterns are ported from pan-desktop/resources/overlays/tools/approval.py.
package approval

import "regexp"

// Pattern holds a compiled dangerous-command regex with its key and description.
//
// NegativeRegex, when non-nil, is a compiled negative-lookahead substitute:
// the pattern matches only when Regex matches AND NegativeRegex does NOT match.
// This is used for the SQL DELETE pattern which uses a Python/PCRE negative
// lookahead that Go's RE2 engine does not support.
type Pattern struct {
	Regex         *regexp.Regexp
	NegativeRegex *regexp.Regexp // optional; nil means no negative check
	Key           string
	Description   string
}

func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + pattern)
}

func mustCompileNeg(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + pattern)
}

// p is a convenience constructor for a plain pattern.
func p(pattern, key, desc string) Pattern {
	return Pattern{Regex: mustCompile(pattern), Key: key, Description: desc}
}

// pNeg constructs a pattern that only fires when regex matches but negRegex does NOT.
func pNeg(pattern, negPattern, key, desc string) Pattern {
	return Pattern{
		Regex:         mustCompile(pattern),
		NegativeRegex: mustCompileNeg(negPattern),
		Key:           key,
		Description:   desc,
	}
}

// DangerousPatterns is Level 1: single-confirm. 75 entries ported verbatim from
// DANGEROUS_PATTERNS in pan-desktop/resources/overlays/tools/approval.py.
//
// The _SENSITIVE_WRITE_TARGET composite from the Python source is inlined below:
//
//	(?:/etc/|/dev/sd|(?:~|\$home|\$\{home\})/\.ssh(?:/|$)|(?:~\/\.hermes/|(?:\$home|\$\{home\})/\.hermes/|(?:\$hermes_home|\$\{hermes_home\})/)\.env\b)
var DangerousPatterns = []Pattern{
	// ---- Linux / cross-platform entries (upstream Hermes) ----
	p(`\brm\s+(-[^\s]*\s+)*/`, "delete in root path", "delete in root path"),
	p(`\brm\s+-[^\s]*r`, "recursive delete", "recursive delete"),
	p(`\brm\s+--recursive\b`, "recursive delete (long flag)", "recursive delete (long flag)"),
	p(`\bchmod\s+(-[^\s]*\s+)*(777|666|o\+[rwx]*w|a\+[rwx]*w)\b`, "world/other-writable permissions", "world/other-writable permissions"),
	p(`\bchmod\s+--recursive\b.*(777|666|o\+[rwx]*w|a\+[rwx]*w)`, "recursive world/other-writable (long flag)", "recursive world/other-writable (long flag)"),
	p(`\bchown\s+(-[^\s]*)?R\s+root`, "recursive chown to root", "recursive chown to root"),
	p(`\bchown\s+--recursive\b.*root`, "recursive chown to root (long flag)", "recursive chown to root (long flag)"),
	p(`\bmkfs\b`, "format filesystem", "format filesystem"),
	p(`\bdd\s+.*if=`, "disk copy", "disk copy"),
	p(`>\s*/dev/sd`, "write to block device", "write to block device"),
	p(`\bDROP\s+(TABLE|DATABASE)\b`, "SQL DROP", "SQL DROP"),
	// Python: r'\bDELETE\s+FROM\b(?!.*\bWHERE\b)' — RE2 does not support negative
	// lookaheads, so this is split into a positive match + negative guard.
	pNeg(`\bDELETE\s+FROM\b`, `\bWHERE\b`, "SQL DELETE without WHERE", "SQL DELETE without WHERE"),
	p(`\bTRUNCATE\s+(TABLE)?\s*\w`, "SQL TRUNCATE", "SQL TRUNCATE"),
	p(`>\s*/etc/`, "overwrite system config", "overwrite system config"),
	p(`\bsystemctl\s+(stop|disable|mask)\b`, "stop/disable system service", "stop/disable system service"),
	p(`\bkill\s+-9\s+-1\b`, "kill all processes", "kill all processes"),
	p(`\bpkill\s+-9\b`, "force kill processes", "force kill processes"),
	p(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`, "fork bomb", "fork bomb"),
	// Any shell invocation via -c or combined flags like -lc, -ic, etc.
	p(`\b(bash|sh|zsh|ksh)\s+-[^\s]*c(\s+|$)`, "shell command via -c/-lc flag", "shell command via -c/-lc flag"),
	p(`\b(python[23]?|perl|ruby|node)\s+-[ec]\s+`, "script execution via -e/-c flag", "script execution via -e/-c flag"),
	p(`\b(curl|wget)\b.*\|\s*(ba)?sh\b`, "pipe remote content to shell", "pipe remote content to shell"),
	p(`\b(bash|sh|zsh|ksh)\s+<\s*<?\s*\(\s*(curl|wget)\b`, "execute remote script via process substitution", "execute remote script via process substitution"),
	// _SENSITIVE_WRITE_TARGET inlined:
	p(`\btee\b.*["']?(?:/etc/|/dev/sd|(?:~|\$home|\$\{home\})/\.ssh(?:/|$)|(?:~\/\.hermes/|(?:\$home|\$\{home\})/\.hermes/|(?:\$hermes_home|\$\{hermes_home\})/)\.env\b)`,
		"overwrite system file via tee", "overwrite system file via tee"),
	p(`>>?\s*["']?(?:/etc/|/dev/sd|(?:~|\$home|\$\{home\})/\.ssh(?:/|$)|(?:~\/\.hermes/|(?:\$home|\$\{home\})/\.hermes/|(?:\$hermes_home|\$\{hermes_home\})/)\.env\b)`,
		"overwrite system file via redirection", "overwrite system file via redirection"),
	p(`\bxargs\s+.*\brm\b`, "xargs with rm", "xargs with rm"),
	p(`\bfind\b.*-exec\s+(/\S*/)?rm\b`, "find -exec rm", "find -exec rm"),
	p(`\bfind\b.*-delete\b`, "find -delete", "find -delete"),
	// Gateway protection
	p(`gateway\s+run\b.*(&\s*$|&\s*;|\bdisown\b|\bsetsid\b)`, "start gateway outside systemd (use 'systemctl --user restart hermes-gateway')", "start gateway outside systemd (use 'systemctl --user restart hermes-gateway')"),
	p(`\bnohup\b.*gateway\s+run\b`, "start gateway outside systemd (use 'systemctl --user restart hermes-gateway')", "start gateway outside systemd (use 'systemctl --user restart hermes-gateway')"),
	// Self-termination protection
	p(`\b(pkill|killall)\b.*\b(hermes|gateway|cli\.py)\b`, "kill hermes/gateway process (self-termination)", "kill hermes/gateway process (self-termination)"),
	p(`\bkill\b.*\$\(\s*pgrep\b`, "kill process via pgrep expansion (self-termination)", "kill process via pgrep expansion (self-termination)"),
	p(`\bkill\b.*`+"`"+`\s*pgrep\b`, "kill process via backtick pgrep expansion (self-termination)", "kill process via backtick pgrep expansion (self-termination)"),
	// File copy/move/edit into sensitive system paths
	p(`\b(cp|mv|install)\b.*\s/etc/`, "copy/move file into /etc/", "copy/move file into /etc/"),
	p(`\bsed\s+-[^\s]*i.*\s/etc/`, "in-place edit of system config", "in-place edit of system config"),
	p(`\bsed\s+--in-place\b.*\s/etc/`, "in-place edit of system config (long flag)", "in-place edit of system config (long flag)"),
	// Script execution via heredoc
	p(`\b(python[23]?|perl|ruby|node)\s+<<`, "script execution via heredoc", "script execution via heredoc"),
	// Git destructive operations
	p(`\bgit\s+reset\s+--hard\b`, "git reset --hard (destroys uncommitted changes)", "git reset --hard (destroys uncommitted changes)"),
	p(`\bgit\s+push\b.*--force\b`, "git force push (rewrites remote history)", "git force push (rewrites remote history)"),
	p(`\bgit\s+push\b.*-f\b`, "git force push short flag (rewrites remote history)", "git force push short flag (rewrites remote history)"),
	p(`\bgit\s+clean\s+-[^\s]*f`, "git clean with force (deletes untracked files)", "git clean with force (deletes untracked files)"),
	p(`\bgit\s+branch\s+-D\b`, "git branch force delete", "git branch force delete"),
	// Script execution after chmod +x
	p(`\bchmod\s+\+x\b.*[;&|]+\s*\./`, "chmod +x followed by immediate execution", "chmod +x followed by immediate execution"),

	// ---- Pan Desktop — Windows (Level 1: single confirm) ----
	// Recursive Windows delete
	p(`\bdel\b[^\n]*\s/s\b`, "Windows recursive delete (del /s)", "Windows recursive delete (del /s)"),
	p(`\b(rmdir|rd)\b[^\n]*\s/s\b`, "Windows recursive directory remove (rmdir /s)", "Windows recursive directory remove (rmdir /s)"),
	// Registry modifications under HKLM / HKCU
	p(`\breg\s+add\s+("?hklm\\|hkey_local_machine\\)`, "modify HKLM registry", "modify HKLM registry"),
	p(`\breg\s+add\s+("?hkcu\\|hkey_current_user\\)`, "modify HKCU registry", "modify HKCU registry"),
	// Scheduled task / service persistence
	p(`\bschtasks\s+/create\b`, "scheduled task creation (persistence)", "scheduled task creation (persistence)"),
	// Privilege escalation
	p(`\brunas\s+/user:`, "runas privilege escalation", "runas privilege escalation"),
	p(`\bstart-process\b[^\n]*-verb\s+runas\b`, "PowerShell privilege escalation (Start-Process -Verb RunAs)", "PowerShell privilege escalation (Start-Process -Verb RunAs)"),
	// icacls granting full access to broad principals
	p(`\bicacls\b[^\n]*\/grant(?::r)?\s+(everyone|users|authenticated\s+users):f\b`, "icacls grant Full to Everyone/Users", "icacls grant Full to Everyone/Users"),
	// takeown
	p(`\btakeown\s+/f\b`, "takeown /f (file ownership seizure)", "takeown /f (file ownership seizure)"),
	// attrib strip flags
	p(`\battrib\s+[^\n]*(-r\b|-s\b|-h\b)`, "attrib remove read-only/system/hidden", "attrib remove read-only/system/hidden"),
	// WMI process creation
	p(`\bwmic\s+process\s+call\s+create\b`, "WMI process create (code execution)", "WMI process create (code execution)"),
	// PowerShell eval primitives
	p(`\binvoke-expression\b`, "PowerShell Invoke-Expression (eval)", "PowerShell Invoke-Expression (eval)"),
	p(`\biex\s*\(`, "PowerShell iex( (eval shorthand)", "PowerShell iex( (eval shorthand)"),
	p(`\b(iex|invoke-expression)\b[^\n]*\(\s*new-object\s+[^\n]*net\.webclient\b`, "PowerShell download-and-execute (IEX New-Object Net.WebClient)", "PowerShell download-and-execute (IEX New-Object Net.WebClient)"),
	p(`\bdownloadstring\s*\(\s*['"]https?://`, "PowerShell DownloadString (remote content fetch)", "PowerShell DownloadString (remote content fetch)"),
	// Disable PowerShell execution policy
	p(`\bset-executionpolicy\s+(unrestricted|bypass)\b`, "disable PowerShell execution policy", "disable PowerShell execution policy"),
	// Firewall changes
	p(`\bnetsh\s+advfirewall\s+set\b`, "Windows firewall modification (netsh advfirewall)", "Windows firewall modification (netsh advfirewall)"),
	p(`\bnetsh\s+firewall\s+set\b`, "Windows firewall modification (legacy netsh firewall)", "Windows firewall modification (legacy netsh firewall)"),
	// Service deletion / stop
	p(`\bsc(?:\.exe)?\s+delete\b`, "delete Windows service (sc delete)", "delete Windows service (sc delete)"),
	p(`\bsc(?:\.exe)?\s+stop\b`, "stop Windows service (sc stop)", "stop Windows service (sc stop)"),
	// LOLBIN download tools
	p(`\bcertutil\b[^\n]*-urlcache\b[^\n]*\bhttps?://`, "certutil LOLBIN download (-urlcache)", "certutil LOLBIN download (-urlcache)"),
	p(`\bcertutil\b[^\n]*-decode\b`, "certutil LOLBIN decode", "certutil LOLBIN decode"),
	p(`\bbitsadmin\s+/transfer\b`, "bitsadmin LOLBIN download (/transfer)", "bitsadmin LOLBIN download (/transfer)"),
	p(`\bmshta\s+https?://`, "mshta LOLBIN remote script execution", "mshta LOLBIN remote script execution"),
	// rundll32
	p(`\brundll32(?:\.exe)?\s+\S+\.dll,`, "rundll32 DLL export execution", "rundll32 DLL export execution"),
	// Squiblydoo
	p(`\bregsvr32\b[^\n]*/i:https?://`, "regsvr32 squiblydoo (/i:http)", "regsvr32 squiblydoo (/i:http)"),
	// curl download-and-execute patterns
	p(`\bcurl(?:\.exe)?\b[^\n]*\|\s*(cmd|powershell|pwsh)\b`, "curl piped to cmd/powershell", "curl piped to cmd/powershell"),
	p(`\bcurl(?:\.exe)?\b[^\n]*>\s*['"]?[^\s'"|&;]+\.(exe|bat|cmd|ps1|vbs|hta|msi|scr|dll)\b`, "curl redirected to executable file", "curl redirected to executable file"),
	p(`\bcurl(?:\.exe)?\b[^\n]*\s(?:-o|--output)\s+['"]?[^\s'"|&;]+\.(exe|bat|cmd|ps1|vbs|hta|msi|scr|dll)\b`, "curl -o to executable file", "curl -o to executable file"),
	// Invoke-WebRequest / iwr
	p(`\b(invoke-webrequest|iwr)\b[^\n]*-outfile\s+['"]?[^\s'"|&;]+\.(exe|bat|cmd|ps1|vbs|hta|msi|scr|dll)\b`, "Invoke-WebRequest to executable file", "Invoke-WebRequest to executable file"),
	// Exfil staging
	p(`\bcompress-archive\b[^\n]*(\\\.ssh\b|\\\.hermes\b|\\appdata\\|\\sysvol\b|ntds\.dit|sam\b)`, "Compress-Archive of sensitive path (exfil staging)", "Compress-Archive of sensitive path (exfil staging)"),
	// Certificate export
	p(`\bexport-pfxcertificate\b`, "Export-PfxCertificate (private key export)", "Export-PfxCertificate (private key export)"),
	p(`\bcertutil\b[^\n]*-exportpfx\b`, "certutil -exportPFX (private key export)", "certutil -exportPFX (private key export)"),
}

// CatastrophicPatterns is Level 2: double-confirm with exact phrase. 28 entries
// ported verbatim from CATASTROPHIC_PATTERNS in
// pan-desktop/resources/overlays/tools/approval.py.
var CatastrophicPatterns = []Pattern{
	// Volume Shadow Copy deletion (ransomware primitive)
	p(`\bvssadmin\s+delete\s+shadows\b`, "delete Volume Shadow Copies (ransomware primitive)", "delete Volume Shadow Copies (ransomware primitive)"),
	p(`\bwmic\s+shadowcopy\s+delete\b`, "delete Volume Shadow Copies via wmic", "delete Volume Shadow Copies via wmic"),
	p(`\bget-wmiobject\b[^\n]*win32_shadowcopy\b[^\n]*\.delete\(\)`, "delete Volume Shadow Copies via Get-WmiObject", "delete Volume Shadow Copies via Get-WmiObject"),
	// Disk format
	p(`\bformat\s+[a-z]:(?:\s|$|/)`, "format disk", "format disk"),
	// Mass Windows delete against drive root
	p(`\bdel\b[^\n]*\s/s\b[^\n]*\s/q\b[^\n]*\s[a-z]:\\`, "del /s /q against drive root", "del /s /q against drive root"),
	p(`\bdel\b[^\n]*\s/q\b[^\n]*\s/s\b[^\n]*\s[a-z]:\\`, "del /q /s against drive root", "del /q /s against drive root"),
	p(`\b(rmdir|rd)\b[^\n]*\s/s\b[^\n]*\s/q\b[^\n]*\s[a-z]:\\`, "rmdir /s /q against drive root", "rmdir /s /q against drive root"),
	p(`\b(rmdir|rd)\b[^\n]*\s/q\b[^\n]*\s/s\b[^\n]*\s[a-z]:\\`, "rmdir /q /s against drive root", "rmdir /q /s against drive root"),
	// Windows Defender tampering
	p(`\bset-mppreference\b[^\n]*-disable\w+`, "disable Windows Defender protection (Set-MpPreference -Disable*)", "disable Windows Defender protection (Set-MpPreference -Disable*)"),
	p(`\badd-mppreference\b[^\n]*-exclusion(path|process|extension)\b`, "Defender exclusion bypass (Add-MpPreference -Exclusion*)", "Defender exclusion bypass (Add-MpPreference -Exclusion*)"),
	p(`\bstop-service\b[^\n]*\bwindefend\b`, "stop Windows Defender service (Stop-Service)", "stop Windows Defender service (Stop-Service)"),
	p(`\bsc(?:\.exe)?\s+stop\s+windefend\b`, "stop Windows Defender service (sc stop)", "stop Windows Defender service (sc stop)"),
	p(`\bsc(?:\.exe)?\s+config\s+windefend\b[^\n]*start=\s*disabled\b`, "disable Windows Defender service at boot", "disable Windows Defender service at boot"),
	// Boot configuration tampering
	p(`\bbcdedit\b[^\n]*/set\b`, "bcdedit boot configuration modification", "bcdedit boot configuration modification"),
	p(`\bbcdedit\b[^\n]*/deletevalue\b`, "bcdedit boot value deletion", "bcdedit boot value deletion"),
	// cipher /w free-space wipe
	p(`\bcipher\s+/w:`, "cipher /w (secure-wipe free space)", "cipher /w (secure-wipe free space)"),
	// Registry hive deletion
	p(`\breg\s+delete\s+("?hklm\\|hkey_local_machine\\)`, "delete HKLM registry key", "delete HKLM registry key"),
	p(`\breg\s+delete\s+("?hkcu\\|hkey_current_user\\)`, "delete HKCU registry key", "delete HKCU registry key"),
	// Delete local user account
	p(`\bnet\s+user\s+\S+\s+/delete\b`, "delete Windows user account (net user /delete)", "delete Windows user account (net user /delete)"),
	p(`\bremove-localuser\b`, "delete Windows user account (Remove-LocalUser)", "delete Windows user account (Remove-LocalUser)"),
	// Credential dump tooling
	p(`\bmimikatz\b`, "mimikatz credential dumper", "mimikatz credential dumper"),
	p(`\bprocdump(?:\.exe)?\b[^\n]*-ma\b[^\n]*\blsass\b`, "procdump LSASS credential dump", "procdump LSASS credential dump"),
	p(`\brundll32(?:\.exe)?\s+[^\n]*comsvcs\.dll[^\n]*minidump\b`, "comsvcs.dll MiniDump (LSASS dump LOLBIN)", "comsvcs.dll MiniDump (LSASS dump LOLBIN)"),
	// Active Directory database
	p(`\bntdsutil\b`, "ntdsutil (Active Directory database access)", "ntdsutil (Active Directory database access)"),
	// Anti-forensics: clear Windows event logs
	p(`\bwevtutil\s+cl\b`, "clear Windows event log (wevtutil cl)", "clear Windows event log (wevtutil cl)"),
	p(`\bclear-eventlog\b`, "clear Windows event log (Clear-EventLog)", "clear Windows event log (Clear-EventLog)"),
	p(`\bwevtutil\s+sl\b[^\n]*/e:false\b`, "disable Windows event log channel (wevtutil sl /e:false)", "disable Windows event log channel (wevtutil sl /e:false)"),
	// Disable Volume Shadow Copy service
	p(`\bsc(?:\.exe)?\s+(stop|delete|config)\s+vss\b`, "disable Volume Shadow Copy service", "disable Volume Shadow Copy service"),

	// -------------------------------------------------------------------
	// Unix-side catastrophic commands (previously only Dangerous / level-1).
	// Promoted here because they are system-destructive or wormable and
	// deserve typed confirmation rather than a single-click approval.
	// -------------------------------------------------------------------

	// Recursive delete anchored at / or a protected system root. Matches
	// any combination of -r/-R/-f or --recursive/--force in any order.
	p(`\brm\s+(-[rRfF]+\s*|--(recursive|force)\s+){1,}/\s*($|\*|etc\b|usr\b|bin\b|sbin\b|boot\b|var\b|lib\b|system\b|library\b)`,
		"recursive delete at system root", "recursive delete at system root"),
	// dd writing to a raw block device.
	p(`\bdd\b[^\n]*\bof=\s*/dev/(sd|nvme|hd|mmcblk|loop)`, "dd to raw block device", "dd to raw block device"),
	// mkfs targeting a device.
	p(`\bmkfs(?:\.[a-z0-9]+)?\s+[^\n]*/dev/`, "mkfs on a device", "mkfs on a device"),
	// curl/wget piped to a shell — self-propagating remote-exec primitive.
	p(`\b(curl|wget)\b[^\n]*\|\s*(ba)?sh\b`, "pipe remote content to shell (catastrophic)", "pipe remote content to shell (catastrophic)"),
	// tee / redirect into /etc/passwd, /etc/shadow, /etc/sudoers.
	p(`(>>?|tee)\s+["']?/etc/(passwd|shadow|sudoers|ssh/)`,
		"overwrite critical system file", "overwrite critical system file"),
}
