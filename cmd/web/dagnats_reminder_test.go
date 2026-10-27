// SCOPE:core — dagnats worker payload and port wiring.
package main

import (
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/delivery"
)

// TestReminderDeliveryReadsTheSeed pins the worker payload contract: the
// reminder workflow forwards the run input through the seed step, and this
// mapping is the only place that shape is decoded. A renamed JSON key would
// otherwise deliver an empty notification silently.
func TestReminderDeliveryReadsTheSeed(t *testing.T) {
	raw := []byte(`{"seed":{
		"user":"u1",
		"transaction_id":"txn-1",
		"body":"Drink water",
		"channels":["email","sms"],
		"recipients":{"email":"ada@example.com"}
	}}`)
	d, err := reminderDelivery(raw)
	if err != nil {
		t.Fatalf("reminderDelivery: %v", err)
	}
	if d.Owner != "u1" || d.TransactionID != "txn-1" || d.Body != "Drink water" {
		t.Fatalf("delivery = %+v, want owner/transaction/body from the seed", d)
	}
	if len(d.Channels) != 2 || d.Channels[0] != delivery.ChannelEmail || d.Channels[1] != delivery.Channel("sms") {
		t.Fatalf("channels = %v, want [email sms]", d.Channels)
	}
	if d.Recipients[delivery.ChannelEmail] != "ada@example.com" {
		t.Fatalf("recipients = %v, want the email address", d.Recipients)
	}
}

// TestReminderDeliveryRefusesMissingSeed pins the fail-fast half: a payload
// without the seed is an error, not an empty delivery.
func TestReminderDeliveryRefusesMissingSeed(t *testing.T) {
	if _, err := reminderDelivery([]byte(`{"other":true}`)); err == nil {
		t.Fatal("reminderDelivery accepted a payload with no seed")
	}
}

// TestNatsPortUsesConfig pins the fix that made DAGNATS_NATS_PORT meaningful:
// the engine and the broadcaster must read the same value. A zero value falls
// back to the conventional port.
func TestNatsPortUsesConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.DagNats.NATSPort = 14222
	if got := natsPort(cfg); got != 14222 {
		t.Fatalf("natsPort = %d, want the configured 14222", got)
	}
	if got := natsPort(&config.Config{}); got != dagnatsNATSPort {
		t.Fatalf("natsPort(zero) = %d, want the fallback %d", got, dagnatsNATSPort)
	}
}
