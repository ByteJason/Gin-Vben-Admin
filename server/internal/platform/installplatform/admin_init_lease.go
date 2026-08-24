package installplatform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const adminInitLeaseOwner = "admin-init"

const adminInitReclaimGrace = 60 * time.Second

type adminInitLease struct {
	Schema        int    `json:"schema"`
	Owner         string `json:"owner"`
	ID            string `json:"id"`
	PID           int    `json:"pid"`
	PIDStartToken string `json:"pidStartToken,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

type adminInitLeaseOperations struct {
	lstat             func(string) (os.FileInfo, error)
	read              func(string, int64) ([]byte, bool, error)
	link              func(string, string) error
	remove            func(string) error
	syncDir           func(string) error
	processAlive      func(int) bool
	livenessAvailable func() bool
	changeTime        func(string) (time.Time, bool)
	now               func() time.Time
	reclaimGrace      time.Duration
}

func defaultAdminInitLeaseOperations() adminInitLeaseOperations {
	return adminInitLeaseOperations{
		lstat:             os.Lstat,
		read:              readRegularFile,
		link:              os.Link,
		remove:            os.Remove,
		syncDir:           syncDirectory,
		processAlive:      processAlive,
		livenessAvailable: processLivenessAvailable,
		changeTime:        adminInitReclaimChangeTime,
		now:               func() time.Time { return time.Now().UTC() },
		reclaimGrace:      adminInitReclaimGrace,
	}
}

// reconcileStaleAdminInitLease removes only the exact regular lease left by a
// confirmed exited Node initializer. Active, malformed and uninspectable
// owners remain busy. This lets the web installer recover when
// pnpm init completed its workspace transaction but Windows could not remove
// the final coordination receipt.
func reconcileStaleAdminInitLease(path string) error {
	return reconcileStaleAdminInitLeaseWithOperations(path, defaultAdminInitLeaseOperations())
}

func reconcileStaleAdminInitLeaseWithOperations(path string, operations adminInitLeaseOperations) error {
	if err := recoverInterruptedAdminInitReclaim(path, operations); err != nil {
		return err
	}
	info, err := operations.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect admin init lease: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errProcessLeaseBusy
	}
	contents, exists, err := operations.read(path, maxProcessLeaseBytes)
	if err != nil {
		return fmt.Errorf("read admin init lease: %w", err)
	}
	if !exists {
		return errProcessLeaseBusy
	}
	lease, valid := decodeAdminInitLease(contents, operations.now())
	if !valid {
		return errProcessLeaseBusy
	}
	// Node and Go intentionally use different process-start token sources on
	// some platforms (notably Windows FILETIME vs DateTime ticks and Darwin ps
	// vs sysctl). Therefore a live PID is always preserved. The token remains a
	// strict receipt-integrity field, not a cross-runtime eviction signal.
	if !operations.livenessAvailable() || operations.processAlive(lease.PID) {
		return errProcessLeaseBusy
	}

	tombstonePath := path + ".reclaim"
	if _, tombstoneErr := operations.lstat(tombstonePath); tombstoneErr == nil {
		return errProcessLeaseBusy
	} else if !errors.Is(tombstoneErr, os.ErrNotExist) {
		return fmt.Errorf("inspect admin init reclaim tombstone: %w", tombstoneErr)
	}
	if err := operations.link(path, tombstonePath); err != nil {
		if errors.Is(err, os.ErrExist) || errors.Is(err, os.ErrNotExist) {
			return errProcessLeaseBusy
		}
		return fmt.Errorf("claim stale admin init lease: %w", err)
	}
	_ = operations.syncDir(filepath.Dir(path))

	tombstoneInfo, tombstoneContents, tombstoneMatches := matchingAdminInitLeaseSnapshot(tombstonePath, info, contents, operations)
	_, _, canonicalMatches := matchingAdminInitLeaseSnapshot(path, info, contents, operations)
	if !tombstoneMatches || !canonicalMatches {
		removeClaimedAdminInitTombstone(tombstonePath, tombstoneInfo, tombstoneContents, operations)
		return errProcessLeaseBusy
	}
	// admin-init.lock.reclaim is the same no-clobber hard-link tombstone used by
	// Node. While it exists, another initializer will not replace the canonical
	// path, closing the check/remove race that a plain Lstat+Remove would leave.
	if err := operations.remove(path); err != nil {
		removeClaimedAdminInitTombstone(tombstonePath, tombstoneInfo, tombstoneContents, operations)
		if errors.Is(err, os.ErrNotExist) {
			return errProcessLeaseBusy
		}
		return fmt.Errorf("remove stale admin init lease: %w", err)
	}
	_ = operations.syncDir(filepath.Dir(path))
	if !removeClaimedAdminInitTombstone(tombstonePath, tombstoneInfo, tombstoneContents, operations) {
		return errors.New("remove stale admin init reclaim tombstone")
	}
	return nil
}

func recoverInterruptedAdminInitReclaim(path string, operations adminInitLeaseOperations) error {
	tombstonePath := path + ".reclaim"
	tombstoneInfo, err := operations.lstat(tombstonePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect interrupted admin init reclaim: %w", err)
	}
	if !tombstoneInfo.Mode().IsRegular() {
		return errProcessLeaseBusy
	}
	changedAt, available := operations.changeTime(tombstonePath)
	now := operations.now()
	if !available || operations.reclaimGrace <= 0 || changedAt.After(now) || now.Sub(changedAt) <= operations.reclaimGrace {
		return errProcessLeaseBusy
	}
	tombstoneContents, exists, err := operations.read(tombstonePath, maxProcessLeaseBytes)
	if err != nil {
		return fmt.Errorf("read interrupted admin init reclaim: %w", err)
	}
	if !exists {
		return errProcessLeaseBusy
	}
	tombstoneLease, valid := decodeAdminInitLease(tombstoneContents, now)
	if !valid || !operations.livenessAvailable() || operations.processAlive(tombstoneLease.PID) {
		return errProcessLeaseBusy
	}

	canonicalInfo, canonicalErr := operations.lstat(path)
	if canonicalErr == nil {
		if !canonicalInfo.Mode().IsRegular() {
			return errProcessLeaseBusy
		}
		canonicalContents, canonicalExists, readErr := operations.read(path, maxProcessLeaseBytes)
		if readErr != nil {
			return fmt.Errorf("read canonical admin init lease during reclaim recovery: %w", readErr)
		}
		canonicalLease, canonicalValid := decodeAdminInitLease(canonicalContents, now)
		if !canonicalExists || !canonicalValid || !operations.livenessAvailable() || operations.processAlive(canonicalLease.PID) {
			return errProcessLeaseBusy
		}
		if _, _, matches := matchingAdminInitLeaseSnapshot(tombstonePath, tombstoneInfo, tombstoneContents, operations); !matches {
			return errProcessLeaseBusy
		}
		if _, _, matches := matchingAdminInitLeaseSnapshot(path, canonicalInfo, canonicalContents, operations); !matches {
			return errProcessLeaseBusy
		}
		if err := operations.remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errProcessLeaseBusy
			}
			return fmt.Errorf("remove interrupted canonical admin init lease: %w", err)
		}
		_ = operations.syncDir(filepath.Dir(path))
	} else if !errors.Is(canonicalErr, os.ErrNotExist) {
		return fmt.Errorf("inspect canonical admin init lease during reclaim recovery: %w", canonicalErr)
	}

	if _, _, matches := matchingAdminInitLeaseSnapshot(tombstonePath, tombstoneInfo, tombstoneContents, operations); !matches {
		return errProcessLeaseBusy
	}
	if err := operations.link(tombstonePath, path); err != nil {
		if errors.Is(err, os.ErrExist) || errors.Is(err, os.ErrNotExist) {
			return errProcessLeaseBusy
		}
		return fmt.Errorf("restore interrupted admin init lease: %w", err)
	}
	_ = operations.syncDir(filepath.Dir(path))
	if _, _, matches := matchingAdminInitLeaseSnapshot(path, tombstoneInfo, tombstoneContents, operations); !matches {
		return errProcessLeaseBusy
	}
	if !removeClaimedAdminInitTombstone(tombstonePath, tombstoneInfo, tombstoneContents, operations) {
		return errors.New("complete interrupted admin init reclaim recovery")
	}
	return nil
}

func matchingAdminInitLeaseSnapshot(path string, acquired os.FileInfo, expected []byte, operations adminInitLeaseOperations) (os.FileInfo, []byte, bool) {
	currentInfo, err := operations.lstat(path)
	if err != nil || !currentInfo.Mode().IsRegular() {
		return currentInfo, nil, false
	}
	currentContents, exists, err := operations.read(path, maxProcessLeaseBytes)
	if err != nil || !exists {
		return currentInfo, currentContents, false
	}
	return currentInfo, currentContents, acquired != nil && os.SameFile(acquired, currentInfo) && bytes.Equal(currentContents, expected)
}

func removeClaimedAdminInitTombstone(path string, acquired os.FileInfo, expected []byte, operations adminInitLeaseOperations) bool {
	_, _, matches := matchingAdminInitLeaseSnapshot(path, acquired, expected, operations)
	if !matches {
		return false
	}
	if err := operations.remove(path); err != nil {
		return false
	}
	_ = operations.syncDir(filepath.Dir(path))
	return true
}

func validAdminInitLease(lease adminInitLease, now time.Time) bool {
	legacy := lease.Schema == legacyProcessLeaseSchema && lease.PIDStartToken == ""
	current := lease.Schema == processLeaseSchema && validProcessStartToken(lease.PIDStartToken)
	const canonicalTimestamp = "2006-01-02T15:04:05.000Z"
	createdAt, err := time.Parse(canonicalTimestamp, lease.CreatedAt)
	canonicalCreatedAt := err == nil && createdAt.Format(canonicalTimestamp) == lease.CreatedAt
	return (legacy || current) &&
		lease.Owner == adminInitLeaseOwner &&
		validAdminInitLeaseID(lease.ID) &&
		lease.PID > 0 &&
		canonicalCreatedAt &&
		!createdAt.After(now.Add(time.Second))
}

func decodeAdminInitLease(contents []byte, now time.Time) (adminInitLease, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(contents, &fields) != nil {
		return adminInitLease{}, false
	}
	var schema int
	if json.Unmarshal(fields["schema"], &schema) != nil || !validAdminInitLeaseFields(schema, fields) {
		return adminInitLease{}, false
	}
	var lease adminInitLease
	if json.Unmarshal(contents, &lease) != nil || !validAdminInitLease(lease, now) {
		return adminInitLease{}, false
	}
	return lease, true
}

func validAdminInitLeaseFields(schema int, fields map[string]json.RawMessage) bool {
	expected := map[string]bool{
		"schema": true, "owner": true, "id": true, "pid": true, "createdAt": true,
	}
	if schema == processLeaseSchema {
		expected["pidStartToken"] = true
	} else if schema != legacyProcessLeaseSchema {
		return false
	}
	if len(fields) != len(expected) {
		return false
	}
	for name := range fields {
		if !expected[name] {
			return false
		}
	}
	return true
}

func validAdminInitLeaseID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range strings.ToLower(value) {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return value == strings.ToLower(value)
}
