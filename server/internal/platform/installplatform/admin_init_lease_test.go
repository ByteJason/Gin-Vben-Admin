package installplatform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminInitLeasePreservesLiveSchemaTwoOwnerEvenWhenRuntimeTokensDiffer(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "admin-init.lock")
	contents := []byte(`{"schema":2,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","pid":42,"pidStartToken":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","createdAt":"2026-08-24T00:00:00.000Z"}` + "\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	operations := defaultAdminInitLeaseOperations()
	operations.processAlive = func(pid int) bool { return pid == 42 }
	operations.livenessAvailable = func() bool { return true }

	if err := reconcileStaleAdminInitLeaseWithOperations(path, operations); !errors.Is(err, errProcessLeaseBusy) {
		t.Fatalf("reconcile live cross-runtime owner error = %v, want busy", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(contents) {
		t.Fatalf("live admin lease changed: %q error=%v", got, err)
	}
	if _, err := os.Lstat(path + ".reclaim"); !os.IsNotExist(err) {
		t.Fatalf("live owner left a reclaim tombstone: %v", err)
	}
}

func TestAdminInitLeaseDoesNotRemoveSameBytesReplacement(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "admin-init.lock")
	contents := []byte(`{"schema":2,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","pid":99999999,"pidStartToken":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","createdAt":"2026-08-24T00:00:00.000Z"}` + "\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	operations := defaultAdminInitLeaseOperations()
	operations.processAlive = func(int) bool { return false }
	operations.livenessAvailable = func() bool { return true }
	operations.link = func(oldPath, newPath string) error {
		if err := os.Remove(oldPath); err != nil {
			return err
		}
		if err := os.WriteFile(oldPath, contents, 0o600); err != nil {
			return err
		}
		return os.Link(oldPath, newPath)
	}

	if err := reconcileStaleAdminInitLeaseWithOperations(path, operations); !errors.Is(err, errProcessLeaseBusy) {
		t.Fatalf("reconcile replaced owner error = %v, want busy", err)
	}
	replacementInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("same-bytes replacement was removed: %v", err)
	}
	if os.SameFile(originalInfo, replacementInfo) {
		t.Fatal("fixture did not replace the admin lease inode")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(contents) {
		t.Fatalf("same-bytes replacement changed: %q error=%v", got, err)
	}
	if _, err := os.Lstat(path + ".reclaim"); !os.IsNotExist(err) {
		t.Fatalf("replacement race left a reclaim tombstone: %v", err)
	}
}

func TestAdminInitLeaseRejectsReceiptsNodeWouldNotParse(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		contents string
	}{
		{
			name:     "schema one explicit start token",
			contents: `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","pid":99999999,"pidStartToken":"","createdAt":"2026-08-24T00:00:00.000Z"}` + "\n",
		},
		{
			name:     "non canonical timestamp",
			contents: `{"schema":2,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","pid":99999999,"pidStartToken":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","createdAt":"2026-08-24T00:00:00Z"}` + "\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "admin-init.lock")
			if err := os.WriteFile(path, []byte(testCase.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			operations := defaultAdminInitLeaseOperations()
			operations.processAlive = func(int) bool { return false }
			operations.livenessAvailable = func() bool { return true }

			if err := reconcileStaleAdminInitLeaseWithOperations(path, operations); !errors.Is(err, errProcessLeaseBusy) {
				t.Fatalf("reconcile malformed cross-runtime receipt error = %v, want busy", err)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != testCase.contents {
				t.Fatalf("malformed admin lease changed: %q error=%v", got, err)
			}
		})
	}
}

func TestAdminInitLeaseRecoversOldInterruptedReclaimWithoutDeletingFreshOne(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	contents := []byte(`{"schema":2,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","pid":99999999,"pidStartToken":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","createdAt":"2026-08-24T00:00:00.000Z"}` + "\n")
	for _, phase := range []struct {
		name            string
		removeCanonical bool
	}{
		{name: "after tombstone link"},
		{name: "after canonical remove", removeCanonical: true},
	} {
		t.Run(phase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "admin-init.lock")
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(path, path+".reclaim"); err != nil {
				t.Fatal(err)
			}
			if phase.removeCanonical {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			operations := defaultAdminInitLeaseOperations()
			operations.processAlive = func(int) bool { return false }
			operations.livenessAvailable = func() bool { return true }
			operations.now = func() time.Time { return now }
			operations.changeTime = func(string) (time.Time, bool) { return now.Add(-2 * adminInitReclaimGrace), true }

			if err := reconcileStaleAdminInitLeaseWithOperations(path, operations); err != nil {
				t.Fatalf("reconcile old interrupted reclaim error = %v", err)
			}
			for _, candidate := range []string{path, path + ".reclaim"} {
				if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
					t.Fatalf("interrupted reclaim artifact %s remains: %v", candidate, err)
				}
			}
		})
	}

	t.Run("fresh tombstone remains busy", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "admin-init.lock")
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, path+".reclaim"); err != nil {
			t.Fatal(err)
		}
		operations := defaultAdminInitLeaseOperations()
		operations.processAlive = func(int) bool { return false }
		operations.livenessAvailable = func() bool { return true }
		operations.now = func() time.Time { return now }
		operations.changeTime = func(string) (time.Time, bool) { return now, true }

		if err := reconcileStaleAdminInitLeaseWithOperations(path, operations); !errors.Is(err, errProcessLeaseBusy) {
			t.Fatalf("reconcile fresh interrupted reclaim error = %v, want busy", err)
		}
		for _, candidate := range []string{path, path + ".reclaim"} {
			if got, err := os.ReadFile(candidate); err != nil || string(got) != string(contents) {
				t.Fatalf("fresh reclaim artifact %s changed: %q error=%v", candidate, got, err)
			}
		}
	})

	t.Run("old tombstone never removes a live replacement", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "admin-init.lock")
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, path+".reclaim"); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		live := []byte(`{"schema":2,"owner":"admin-init","id":"abcdefab-1234-1234-1234-123456789abc","pid":42,"pidStartToken":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","createdAt":"2026-08-24T11:59:00.000Z"}` + "\n")
		if err := os.WriteFile(path, live, 0o600); err != nil {
			t.Fatal(err)
		}
		operations := defaultAdminInitLeaseOperations()
		operations.processAlive = func(pid int) bool { return pid == 42 }
		operations.livenessAvailable = func() bool { return true }
		operations.now = func() time.Time { return now }
		operations.changeTime = func(string) (time.Time, bool) { return now.Add(-2 * adminInitReclaimGrace), true }

		if err := reconcileStaleAdminInitLeaseWithOperations(path, operations); !errors.Is(err, errProcessLeaseBusy) {
			t.Fatalf("reconcile old tombstone with live replacement error = %v, want busy", err)
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != string(live) {
			t.Fatalf("live replacement changed: %q error=%v", got, err)
		}
		if got, err := os.ReadFile(path + ".reclaim"); err != nil || string(got) != string(contents) {
			t.Fatalf("old tombstone changed around live replacement: %q error=%v", got, err)
		}
	})
}

func TestAdminInitReclaimChangeTimeTracksHardlinkClaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin-init.lock")
	if err := os.WriteFile(path, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	claimedAt := time.Now()
	if err := os.Link(path, path+".reclaim"); err != nil {
		t.Fatal(err)
	}
	changedAt, available := adminInitReclaimChangeTime(path + ".reclaim")
	if !available {
		t.Skip("admin init reclaim change time is unavailable on this platform")
	}
	if changedAt.Before(claimedAt.Add(-2*time.Second)) || changedAt.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("reclaim change time = %v, claim started at %v", changedAt, claimedAt)
	}
}
