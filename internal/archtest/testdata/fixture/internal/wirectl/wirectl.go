// Package wirectl is a controller in the wire scope.
package wirectl

import (
	"encoding/json"
	j "encoding/json"

	"example.com/fixture/internal/contract"
	"example.com/fixture/internal/svc"
)

// reply and serve are registered wrappers; their callers are checked.
func reply(v any) { _, _ = json.Marshal(v) }

func serve[T any](fn func() T) { reply(fn()) }

// send is an unregistered wrapper, which would hide its callers' types.
func send(v any) { _, _ = json.Marshal(v) } // want R8

func handlers() {
	reply(contract.Item{})
	reply([]contract.Item{})
	reply(map[string]*contract.Item{})
	reply(contract.Items(nil))
	reply(svc.Report{})            // want R8
	reply(map[string]string{})     // want R8
	reply([]string{})              // want R8
	_, _ = j.Marshal(svc.Report{}) // want R8
	var item contract.Item
	_ = json.Unmarshal(nil, &item)
	var bad svc.Report
	_ = json.Unmarshal(nil, &bad)         // want R8
	_ = json.NewDecoder(nil).Decode(&bad) // want R8
	_ = json.NewEncoder(nil).Encode(item)
	serve(func() contract.Item { return contract.Item{} })
	serve(func() svc.Report { return svc.Report{} }) // want R8
	marshal := json.Marshal                          // want R8
	_, _ = marshal(item)
	send(item)
}
