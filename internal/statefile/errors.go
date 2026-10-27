package statefile

import (
	"errors"
	"fmt"
	"os"
)

type Kind string

const (
	Missing            Kind = "missing"
	Locked             Kind = "locked"
	Corrupt            Kind = "corrupt"
	UnsupportedVersion Kind = "unsupported_version"
	TooLarge           Kind = "too_large"
	Permission         Kind = "permission"
	IO                 Kind = "io"
	CommitUncertain    Kind = "commit_uncertain"
	Closed             Kind = "closed"
	LegacyLayout       Kind = "legacy_layout"
)

type Phase string

const (
	NotCommitted Phase = "not_committed"
	Committed    Phase = "committed"
	Uncertain    Phase = "commit_uncertain"
)

// Error never prints underlying decoder values or external command output.
type Error struct {
	Store    ID
	Path, Op string
	Kind     Kind
	Phase    Phase
	cause    error
}

func (e *Error) Error() string {
	return fmt.Sprintf("state %s: %s %s (%s, %s)", e.Store, e.Op, e.Path, e.Kind, e.Phase)
}
func (e *Error) Unwrap() error         { return e.cause }
func IsKind(err error, kind Kind) bool { var e *Error; return errors.As(err, &e) && e.Kind == kind }
func fault(d Descriptor, path, op string, kind Kind, err error) *Error {
	if kind == IO && errors.Is(err, os.ErrPermission) {
		kind = Permission
	}
	phase := NotCommitted
	if kind == CommitUncertain {
		phase = Uncertain
	}
	return &Error{Store: d.ID, Path: path, Op: op, Kind: kind, Phase: phase, cause: err}
}

func ErrUnavailableStore() error { return fault(Descriptor{}, "", "access", Closed, nil) }
