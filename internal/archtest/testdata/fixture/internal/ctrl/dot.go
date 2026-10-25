package ctrl

import . "os"

func dotted() { _, _ = ReadFile("x") } // want R4
