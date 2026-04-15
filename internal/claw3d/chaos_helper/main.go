// Package main is the chaos-test helper: a tiny cross-platform
// sleep-forever binary used by internal/claw3d/chaos_test.go as a
// dummy parent process for the parentwatch scenario.
//
// Why a dedicated binary instead of `sleep 60` (Unix) or `timeout /T 60`
// (Windows)? Go tests need one invocation that works everywhere — shelling
// out to platform-specific sleep commands adds platform branches to the
// test driver and breaks on minimal CI images that strip coreutils. A
// 5-line Go binary built by the test fixture (or by `go test`'s TestMain)
// is always available and behaves identically on all OSes.
//
// The binary exits naturally after 2 minutes as a safety net — if a
// chaos test forgets to SIGKILL it, the helper still reaps itself.
package main

import "time"

func main() {
	time.Sleep(2 * time.Minute)
}
