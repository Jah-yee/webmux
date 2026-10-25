// Command app is the assembly root: it may import every layer.
package main

import (
	"example.com/fixture/internal/api"
	_ "example.com/fixture/internal/client"
	_ "example.com/fixture/internal/ctrl"
	_ "example.com/fixture/internal/svchttp"
	_ "example.com/fixture/internal/wirectl"
)

func main() { _ = api.New() }
