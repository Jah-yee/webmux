// Package shared may not depend on services or controllers.
package shared

import (
	_ "example.com/fixture/internal/hubx" // want R1
	_ "example.com/fixture/internal/svc"  // want R1
)
