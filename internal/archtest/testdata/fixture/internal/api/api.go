// Package api is the HTTP controller only cmd/ may import.
package api

import "net/http"

func New() http.Handler { return http.NotFoundHandler() }
