//go:build !windows

package book

import "syscall"

var platformResets []syscall.Errno
