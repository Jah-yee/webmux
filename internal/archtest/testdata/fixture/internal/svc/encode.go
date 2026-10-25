package svc

import (
	"encoding/json"
	enc "encoding/json"
)

// digest is a registered encoder: an internal checksum.
func digest(v any) []byte { b, _ := json.Marshal(v); return b }

// Encode is a registered method encoder.
func (*Service) Encode(v any) []byte { b, _ := json.Marshal(v); return b }

// frame encodes a client-visible payload itself.
func frame(r Report) []byte { b, _ := enc.Marshal(r); return b } // want R9

func stream(r Report) { _ = json.NewEncoder(nil).Encode(r) } // want R9

func (r Report) MarshalJSON() ([]byte, error) { return nil, nil } // want R9

// Decoding is not encoding.
func parse(b []byte) (r Report) { _ = json.Unmarshal(b, &r); return }

var initialJSON, initialErr = json.Marshal(Report{}) // want R9
var marshalAlias = json.Marshal                      // want R9
var encodeAlias = json.NewEncoder(nil).Encode        // want R9
var encodeMethod = (*json.Encoder).Encode            // want R9

func aliasCalls(r Report) {
	_, _ = marshalAlias(r)
	_ = encodeAlias(r)
	local := json.MarshalIndent // want R9
	_, _ = local(r, "", "")
}

// Registration permits direct calls, not aliases escaping its body.
func registeredAlias(v any) []byte {
	marshal := json.Marshal // want R9
	_, _ = marshal(v)
	b, _ := json.Marshal(v)
	return b
}
