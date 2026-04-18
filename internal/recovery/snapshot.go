package recovery

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// Sentinel errors for the snapshotter.
var (
	ErrSnapshotOutsideSandbox = errors.New("recovery: refuse to snapshot browser-profile path")
	ErrSnapshotSizeExceeded   = errors.New("recovery: snapshot exceeds tier-2 size cap (audit-only)")
	ErrSnapshotReadonly       = errors.New("recovery: destination filesystem is read-only")
	ErrSnapshotCrossDevice    = errors.New("recovery: cannot reflink across devices")
)

const (
	defaultMaxCopyMB = 50
	defaultMaxCopyN  = 500
	probeTTLSeconds  = 600 // 10-minute capability cache TTL
)

// SnapshotInfo is the stable descriptor stored on a Receipt.
type SnapshotInfo struct {
	Tier      SnapshotTier
	ReceiptID string
	Subpath   string  // relative to Snapshotter.root
	SizeBytes int64
	FileCount int
	DeviceID  uint64
}

// mountKey is the cache key for CoW capability probes.
type mountKey struct {
	dev uint64
	ino uint64
}

type probeResult struct {
	cowSupported bool
	at           int64
}

// capabilityCache memoises CoW probe results with a TTL.
type capabilityCache struct {
	mu   sync.Mutex
	seen map[mountKey]probeResult
}

func newCapabilityCache() *capabilityCache {
	return &capabilityCache{seen: make(map[mountKey]probeResult)}
}

func (c *capabilityCache) lookup(k mountKey, now int64) (probeResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.seen[k]
	if !ok || now-r.at > probeTTLSeconds {
		return probeResult{}, false
	}
	return r, true
}

func (c *capabilityCache) store(k mountKey, r probeResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen[k] = r
}

