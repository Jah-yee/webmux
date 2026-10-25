// Package contract holds the fixture's wire types.
package contract

import "example.com/fixture/internal/svc" // want R10

type Item struct{ Name string }

type Items []Item

// Leak nests a service type deep inside a wire type.
type Leak struct { // want R8
	Inner []map[string]*svc.Report
}
