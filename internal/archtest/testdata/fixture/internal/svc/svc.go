// Package svc is a service whose functions and errors the controller sees.
package svc

import (
	"errors"
	"fmt"
	"time"

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
	Status  int // want R7
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

type IntegerAlias = int
type DomainInteger int

// Only HTTP status names are restricted; domain error context is useful.
type DomainError struct {
	ExitCode IntegerAlias
	Size     int64
	Limit    uint
	Offset   DomainInteger
	Elapsed  time.Duration
	Code     int
}

func (*DomainError) Error() string   { return "domain failure" }
func (*DomainError) ExitStatus() int { return 1 }

type DomainState struct{}

func (DomainState) Status() int     { return 1 }
func (DomainState) HTTPStatus() int { return 1 } // want R7

type StatusFields struct {
	Status         IntegerAlias  // want R7
	status         DomainInteger // want R7
	StatusCode     int           // want R7
	statusCode     int           // want R7
	HTTPStatus     int           // want R7
	httpStatus     int           // want R7
	HTTPStatusCode int           // want R7
	httpStatusCode int           // want R7
}

func (StatusFields) Error() string { return "status" }

type StatusMethods string

func (s *StatusMethods) Error() string                       { return string(*s) }
func (*StatusMethods) Status() IntegerAlias                  { return 500 }      // want R7
func (*StatusMethods) status() DomainInteger                 { return 500 }      // want R7
func (*StatusMethods) StatusCode() int                       { return 500 }      // want R7
func (*StatusMethods) statusCode() int                       { return 500 }      // want R7
func (*StatusMethods) HTTPStatus() int                       { return 500 }      // want R7
func (*StatusMethods) httpStatus() int                       { return 500 }      // want R7
func (*StatusMethods) HTTPStatusCode() int                   { return 500 }      // want R7
func (*StatusMethods) httpStatusCode() (IntegerAlias, error) { return 500, nil } // want R7

// Pure is reviewed explicitly; a helpful function name alone is insufficient.
func Pure(string) error { return nil }
func UnusedPure()       {}
func NoReason()         {}

var NotAFunction = 0

func Save() { NotAFunction++ }

func StatusCode(error) IntegerAlias      { return 500 } // want R7
func HTTPStatus() int                    { return 500 } // want R7
func HTTPStatusCode() IntegerAlias       { return 500 } // want R7
func (DomainState) StatusCode(error) int { return 500 } // want R7
