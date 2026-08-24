//go:build linux

package installplatform

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func processStartIdentitySupported() bool { return true }

func processStartToken(pid int) (string, bool) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	// The command field is parenthesized and may itself contain spaces or ')'.
	// Fields after its final ')' start at Linux proc field 3; starttime is 22.
	closing := strings.LastIndexByte(string(contents), ')')
	if closing < 0 || closing+1 >= len(contents) {
		return "", false
	}
	fields := strings.Fields(string(contents[closing+1:]))
	if len(fields) <= 19 {
		return "", false
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", false
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(bootID)) == "" {
		return "", false
	}
	return processStartTokenDigest("linux\x00" + strings.TrimSpace(string(bootID)) + "\x00" + fields[19]), true
}
