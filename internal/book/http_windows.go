package book

import "syscall"

// Windows reports resets as WSAECONNRESET or ERROR_NETNAME_DELETED, which
// syscall.ECONNRESET does not match there.
var platformResets = []syscall.Errno{10054, 64}
