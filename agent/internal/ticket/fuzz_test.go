package ticket

import (
	"testing"
	"time"
)

// A ticket that was never issued is never accepted, whatever it looks like.
func FuzzConsumeUnknownTickets(f *testing.F) {
	for _, s := range []string{"", "abc", "\x00", "日本語", string(make([]byte, 10000))} {
		f.Add(s, "ses_1")
	}
	store := NewStore(time.Minute)
	issued, _ := store.Issue("dev_1", "ses_1")
	f.Fuzz(func(t *testing.T, ticket, sessionID string) {
		if ticket == issued {
			t.Skip()
		}
		if device, res := store.Consume(ticket, sessionID); res == OK {
			t.Fatalf("accepted a ticket that was never issued (device %q)", device)
		}
	})
}
