package tunnel

import "syscall"

// dupFD duplicates a file descriptor so the original can be independently
// managed by Android while Go owns the duplicate.
func dupFD(fd int) (int, error) {
	return syscall.Dup(fd)
}
