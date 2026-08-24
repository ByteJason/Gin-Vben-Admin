//go:build !unix && !windows

package installplatform

func acquireProcessLeaseGuard(string) (func(), error) {
	return nil, errProcessLeaseBusy
}
