// Package svchttp is an unregistered service that touches net/http.
package svchttp

import "net/http"

func Serve(w http.ResponseWriter) {} // want R6

func Handle(r *http.Request) {} // want R6

func Fetch() (*http.Client, error) { return nil, nil } // want R6
