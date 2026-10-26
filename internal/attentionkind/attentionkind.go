// Package attentionkind names the signals that a session needs the user.
// Sessions raise them and the push configuration selects among them.
package attentionkind

import "github.com/rainchan/webmux/internal/contract"

type Kind = contract.AttentionKind

const (
	Question   = contract.AttentionKindQuestion
	Permission = contract.AttentionKindPermission
	Done       = contract.AttentionKindDone
	Error      = contract.AttentionKindError
	Notify     = contract.AttentionKindNotify
	Bell       = contract.AttentionKindBell
)