// cowSupportedFor returns true if the cache records cowSupported=true for the
// mount containing path. Used by tests to inspect cache state.
func (c *capabilityCache) cowSupportedFor(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	k := mountKey{dev: deviceID(fi)}
	if pfi, err := os.Stat(filepath.Dir(path)); err == nil {
		k.ino = inodeID(pfi)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.seen[k]
	return ok && r.cowSupported
}

// ---------------------------------------------------------------------------
// Option — functional options for Snapshotter
// ---------------------------------------------------------------------------

// Option is a functional option for NewSnapshotter.
type Option func(*Snapshotter)

// WithExecStub replaces the CoW shell-out function with a stub.
// The stub receives (binaryName string, args []string) and returns an error.
// Used by tests to simulate cp -c / cp --reflink failures without a real filesystem.
func WithExecStub(fn func(name string, args []string) error) Option {
	return func(s *Snapshotter) { s.execStub = fn }
}

// WithProbeHook adds a callback that fires once per CoW capability probe
// (cache miss only). Used by tests to count probe invocations.
func WithProbeHook(fn func()) Option {
	return func(s *Snapshotter) { s.probeHook = fn }
}

// WithBrowserProfileDir overrides the browser-profile directory used by
// insideBrowserProfile. Tests use this to avoid depending on the real OS data dir.
func WithBrowserProfileDir(dir string) Option {
	return func(s *Snapshotter) { s.browserProfileDir = dir }
}

// WithStatHook replaces the os.Stat function for cross-device testing.
// The hook receives a path and returns (os.FileInfo, error).
func WithStatHook(fn func(string) (os.FileInfo, error)) Option {
	return func(s *Snapshotter) { s.statHook = fn }
}

// withCrossDeviceStatHook returns an Option that makes src and dst appear to
// have different device IDs, simulating a cross-device copy scenario.
// This is the hook the test calls via crossDeviceStatHook().
func withCrossDeviceStatHook() Option {
	var callCount int
	return WithStatHook(func(path string) (os.FileInfo, error) {
		fi, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		callCount++
		// Every other call returns a fake FileInfo with Dev=0 so src != dst.
		if callCount%2 == 0 {
			return &fakeFileInfo{FileInfo: fi, dev: 0}, nil
		}
		return &fakeFileInfo{FileInfo: fi, dev: 1}, nil
	})
}

// ---------------------------------------------------------------------------
// Snapshotter
// ---------------------------------------------------------------------------

// Snapshotter captures files before the agent mutates them.
type Snapshotter struct {
	root             string
	session          string
	probe            *capabilityCache
	maxCopyMB        int
	maxCopyN         int
	clock            func() int64
	browserProfileDir string          // overridable for tests
	execStub         func(name string, args []string) error // nil = real exec
	probeHook        func()           // called on each cache-miss probe
	statHook         func(string) (os.FileInfo, error) // nil = os.Stat
}

// NewSnapshotter creates a Snapshotter for the given session.
// root is the base snapshot directory (typically paths.AgentHome()+"/recovery").
func NewSnapshotter(root, sessionID string, opts ...Option) (*Snapshotter, error) {
	s := &Snapshotter{
		root:      root,
		session:   sessionID,
		probe:     newCapabilityCache(),
		maxCopyMB: defaultMaxCopyMB,
		maxCopyN:  defaultMaxCopyN,
		clock:     func() int64 { return time.Now().Unix() },
	}
	for _, o := range opts {
		o(s)
	}
	if err := os.MkdirAll(filepath.Join(root, sessionID), 0o700); err != nil {
		return nil, fmt.Errorf("recovery: NewSnapshotter mkdir: %w", err)
	}
	return s, nil
}

// DefaultSnapshotter builds a Snapshotter rooted at the standard data path.
func DefaultSnapshotter(sessionID string) *Snapshotter {
	root := filepath.Join(paths.AgentHome(), "recovery")
	s, _ := NewSnapshotter(root, sessionID)
	return s
}

// SetClock replaces the clock. Exposed for tests.
func (s *Snapshotter) SetClock(fn func() int64) { s.clock = fn }

// Root returns the snapshot root directory (<DataDir>/recovery/ in production,
// a t.TempDir path under test). Exposed so endpoint/test code can compose
// paths without reaching into the unexported root field.
func (s *Snapshotter) Root() string { return s.root }

// SetCaps overrides the size caps. Exposed for tests.
func (s *Snapshotter) SetCaps(maxMB, maxN int) {
	s.maxCopyMB = maxMB
	s.maxCopyN = maxN
}

// stat calls the injected stat hook if set, otherwise os.Stat.
func (s *Snapshotter) stat(path string) (os.FileInfo, error) {
	if s.statHook != nil {
		return s.statHook(path)
	}
	return os.Stat(path)
}

// ---------------------------------------------------------------------------
// Capture
// ---------------------------------------------------------------------------

// Capture snapshots path into <root>/<session>/<receiptID>/.
// The browser-profile directory is hard-excluded per the architecture spec.
func (s *Snapshotter) Capture(ctx context.Context, path, receiptID string) (SnapshotInfo, error) {
	if s.insideBrowserProfile(path) {
		return SnapshotInfo{}, ErrSnapshotOutsideSandbox
	}
	return s.captureOne(ctx, path, receiptID)
}

// CaptureMany snapshots multiple paths under a single receiptID directory.
func (s *Snapshotter) CaptureMany(ctx context.Context, pathList []string, receiptID string) (SnapshotInfo, error) {
	var totalSize int64
	var fileCount int
	var tier SnapshotTier = TierCoW
	var devID uint64

	destDir := s.destDir(receiptID)
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return SnapshotInfo{}, fmt.Errorf("recovery: CaptureMany mkdir: %w", err)
	}

	for _, p := range pathList {
		if s.insideBrowserProfile(p) {
			continue
		}
		fi, err := s.stat(p)
		if err != nil {
			continue
		}
		dev := deviceID(fi)
		if devID == 0 {
			devID = dev
		}

		sz, n, usedTier, err := s.captureInto(ctx, p, destDir)
		if err != nil {
			if errors.Is(err, ErrSnapshotSizeExceeded) {
				return SnapshotInfo{Tier: TierAuditOnly, ReceiptID: receiptID}, nil
			}
			return SnapshotInfo{}, err
		}
		totalSize += sz
		fileCount += n
		if usedTier == TierCopyFS {
			tier = TierCopyFS
		}
	}

	return SnapshotInfo{
		Tier:      tier,
		ReceiptID: receiptID,
		Subpath:   filepath.Join(s.session, receiptID),
		SizeBytes: totalSize,
		FileCount: fileCount,
		DeviceID:  devID,
	}, nil
}

