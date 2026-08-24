//go:build darwin

package installplatform

import (
	"strconv"

	"golang.org/x/sys/unix"
)

func processStartIdentitySupported() bool { return true }

func processStartToken(pid int) (string, bool) {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || process == nil || int(process.Proc.P_pid) != pid {
		return "", false
	}
	started := process.Proc.P_starttime
	return processStartTokenDigest("darwin\x00" + strconv.FormatInt(started.Sec, 10) + "\x00" + strconv.FormatInt(int64(started.Usec), 10)), true
}
