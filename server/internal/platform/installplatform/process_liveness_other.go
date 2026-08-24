//go:build !unix && !windows

package installplatform

// Unknown platforms fail closed for valid process receipts. Invalid or empty
// legacy receipts still use the bounded age policy in process_lease.go.
func processAlive(int) bool { return true }

func processLivenessAvailable() bool { return false }