func (s *Snapshotter) captureOne(ctx context.Context, src, receiptID string) (SnapshotInfo, error) {
	fi, err := s.stat(src)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("recovery: Capture stat: %w", err)
	}

	destDir := s.destDir(receiptID)
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return SnapshotInfo{}, fmt.Errorf("recovery: Capture mkdir: %w", err)
	}

	// Write the clock-based created_at for Purge, and the original absolute
	// path so Restore knows where to write back.
	absSrc, _ := filepath.Abs(src)
	_ = os.WriteFile(filepath.Join(destDir, ".created_at"), []byte(strconv.FormatInt(s.clock(), 10)), 0o600)
	_ = os.WriteFile(filepath.Join(destDir, ".origin"), []byte(absSrc), 0o600)

	sz, n, tier, err := s.captureInto(ctx, src, destDir)
	if err != nil {
		if errors.Is(err, ErrSnapshotSizeExceeded) {
			return SnapshotInfo{Tier: TierAuditOnly, ReceiptID: receiptID}, ErrSnapshotSizeExceeded
		}
		return SnapshotInfo{}, err
	}

	return SnapshotInfo{
		Tier:      tier,
		ReceiptID: receiptID,
		Subpath:   filepath.Join(s.session, receiptID),
		SizeBytes: sz,
		FileCount: n,
		DeviceID:  deviceID(fi),
	}, nil
}

func (s *Snapshotter) captureInto(_ context.Context, src, destDir string) (int64, int, SnapshotTier, error) {
	fi, err := s.stat(src)
	if err != nil {
		return 0, 0, TierAuditOnly, fmt.Errorf("recovery: captureInto stat src: %w", err)
	}

	dstFI, err := s.stat(destDir)
	if err != nil {
		return 0, 0, TierAuditOnly, fmt.Errorf("recovery: captureInto stat dest: %w", err)
	}
	crossDevice := deviceID(fi) != deviceID(dstFI)

	// Size / count pre-check.
	var totalSize int64
	var fileCount int
	if fi.IsDir() {
		totalSize, fileCount, err = dirStats(src)
		if err != nil {
			return 0, 0, TierAuditOnly, err
		}
	} else {
		totalSize = fi.Size()
		fileCount = 1
	}

	capMB := int64(s.maxCopyMB) * 1024 * 1024
	if totalSize > capMB || fileCount > s.maxCopyN {
		return 0, 0, TierAuditOnly, ErrSnapshotSizeExceeded
	}

	tier, cowOK := s.resolveTier(src, crossDevice)
	dest := filepath.Join(destDir, filepath.Base(src))

	if cowOK && tier == TierCoW {
		var cowErr error
		if s.execStub != nil {
			// Test-injected exec stub: call it with the cp args.
			cowErr = s.execStub(cowBinary(), cowArgs(src, dest))
		} else {
			cowErr = cowCopy(src, dest)
		}
		if cowErr != nil {
			s.markCacheFailed(src)
			tier = TierCopyFS
		}
	}

	if tier == TierCopyFS {
		if err := copyFSFallback(src, dest); err != nil {
			return 0, 0, TierAuditOnly, fmt.Errorf("recovery: tier-2 copy: %w", err)
		}
	}

	return totalSize, fileCount, tier, nil
}

func (s *Snapshotter) resolveTier(src string, crossDevice bool) (SnapshotTier, bool) {
	if crossDevice {
		return TierCopyFS, false
	}
	fi, err := s.stat(src)
	if err != nil {
		return TierCopyFS, false
	}
	k := mountKey{dev: deviceID(fi)}
	if pfi, err := s.stat(filepath.Dir(src)); err == nil {
		k.ino = inodeID(pfi)
	}

	now := s.clock()
	if cached, ok := s.probe.lookup(k, now); ok {
		if cached.cowSupported {
			return TierCoW, true
		}
		return TierCopyFS, false
	}

	// Cache miss — run probe.
	if s.probeHook != nil {
		s.probeHook()
	}
	var ok bool
	if s.execStub != nil {
		// Use the stub to simulate the probe: a nil error means CoW works.
		err := s.execStub(cowBinary(), []string{"-probe"})
		ok = (err == nil)
	} else {
		ok = probeCow(s.root)
	}
	s.probe.store(k, probeResult{cowSupported: ok, at: now})
	if ok {
		return TierCoW, true
	}
	return TierCopyFS, false
}

