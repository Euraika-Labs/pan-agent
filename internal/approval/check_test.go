package approval

import "testing"

func TestCheckCatastrophicVssadmin(t *testing.T) {
	result := Check("vssadmin delete shadows /all /quiet")
	if result.Level != Catastrophic {
		t.Errorf("vssadmin delete shadows: level = %d, want %d (Catastrophic)", result.Level, Catastrophic)
	}
}

func TestCheckCatastrophicWmicShadowcopy(t *testing.T) {
	result := Check("wmic shadowcopy delete")
	if result.Level != Catastrophic {
		t.Errorf("wmic shadowcopy delete: level = %d, want %d (Catastrophic)", result.Level, Catastrophic)
	}
}

func TestCheckCatastrophicBcdedit(t *testing.T) {
	result := Check("bcdedit /set {default} recoveryenabled No")
	if result.Level != Catastrophic {
		t.Errorf("bcdedit /set: level = %d, want %d (Catastrophic)", result.Level, Catastrophic)
	}
}

func TestCheckCatastrophicFormatDisk(t *testing.T) {
	result := Check("format C: /FS:NTFS")
	if result.Level != Catastrophic {
		t.Errorf("format C:: level = %d, want %d (Catastrophic)", result.Level, Catastrophic)
	}
}

func TestCheckCatastrophicMimikatz(t *testing.T) {
	result := Check("mimikatz sekurlsa::logonpasswords")
	if result.Level != Catastrophic {
		t.Errorf("mimikatz: level = %d, want %d (Catastrophic)", result.Level, Catastrophic)
	}
}

func TestCheckDangerousRmRF(t *testing.T) {
	result := Check("rm -rf /")
	if result.Level != Dangerous {
		t.Errorf("rm -rf /: level = %d, want %d (Dangerous)", result.Level, Dangerous)
	}
}

func TestCheckDangerousRmRecursive(t *testing.T) {
	result := Check("rm -r /tmp/foo")
	if result.Level != Dangerous {
		t.Errorf("rm -r /tmp/foo: level = %d, want %d (Dangerous)", result.Level, Dangerous)
	}
}

func TestCheckDangerousSQLDrop(t *testing.T) {
	result := Check("DROP TABLE users")
	if result.Level != Dangerous {
		t.Errorf("DROP TABLE: level = %d, want %d (Dangerous)", result.Level, Dangerous)
	}
}

func TestCheckDangerousGitResetHard(t *testing.T) {
	result := Check("git reset --hard HEAD~1")
	if result.Level != Dangerous {
		t.Errorf("git reset --hard: level = %d, want %d (Dangerous)", result.Level, Dangerous)
	}
}

func TestCheckDangerousKillAll(t *testing.T) {
	result := Check("kill -9 -1")
	if result.Level != Dangerous {
		t.Errorf("kill -9 -1: level = %d, want %d (Dangerous)", result.Level, Dangerous)
	}
}

func TestCheckSafeEchoHello(t *testing.T) {
	result := Check("echo hello")
	if result.Level != Safe {
		t.Errorf("echo hello: level = %d, want %d (Safe)", result.Level, Safe)
	}
}

func TestCheckSafeLs(t *testing.T) {
	result := Check("ls -la /tmp")
	if result.Level != Safe {
		t.Errorf("ls -la: level = %d, want %d (Safe)", result.Level, Safe)
	}
}

func TestCheckSafeGitStatus(t *testing.T) {
	result := Check("git status")
	if result.Level != Safe {
		t.Errorf("git status: level = %d, want %d (Safe)", result.Level, Safe)
	}
}

func TestCheckSafeGoTest(t *testing.T) {
	result := Check("go test ./...")
	if result.Level != Safe {
		t.Errorf("go test: level = %d, want %d (Safe)", result.Level, Safe)
	}
}

// Catastrophic takes precedence over Dangerous.
func TestCheckCatastrophicPrecedence(t *testing.T) {
	// del /s /q C:\ also matches a dangerous pattern, but should be Catastrophic.
	result := Check(`del /s /q C:\`)
	if result.Level != Catastrophic {
		t.Errorf("del /s /q C:\\: level = %d, want %d (Catastrophic)", result.Level, Catastrophic)
	}
}

// SQL DELETE with WHERE clause should be Safe (not Dangerous).
func TestCheckSQLDeleteWithWhere(t *testing.T) {
	result := Check("DELETE FROM users WHERE id = 1")
	if result.Level != Safe {
		t.Errorf("DELETE FROM ... WHERE: level = %d, want %d (Safe)", result.Level, Safe)
	}
}

// SQL DELETE without WHERE clause should be Dangerous.
func TestCheckSQLDeleteWithoutWhere(t *testing.T) {
	result := Check("DELETE FROM users")
	if result.Level != Dangerous {
		t.Errorf("DELETE FROM (no WHERE): level = %d, want %d (Dangerous)", result.Level, Dangerous)
	}
}

// Verify that empty string is safe.
func TestCheckSafeEmpty(t *testing.T) {
	result := Check("")
	if result.Level != Safe {
		t.Errorf("empty command: level = %d, want %d (Safe)", result.Level, Safe)
	}
}

// Level constants have expected values.
func TestLevelConstants(t *testing.T) {
	if Safe != 0 {
		t.Errorf("Safe = %d, want 0", Safe)
	}
	if Dangerous != 1 {
		t.Errorf("Dangerous = %d, want 1", Dangerous)
	}
	if Catastrophic != 2 {
		t.Errorf("Catastrophic = %d, want 2", Catastrophic)
	}
}
