package tunnel

import "syscall"

// childAttr has no darwin equivalent of Linux's parent-death signal; a hard
// kill of webmux relies on launchd tearing down the job's processes.
func childAttr() *syscall.SysProcAttr { return nil }
