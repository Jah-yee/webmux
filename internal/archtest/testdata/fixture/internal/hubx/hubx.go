// Package hubx is a controller that must not import the api package.
package hubx

import _ "example.com/fixture/internal/api" // want R2

const Name = "hub"
