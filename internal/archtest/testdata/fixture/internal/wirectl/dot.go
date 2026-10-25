package wirectl

import (
	. "encoding/json"

	"example.com/fixture/internal/svc"
)

func dotted() { _, _ = Marshal(svc.Report{}) } // want R8
