//go:build !linux && !darwin && !windows

package installplatform

func processStartIdentitySupported() bool { return false }

func processStartToken(int) (string, bool) { return "", false }
