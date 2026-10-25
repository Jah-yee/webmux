// Package svc is a service whose functions and errors the controller sees.
package svc

import (
	"errors"
	"fmt"

	"example.com/fixture/cmd/tool"      // want R1
	"example.com/fixture/internal/hubx" // want R1
	"github.com/coder/websocket"        // want R1
	"net/http/httptest"                 // want R1
)

var ErrNotFound = errors.New("not found")

type Service struct{}

func New() *Service                    { return &Service{} }
func Validate(string) error            { return nil }
func (*Service) Do(string) error       { return nil }
func (*Service) Status() Report        { return Report{} }
func Generic[T any](v T) T             { return v }
func use() (*websocket.Conn, any, any) { return nil, httptest.NewRecorder, tool.Name }

var _ = hubx.Name

// Report is a plain result; a Status method returning it is fine.
type Report struct{ Devices int }

// Problem carries an HTTP status, which only the controller may choose.
type Problem struct {
	Code    int // want R7
	Message string
}

func (p *Problem) Error() string { return p.Message }

// Status maps errors to integers.
func Status(error) int { return 500 } // want R7

// Kind is a compliant error: it wraps a sentinel kind.
type Kind struct {
	kind error
	msg  string
}

func (k Kind) Error() string { return fmt.Sprint(k.msg, k.kind) }

// Coded is an error value type whose method reports a status.
type Coded string

func (c Coded) Error() string { return string(c) }
func (c Coded) Status() uint8 { return 0 } // want R7
