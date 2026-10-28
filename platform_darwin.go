package main

import (
	"os/exec"
	"strconv"
)

// openWith opens a link, a file in its editor (edit) or a folder, as the
// desktop does.
func openWith(target string, edit bool) *exec.Cmd {
	if edit {
		return exec.Command("open", "-t", target)
	}
	return exec.Command("open", "--", target)
}

// awakeCmd keeps the machine from sleeping until it is stopped (or
// Quarters, pid, ends).
func awakeCmd(pid int) *exec.Cmd {
	// -i: no idle sleep, -s: no sleep at all on mains power; -w: ends with Quarters
	return exec.Command("/usr/bin/caffeinate", "-i", "-s", "-w", strconv.Itoa(pid))
}
