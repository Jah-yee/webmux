package contract

// APIPushStatus is the body of GET /api/push. (PushStatus is the local
// control status.)
type APIPushStatus struct {
	Enabled       bool   `json:"enabled"`
	Available     bool   `json:"available"`
	Subscriptions int    `json:"subscriptions"`
	Reason        string `json:"reason,omitempty"`
	PublicKey     string `json:"publicKey,omitempty"`
}

// PushConflict is a 409 body naming why push cannot be used here.
type PushConflict struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

// PushSubscriptionRequest is PushSubscription.toJSON(). ExpirationTime is
// accepted but not stored.
type PushSubscriptionRequest struct {
	Endpoint       string   `json:"endpoint"`
	ExpirationTime *float64 `json:"expirationTime"`
	Keys           struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// PushSubscriptionCreated is the body of a successful subscription.
type PushSubscriptionCreated struct {
	ID string `json:"id"`
}

// PushTestReport summarises one test notification sent to every device.
type PushTestReport struct {
	Devices  int      `json:"devices"`
	Accepted int      `json:"accepted"`
	Removed  []string `json:"removed"`
	Errors   []string `json:"errors"`
}
