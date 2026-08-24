package installplatform

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	legacyProcessLeaseSchema      = 1
	processLeaseSchema            = 2
	maxProcessLeaseBytes          = 4 << 10
	maximumProcessLease           = 30 * time.Second
	processLeaseReleaseAttempts   = 3
	processLeaseReleaseRetryDelay = 10 * time.Millisecond
)

var errProcessLeaseBusy = errors.New("installation process lease is busy")

type processLease struct {
	Schema     int       `json:"schema"`
	ID         string    `json:"id,omitempty"`
	PID        int       `json:"pid"`
	StartToken string    `json:"startToken,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type processLeaseReleaseOperations struct {
	acquireGuard func(string) (func(), error)
	lstat        func(string) (os.FileInfo, error)
	read         func(string, int64) ([]byte, bool, error)
	remove       func(string) error
	syncDir      func(string) error
	sleep        func(time.Duration)
}

func defaultProcessLeaseReleaseOperations() processLeaseReleaseOperations {
	return processLeaseReleaseOperations{
		acquireGuard: acquireProcessLeaseGuard,
		lstat:        os.Lstat,
		read:         readRegularFile,
		remove:       os.Remove,
		syncDir:      syncDirectory,
		sleep:        time.Sleep,
	}
}

func acquireProcessLease(path string) (func() error, error) {
	return acquireProcessLeaseWithGuard(path, filepath.Join(filepath.Dir(path), "process.guard"))
}

func acquireProcessLeaseWithGuard(path, guardPath string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(guardPath), 0o700); err != nil {
		return nil, err
	}
	guardRelease, err := acquireProcessLeaseGuard(guardPath)
	if err != nil {
		return nil, errProcessLeaseBusy
	}
	defer guardRelease()
	startToken, _ := processStartToken(os.Getpid())
	return acquireProcessLeaseLockedWithStartToken(path, guardPath, startToken)
}

// reconcileStaleProcessLeaseWithGuard removes only a syntactically valid lease
// whose recorded process is confirmed dead (or whose PID now has a different
// start identity). Unlike acquisition, reconciliation never publishes a lease.
func reconcileStaleProcessLeaseWithGuard(path, guardPath string) error {
	if err := os.MkdirAll(filepath.Dir(guardPath), 0o700); err != nil {
		return err
	}
	guardRelease, err := acquireProcessLeaseGuard(guardPath)
	if err != nil {
		return errProcessLeaseBusy
	}
	defer guardRelease()

	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return errProcessLeaseBusy
	}
	contents, exists, err := readRegularFile(path, maxProcessLeaseBytes)
	if err != nil || !exists {
		return errProcessLeaseBusy
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var lease processLease
	if decoder.Decode(&lease) != nil || ensureJournalJSONEnd(decoder) != nil || !validProcessLeaseForReconciliation(lease, time.Now().UTC()) {
		return errProcessLeaseBusy
	}
	if !processLivenessAvailable() || processAlive(lease.PID) {
		if lease.Schema != processLeaseSchema || lease.StartToken == "" || !processStartIdentitySupported() {
			return errProcessLeaseBusy
		}
		currentToken, available := processStartToken(lease.PID)
		if !available || currentToken == lease.StartToken {
			return errProcessLeaseBusy
		}
	}

	currentInfo, err := os.Lstat(path)
	if err != nil || !currentInfo.Mode().IsRegular() || !os.SameFile(info, currentInfo) {
		return errProcessLeaseBusy
	}
	currentContents, exists, err := readRegularFile(path, maxProcessLeaseBytes)
	if err != nil || !exists || !bytes.Equal(currentContents, contents) {
		return errProcessLeaseBusy
	}
	if err := os.Remove(path); err != nil {
		return errProcessLeaseBusy
	}
	_ = syncDirectory(filepath.Dir(path))
	return nil
}

func validProcessLeaseForReconciliation(lease processLease, now time.Time) bool {
	legacyReceipt := lease.Schema == legacyProcessLeaseSchema && lease.StartToken == ""
	currentReceipt := lease.Schema == processLeaseSchema &&
		(lease.ID == "" || validProcessLeaseID(lease.ID)) &&
		(lease.StartToken == "" || validProcessStartToken(lease.StartToken))
	return (legacyReceipt || currentReceipt) && lease.PID > 0 && !lease.CreatedAt.IsZero() && !lease.CreatedAt.After(now.Add(time.Second))
}

func acquireProcessLeaseLockedWithStartToken(path, guardPath, startToken string) (func() error, error) {
	if startToken != "" && !validProcessStartToken(startToken) {
		return nil, errProcessLeaseBusy
	}
	id, err := newProcessLeaseID()
	if err != nil {
		return nil, errProcessLeaseBusy
	}
	lease := processLease{Schema: processLeaseSchema, ID: id, PID: os.Getpid(), StartToken: startToken, CreatedAt: time.Now().UTC()}
	encoded, err := json.Marshal(lease)
	if err != nil {
		return nil, errProcessLeaseBusy
	}
	encoded = append(encoded, '\n')
	acquiredInfo, err := publishProcessLeaseWithIdentity(path, encoded)
	if errors.Is(err, os.ErrExist) {
		if reclaimProcessLeaseLocked(path, time.Now().UTC()) != nil {
			return nil, errProcessLeaseBusy
		}
		acquiredInfo, err = publishProcessLeaseWithIdentity(path, encoded)
		if errors.Is(err, os.ErrExist) {
			return nil, errProcessLeaseBusy
		}
	}
	if err != nil {
		return nil, err
	}
	return func() error { return removeOwnedProcessLease(path, guardPath, acquiredInfo, encoded) }, nil
}

func publishProcessLease(path string, encoded []byte) error {
	_, err := publishProcessLeaseWithIdentity(path, encoded)
	return err
}

func publishProcessLeaseWithIdentity(path string, encoded []byte) (os.FileInfo, error) {
	return publishProcessLeaseWithIdentityUsing(path, encoded, (*os.File).Stat)
}

func publishProcessLeaseWithIdentityUsing(path string, encoded []byte, statFile func(*os.File) (os.FileInfo, error)) (os.FileInfo, error) {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return nil, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if statFile == nil {
		_ = temporary.Close()
		return nil, errProcessLeaseBusy
	}
	// Capture the identity from the still-open handle. On Windows, FileInfo
	// returned by Lstat can resolve its file ID lazily from the pathname. The
	// temporary name is removed after the no-clobber hard link is published, so
	// retaining that path-backed FileInfo would make a later SameFile check fail
	// closed and silently preserve our own lease forever.
	acquiredInfo, err := statFile(temporary)
	if err != nil {
		_ = temporary.Close()
		return nil, err
	}
	if acquiredInfo == nil || !acquiredInfo.Mode().IsRegular() {
		_ = temporary.Close()
		return nil, errProcessLeaseBusy
	}
	if err := writeAndSync(temporary, encoded); err != nil {
		return nil, err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return nil, err
	}
	// Both links reference fully fsynced contents. Removing the sibling temp is
	// best effort; it is never treated as lease acquisition failure after the
	// no-clobber final link became visible.
	_ = os.Remove(temporaryPath)
	_ = syncDirectory(dir)
	return acquiredInfo, nil
}

func reclaimProcessLease(path string, now time.Time) error {
	guardRelease, err := acquireProcessLeaseGuard(filepath.Join(filepath.Dir(path), "process.guard"))
	if err != nil {
		return errProcessLeaseBusy
	}
	defer guardRelease()
	return reclaimProcessLeaseLocked(path, now)
}

func reclaimProcessLeaseLocked(path string, now time.Time) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errProcessLeaseBusy
	}
	contents, exists, err := readRegularFile(path, maxProcessLeaseBytes)
	if err != nil || !exists {
		return errProcessLeaseBusy
	}
	if len(contents) == 0 {
		if now.Sub(info.ModTime()) <= maximumProcessLease {
			return errProcessLeaseBusy
		}
	} else {
		decoder := json.NewDecoder(bytes.NewReader(contents))
		decoder.DisallowUnknownFields()
		var lease processLease
		decodeInvalid := decoder.Decode(&lease) != nil || ensureJournalJSONEnd(decoder) != nil
		legacyReceipt := lease.Schema == legacyProcessLeaseSchema && lease.StartToken == ""
		currentReceipt := lease.Schema == processLeaseSchema &&
			(lease.ID == "" || validProcessLeaseID(lease.ID)) &&
			(lease.StartToken == "" || validProcessStartToken(lease.StartToken))
		invalid := decodeInvalid || (!legacyReceipt && !currentReceipt) || lease.PID <= 0 || lease.CreatedAt.IsZero() || lease.CreatedAt.After(now.Add(time.Second))
		if invalid {
			// A partial writer can briefly expose legacy or externally-created
			// invalid contents. Keep a recent receipt busy, but let an old regular
			// file recover rather than blocking initialization forever.
			if now.Sub(info.ModTime()) <= maximumProcessLease {
				return errProcessLeaseBusy
			}
		} else if processLivenessAvailable() {
			alive := processAlive(lease.PID)
			if currentReceipt {
				currentToken, available := processStartToken(lease.PID)
				// An empty token is a deliberate fallback receipt created when the
				// writer could not inspect even its own start identity. It therefore
				// cannot be treated as a match if a later probe succeeds.
				available = available && lease.StartToken != ""
				if !shouldReclaimCurrentProcessLease(
					alive,
					processStartIdentitySupported(),
					available,
					available && currentToken == lease.StartToken,
					now.Sub(info.ModTime()),
				) {
					return errProcessLeaseBusy
				}
			} else if alive && legacyReceipt {
				// Schema 1 had no process-start identity. A parsed receipt whose PID
				// is still live must fail closed: age alone must never let a second
				// installer overlap a long-running owner.
				return errProcessLeaseBusy
			}
		} else if !shouldReclaimValidProcessLease(false, false) {
			// Unknown platforms fail closed for a syntactically valid receipt.
			// The TTL is reserved for empty/invalid legacy files, where there is no
			// trustworthy owner identity to protect.
			return errProcessLeaseBusy
		}
	}
	if err := os.Remove(path); err != nil {
		return errProcessLeaseBusy
	}
	_ = syncDirectory(filepath.Dir(path))
	return nil
}

func shouldReclaimValidProcessLease(livenessAvailable, processIsAlive bool) bool {
	return livenessAvailable && !processIsAlive
}

func shouldReclaimCurrentProcessLease(alive, identitySupported, startTokenAvailable, startTokenMatches bool, _ time.Duration) bool {
	if !alive {
		return true
	}
	if !identitySupported {
		return false
	}
	if startTokenAvailable {
		return !startTokenMatches
	}
	// The PID is confirmed live but its start identity is temporarily unknown
	// (or was unavailable when this receipt was written). Preserve exclusivity;
	// TTL recovery is limited to invalid/truncated receipts above.
	return false
}

func processStartTokenDigest(material string) string {
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:])
}

func validProcessStartToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func newProcessLeaseID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func validProcessLeaseID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func removeOwnedProcessLease(path, guardPath string, acquired os.FileInfo, expected []byte) error {
	return removeOwnedProcessLeaseWithOperations(path, guardPath, acquired, expected, defaultProcessLeaseReleaseOperations())
}

func removeOwnedProcessLeaseWithOperations(path, guardPath string, acquired os.FileInfo, expected []byte, operations processLeaseReleaseOperations) error {
	var lastErr error
	for attempt := 0; attempt < processLeaseReleaseAttempts; attempt++ {
		lastErr = removeOwnedProcessLeaseOnce(path, guardPath, acquired, expected, operations)
		if lastErr == nil {
			return nil
		}
		if attempt+1 < processLeaseReleaseAttempts {
			operations.sleep(processLeaseReleaseRetryDelay)
		}
	}
	return fmt.Errorf("release owned installation process lease after %d attempts: %w", processLeaseReleaseAttempts, lastErr)
}

func removeOwnedProcessLeaseOnce(path, guardPath string, acquired os.FileInfo, expected []byte, operations processLeaseReleaseOperations) error {
	guardRelease, err := operations.acquireGuard(guardPath)
	if err != nil {
		return err
	}
	defer guardRelease()
	currentInfo, err := operations.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if acquired == nil || !currentInfo.Mode().IsRegular() || !os.SameFile(acquired, currentInfo) {
		return nil
	}
	contents, exists, err := operations.read(path, maxProcessLeaseBytes)
	if err != nil {
		return err
	}
	if !exists || !bytes.Equal(contents, expected) {
		return nil
	}
	if err := operations.remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	_ = operations.syncDir(filepath.Dir(path))
	return nil
}