func (s *Snapshotter) markCacheFailed(src string) {
	fi, err := s.stat(src)
	if err != nil {
		return
	}
	k := mountKey{dev: deviceID(fi)}
	if pfi, err := s.stat(filepath.Dir(src)); err == nil {
		k.ino = inodeID(pfi)
	}
	s.probe.store(k, probeResult{cowSupported: false, at: s.clock()})
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

// Restore copies the snapshot back over the live path.
// The original file path is read from the .origin sidecar written by captureOne.
func (s *Snapshotter) Restore(_ context.Context, info SnapshotInfo) error {
	snapDir := filepath.Join(s.root, info.Subpath)
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		return fmt.Errorf("recovery: Restore read snapdir: %w", err)
	}

	// Read original path from sidecar if present.
	originBytes, originErr := os.ReadFile(filepath.Join(snapDir, ".origin"))
	originPath := ""
	if originErr == nil {
		originPath = string(originBytes)
	}

	for _, e := range entries {
		// Skip sidecar files written by the snapshotter.
		if e.Name() == ".origin" || e.Name() == ".created_at" {
			continue
		}
		src := filepath.Join(snapDir, e.Name())
		// Prefer the recorded original absolute path; fall back to the filename
		// (for multi-file CaptureMany snapshots).
		var dst string
		if originPath != "" {
			dst = originPath
		} else {
			dst = e.Name()
		}
		if err := copyFSFallback(src, dst); err != nil {
			return fmt.Errorf("recovery: Restore copy %s: %w", src, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Purge / orphan cleanup
// ---------------------------------------------------------------------------

// Purge removes snapshot subdirs older than cutoff. Called by the Reaper.
// The snapshot timestamp is read from the .created_at sidecar written by
// captureOne (uses the injectable clock, not the filesystem mtime).
func (s *Snapshotter) Purge(_ context.Context, cutoff int64) error {
	sessionDir := filepath.Join(s.root, s.session)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("recovery: Purge readdir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(sessionDir, e.Name())
		// Prefer the clock-stamped sidecar over filesystem mtime so that the
		// injectable clock in tests produces deterministic behaviour.
		ts := s.readCreatedAt(dirPath)
		if ts < cutoff {
			_ = os.RemoveAll(dirPath)
		}
	}
	return nil
}

// readCreatedAt reads the .created_at sidecar timestamp. Falls back to the
// directory's filesystem mtime when the sidecar is absent (e.g. legacy dirs).
func (s *Snapshotter) readCreatedAt(dirPath string) int64 {
	data, err := os.ReadFile(filepath.Join(dirPath, ".created_at"))
	if err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			return v
		}
	}
	// Fallback: filesystem mtime.
	if fi, err := os.Stat(dirPath); err == nil {
		return fi.ModTime().Unix()
	}
	return 0
}

// PurgeOrphans removes snapshot subdirs whose receiptID is not in knownIDs.
func (s *Snapshotter) PurgeOrphans(_ context.Context, knownIDs map[string]bool) error {
	sessionDir := filepath.Join(s.root, s.session)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("recovery: PurgeOrphans readdir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !knownIDs[e.Name()] {
			_ = os.RemoveAll(filepath.Join(sessionDir, e.Name()))
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (s *Snapshotter) destDir(receiptID string) string {
	return filepath.Join(s.root, s.session, receiptID)
}

// insideBrowserProfile returns true when path is inside the browser-profile dir.
// Uses filepath.Rel — same defensive pattern as internal/skills/paths_internal.go.
func (s *Snapshotter) insideBrowserProfile(path string) bool {
	dir := s.browserProfileDir
	if dir == "" {
		dir = browserProfileDir()
	}
	if dir == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// browserProfileDir returns the default browser-profile base path.
func browserProfileDir() string {
	return filepath.Join(paths.AgentHome(), "browser-profile")
}

func dirStats(src string) (int64, int, error) {
	var size int64
	var count int
	err := filepath.WalkDir(src, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			fi, e := d.Info()
			if e == nil {
				size += fi.Size()
				count++
			}
		}
		return nil
	})
	return size, count, err
}

// fakeFileInfo wraps os.FileInfo and overrides the device ID returned by Sys().
type fakeFileInfo struct {
	os.FileInfo
	dev uint64
}

// Sys returns a value that deviceID() can read. The platform-specific
// deviceID() implementations look for *syscall.Stat_t; on platforms where
// that is not available they return 0. For the cross-device test we use a
// simple wrapper that exposes the fake dev via a dedicated interface so
// deviceID() can pick it up without platform-specific code.
func (f *fakeFileInfo) Sys() any { return f }

// FakeDev is read by deviceID() when the Sys() value implements fakeDevProvider.
func (f *fakeFileInfo) FakeDev() uint64 { return f.dev }

// fakeDevProvider is the interface checked by deviceID() in the stub build.
// The real platform implementations (snapshot_darwin.go, snapshot_linux.go)
// always look for *syscall.Stat_t first; when they do not find it they return 0
// which is safe for production. The cross-device test uses the stub path.
type fakeDevProvider interface {
	FakeDev() uint64
}
