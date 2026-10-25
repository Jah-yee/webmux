// Package attentionkind names the signals that a session needs the user.
// Sessions raise them and the push configuration selects among them.
package attentionkind

type Kind string

const (
	Question   Kind = "question"
	Permission Kind = "permission"
	Done       Kind = "done"
	Error      Kind = "error"
	Notify     Kind = "notify"
	Bell       Kind = "bell"
)

// Valid reports whether k is one of the named kinds.
func (k Kind) Valid() bool {
	switch k {
	case Question, Permission, Error, Done, Notify, Bell:
		return true
	}
	return false
}
