//go:build unix

package installplatform

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessLeaseNeverReclaimsAnOldLeaseOwnedByALiveProcess(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "active.install.lock")
	startToken, ok := processStartToken(os.Getpid())
	if !ok {
		t.Skip("process start token is unavailable")
	}
	lease := processLease{
		Schema:     processLeaseSchema,
		PID:        os.Getpid(),
		StartToken: startToken,
		CreatedAt:  time.Now().UTC().Add(-10 * maximumProcessLease),
	}
	encoded, err := json.Marshal(lease)
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
		t.Fatalf("acquireProcessLease() error = %v, want %v", err, errProcessLeaseBusy)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(active lease) error = %v", err)
	}
	if string(contents) != string(encoded) {
		t.Fatalf("active lease was replaced: got %q want %q", contents, encoded)
	}
}

func TestProcessGuardSymlinksFailClosedWithoutTouchingTheirTargets(t *testing.T) {
	for _, dangling := range []bool{false, true} {
		name := "existing target"
		if dangling {
			name = "dangling target"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			leasePath := filepath.Join(dir, "apply.lock")
			guardPath := filepath.Join(dir, "process.guard")
			target := filepath.Join(dir, "external.guard")
			if !dangling {
				if err := os.WriteFile(target, []byte("external-bytes\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, guardPath); err != nil {
				t.Fatal(err)
			}

			if release, err := acquireProcessLeaseWithGuard(leasePath, guardPath); !errors.Is(err, errProcessLeaseBusy) {
				if release != nil {
					_ = release()
				}
				t.Fatalf("acquireProcessLeaseWithGuard() error = %v, want busy", err)
			}
			if _, err := os.Lstat(leasePath); !os.IsNotExist(err) {
				t.Fatalf("lease published through invalid guard: %v", err)
			}
			if dangling {
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatalf("dangling target was created: %v", err)
				}
			} else if got, err := os.ReadFile(target); err != nil || string(got) != "external-bytes\n" {
				t.Fatalf("external guard changed: %q error=%v", got, err)
			}
		})
	}
}
