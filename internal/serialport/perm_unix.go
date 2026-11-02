//go:build !windows

package serialport

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// permissionHint tells how to get at the port at path, by the group that
// owns it, which users join to use ports (dialout on Debian and Ubuntu,
// uucp on Arch, dialer elsewhere).
func permissionHint(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	g, err := user.LookupGroupId(strconv.FormatUint(uint64(sys.Gid), 10))
	if err != nil || g.Name == "root" {
		return ""
	}
	return ": it is group " + g.Name + "'s. Join it with: sudo usermod -aG " + g.Name + " $USER, then log out and in again"
}
