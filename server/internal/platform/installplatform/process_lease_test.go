package installplatform

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessLeaseReclaimsStaleTruncatedReceipt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "truncated.install.lock")
	if err := os.WriteFile(path, []byte(`{"schema":1,"pid":`), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-2 * maximumProcessLease)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	release, err := acquireProcessLease(path)
	if err != nil {
		t.Fatalf("acquireProcessLease() with stale truncated receipt error = %v", err)
	}
	release()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("released replacement lease remains: %v", err)
	}
}

func TestProcessLeasePreservesRecentInvalidReceipt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "invalid.install.lock")
	invalid := []byte(`{"schema":"invalid"}`)
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	if release, err := acquireProcessLease(path); !errors.Is(err, errProcessLeaseBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("acquireProcessLease() error = %v, want %v", err, errProcessLeaseBusy)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(invalid) {
		t.Fatalf("recent invalid receipt changed: got %q want %q", contents, invalid)
	}
}

func TestValidProcessLeaseFailsClosedWhenLivenessIsUnavailable(t *testing.T) {
	t.Parallel()

	if shouldReclaimValidProcessLease(false, false) {
		t.Fatal("valid lease was reclaimable without a reliable process probe")
	}
	if shouldReclaimValidProcessLease(true, true) {
		t.Fatal("valid lease was reclaimable while its owner is alive")
	}
	if !shouldReclaimValidProcessLease(true, false) {
		t.Fatal("valid lease with a confirmed dead owner was not reclaimable")
	}
}

