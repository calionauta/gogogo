// SCOPE:plugin — delivery seam contract.
package delivery

import (
	"context"
	"errors"
	"testing"
)

// TestExternalRefusesInApp pins the invariant the whole design rests on: the
// inbox is not a transport. A provider that could carry in_app would become a
// second owner of the inbox, so the predicate that keeps it out of the loop is
// tested directly — the feature's integration test cannot see the mutation
// because the unique index turns the duplicate write into a no-op.
func TestExternalRefusesInApp(t *testing.T) {
	if External(ChannelInApp) {
		t.Fatal("External(in_app) = true, want false: in_app is written by the feature, never by a provider")
	}
	// Any channel value is legal data; the template only names the two it creates.
	for _, ch := range []Channel{ChannelEmail, Channel("sms"), Channel("chat"), Channel("push")} {
		if !External(ch) {
			t.Errorf("External(%q) = false, want true", ch)
		}
	}
}

// TestFuncReportsReceipts pins the adapter half: a nil error is sent, a
// returned error is error with the cause kept (a delivery that failed must not
// look delivered).
func TestFuncReportsReceipts(t *testing.T) {
	ok := NewFunc("fake", func(_ context.Context, _ Message) error { return nil })
	receipt, err := ok.Send(context.Background(), Message{To: "a@b.c"})
	if err != nil || receipt.Status != StatusSent || receipt.Provider != "fake" {
		t.Fatalf("sent receipt = (%+v, %v), want sent/fake", receipt, err)
	}

	boom := errors.New("boom")
	bad := NewFunc("fake", func(_ context.Context, _ Message) error { return boom })
	receipt, err = bad.Send(context.Background(), Message{To: "a@b.c"})
	if !errors.Is(err, boom) || receipt.Status != StatusError || !errors.Is(receipt.Err, boom) {
		t.Fatalf("error receipt = (%+v, %v), want error carrying boom", receipt, err)
	}
}
