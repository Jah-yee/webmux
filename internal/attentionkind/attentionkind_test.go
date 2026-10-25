package attentionkind

import "testing"

func TestValid(t *testing.T) {
	for _, k := range []Kind{Question, Permission, Done, Error, Notify, Bell} {
		if !k.Valid() {
			t.Errorf("%q is not valid", k)
		}
	}
	for _, k := range []Kind{"", "Question", "idle"} {
		if k.Valid() {
			t.Errorf("%q is valid", k)
		}
	}
}