func TestConcurrentStaleProcessLeaseReclaimHasOnlyOneOwner(t *testing.T) {
	const contenders = 32
	for iteration := 0; iteration < 20; iteration++ {
		path := filepath.Join(t.TempDir(), "concurrent.install.lock")
		stale, err := json.Marshal(processLease{
			Schema: processLeaseSchema, PID: 99999999,
			StartToken: strings.Repeat("a", 64),
			CreatedAt:  time.Now().UTC().Add(-time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(stale, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		results := make(chan func() error, contenders)
		for contender := 0; contender < contenders; contender++ {
			go func() {
				<-start
				release, acquireErr := acquireProcessLease(path)
				if acquireErr != nil {
					results <- nil
					return
				}
				results <- release
			}()
		}
		close(start)
		owners := make([]func() error, 0, 1)
		for contender := 0; contender < contenders; contender++ {
			select {
			case release := <-results:
				if release != nil {
					owners = append(owners, release)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("iteration %d timed out waiting for contender %d", iteration, contender)
			}
		}
		for _, release := range owners {
			release()
		}
		if len(owners) != 1 {
			t.Fatalf("iteration %d acquired %d concurrent owners, want 1", iteration, len(owners))
		}
	}
}

func TestProcessLeasesShareOneDirectoryGuard(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.lock")
	secondPath := filepath.Join(dir, "second.lock")

	firstRelease, err := acquireProcessLease(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondRelease, err := acquireProcessLease(secondPath)
	if err != nil {
		firstRelease()
		t.Fatal(err)
	}
	secondRelease()
	firstRelease()

	if _, err := os.Lstat(filepath.Join(dir, "process.guard")); err != nil {
		t.Fatalf("shared process guard missing: %v", err)
	}
	for _, path := range []string{firstPath + ".guard", secondPath + ".guard"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("per-lease guard %q exists: %v", path, err)
		}
	}
}

func TestNewProcessLeaseReceiptsHaveUniqueAcquisitionIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unique.install.lock")
	ids := make([]string, 0, 2)
	for acquisition := 0; acquisition < 2; acquisition++ {
		release, err := acquireProcessLease(path)
		if err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var receipt processLease
		if err := json.Unmarshal(contents, &receipt); err != nil {
			t.Fatal(err)
		}
		if len(receipt.ID) != 32 || strings.Trim(receipt.ID, "0123456789abcdef") != "" {
			t.Fatalf("acquisition id = %q, want 32 lowercase hex characters", receipt.ID)
		}
		ids = append(ids, receipt.ID)
		if err := release(); err != nil {
			t.Fatal(err)
		}
	}
	if ids[0] == ids[1] {
		t.Fatalf("separate acquisitions reused id %q", ids[0])
	}
}

func TestProcessLeaseReclaimsReceiptAfterPIDReuse(t *testing.T) {
	if !processStartIdentitySupported() {
		t.Skip("process start identity is unavailable on this platform")
	}
	actual, ok := processStartToken(os.Getpid())
	if !ok {
		t.Skip("current process start token is unavailable")
	}
	fake := strings.Repeat("0", 64)
	if fake == actual {
		fake = strings.Repeat("f", 64)
	}
	path := filepath.Join(t.TempDir(), "pid-reused.install.lock")
	encoded, err := json.Marshal(processLease{
		Schema: processLeaseSchema, PID: os.Getpid(), StartToken: fake,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	release, err := acquireProcessLease(path)
	if err != nil {
		t.Fatalf("AcquireProcessLease() after PID reuse error = %v", err)
	}
	release()
}

func TestLegacyLiveProcessLeaseFailsClosedBeyondMigrationWindow(t *testing.T) {
	if !processStartIdentitySupported() {
		t.Skip("process start identity is unavailable on this platform")
	}
	path := filepath.Join(t.TempDir(), "legacy.install.lock")
	encoded, err := json.Marshal(processLease{
		Schema: legacyProcessLeaseSchema, PID: os.Getpid(),
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if release, err := acquireProcessLease(path); !errors.Is(err, errProcessLeaseBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("recent legacy AcquireProcessLease() error = %v, want busy", err)
	}
	stale := time.Now().Add(-2 * maximumProcessLease)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	if release, err := acquireProcessLease(path); !errors.Is(err, errProcessLeaseBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("old live legacy AcquireProcessLease() error = %v, want busy", err)
	}
}

func TestCurrentProcessLeaseFailsClosedWhenStartProbeIsUnavailable(t *testing.T) {
	if shouldReclaimCurrentProcessLease(true, true, false, false, maximumProcessLease/2) {
		t.Fatal("recent live receipt was reclaimed during a transient start-token probe failure")
	}
	if shouldReclaimCurrentProcessLease(true, true, false, false, 2*maximumProcessLease) {
		t.Fatal("old live receipt was reclaimed during a start-token probe failure")
	}
	if shouldReclaimCurrentProcessLease(true, true, true, true, 100*maximumProcessLease) {
		t.Fatal("matching live process identity was reclaimed by age")
	}
	if !shouldReclaimCurrentProcessLease(true, true, true, false, 0) {
		t.Fatal("reused PID with a different process identity was not reclaimed")
	}
	if shouldReclaimCurrentProcessLease(true, false, false, false, 100*maximumProcessLease) {
		t.Fatal("unsupported platform reclaimed a valid live receipt")
	}
}

func TestOldTokenlessLeaseOwnedByALiveProcessRemainsBusy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokenless.install.lock")
	encoded, err := json.Marshal(processLease{
		Schema: processLeaseSchema, PID: os.Getpid(),
		CreatedAt: time.Now().UTC().Add(-10 * maximumProcessLease),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-10 * maximumProcessLease)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}

	if release, err := acquireProcessLease(path); !errors.Is(err, errProcessLeaseBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("AcquireProcessLease() error = %v, want busy", err)
	}
}

func TestProcessLeaseCanAcquireWithoutAnOwnStartToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fallback.install.lock")
	release, err := acquireProcessLeaseLockedWithStartToken(path, filepath.Join(dir, "process.guard"), "")
	if err != nil {
		t.Fatalf("acquire without own start token error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt processLease
	if err := json.Unmarshal(contents, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Schema != processLeaseSchema || receipt.PID != os.Getpid() || receipt.StartToken != "" {
		t.Fatalf("fallback receipt = %#v", receipt)
	}
	release()
}

func TestOldReleaseCannotRemoveReplacementWhileProcessGuardIsHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release-race.install.lock")
	releaseOld, err := acquireProcessLease(path)
	if err != nil {
		t.Fatal(err)
	}
	guardPath := filepath.Join(dir, "process.guard")
	releaseGuard, err := acquireProcessLeaseGuard(guardPath)
	if err != nil {
		t.Fatal(err)
	}
	releaseDone := make(chan struct{})
	go func() {
		releaseOld()
		close(releaseDone)
	}()
	time.Sleep(25 * time.Millisecond)
	if _, err := os.Lstat(path); err != nil {
		releaseGuard()
		t.Fatalf("old owner removed its lease without the process guard: %v", err)
	}
	if err := os.Remove(path); err != nil {
		releaseGuard()
		t.Fatal(err)
	}
	replacement := []byte("replacement-owner\n")
	if err := publishProcessLease(path, replacement); err != nil {
		releaseGuard()
		t.Fatal(err)
	}
	releaseGuard()
	select {
	case <-releaseDone:
	case <-time.After(2 * time.Second):
		t.Fatal("old release did not finish after process guard unlock")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != string(replacement) {
		t.Fatalf("replacement lease changed: contents=%q err=%v", contents, err)
	}
}

func TestPublishProcessLeaseUsesThePreLinkIdentityWithoutASecondFallibleStat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apply.lock")
	contents := []byte("lease-owner\n")
	statCalls := 0
	acquired, err := publishProcessLeaseWithIdentityUsing(path, contents, func(target string) (os.FileInfo, error) {
		statCalls++
		if target == path {
			return nil, errors.New("transient final-path lstat failure")
		}
		return os.Lstat(target)
	})
	if err != nil {
		t.Fatalf("publishProcessLeaseWithIdentityUsing() error = %v", err)
	}
	if statCalls != 1 {
		t.Fatalf("lstat calls = %d, want only the pre-link temporary identity", statCalls)
	}
	published, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(acquired, published) {
		t.Fatal("published lease does not retain the pre-link inode identity")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(contents) {
		t.Fatalf("published lease = %q error=%v", got, err)
	}
}

func TestProcessLeaseReleaseRetriesTransientIdentityReadAndRemoveFailures(t *testing.T) {
	for _, operation := range []string{"lstat", "read", "remove"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "apply.lock")
			guardPath := filepath.Join(dir, "process.guard")
			release, err := acquireProcessLeaseWithGuard(path, guardPath)
			if err != nil {
				t.Fatal(err)
			}
			acquired, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			operations := defaultProcessLeaseReleaseOperations()
			calls := 0
			transient := errors.New("transient " + operation)
			switch operation {
			case "lstat":
				original := operations.lstat
				operations.lstat = func(target string) (os.FileInfo, error) {
					calls++
					if calls == 1 {
						return nil, transient
					}
					return original(target)
				}
			case "read":
				original := operations.read
				operations.read = func(target string, limit int64) ([]byte, bool, error) {
					calls++
					if calls == 1 {
						return nil, true, transient
					}
					return original(target, limit)
				}
			case "remove":
				original := operations.remove
				operations.remove = func(target string) error {
					calls++
					if calls == 1 {
						return transient
					}
					return original(target)
				}
			}

			if err := removeOwnedProcessLeaseWithOperations(path, guardPath, acquired, expected, operations); err != nil {
				t.Fatalf("release after transient %s error = %v", operation, err)
			}
			if calls < 2 {
				t.Fatalf("%s calls = %d, want bounded retry", operation, calls)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("owned lease remains after transient %s recovery: %v", operation, err)
			}
			_ = release()
		})
	}
}

func TestProcessLeaseReleaseReportsPersistentIdentityReadAndRemoveFailures(t *testing.T) {
	for _, operation := range []string{"lstat", "read", "remove"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "apply.lock")
			guardPath := filepath.Join(dir, "process.guard")
			release, err := acquireProcessLeaseWithGuard(path, guardPath)
			if err != nil {
				t.Fatal(err)
			}
			acquired, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			operations := defaultProcessLeaseReleaseOperations()
			calls := 0
			persistent := errors.New("persistent " + operation)
			switch operation {
			case "lstat":
				operations.lstat = func(string) (os.FileInfo, error) {
					calls++
					return nil, persistent
				}
			case "read":
				operations.read = func(string, int64) ([]byte, bool, error) {
					calls++
					return nil, true, persistent
				}
			case "remove":
				operations.remove = func(string) error {
					calls++
					return persistent
				}
			}

			if err := removeOwnedProcessLeaseWithOperations(path, guardPath, acquired, expected, operations); err == nil {
				t.Fatalf("persistent %s release error = nil", operation)
			}
			if calls != processLeaseReleaseAttempts {
				t.Fatalf("%s calls = %d, want %d", operation, calls, processLeaseReleaseAttempts)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != string(expected) {
				t.Fatalf("owned lease changed after persistent %s failure: %q error=%v", operation, got, err)
			}
			_ = release()
		})
	}
}
