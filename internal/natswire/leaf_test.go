package natswire

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/nats"
)

func reserve(t *testing.T) string {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Addr().String()
	l.Close()
	return a
}

func TestLeafForwardsCrudStream(t *testing.T) {
	clientAddr, leafAddr := reserve(t), reserve(t)
	hub, err := nats.StartEmbeddedWithLeaf(t.TempDir(), clientAddr, "tok", leafAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)
	_, lp, _ := net.SplitHostPort(leafAddr)
	leaf, err := nats.StartLeafNode(t.TempDir(), "nats://tok@127.0.0.1:"+lp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(leaf.Close)
	deadline := time.Now().Add(10 * time.Second)
	for hub.Server.NumLeafNodes() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("no leaf")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Streams on both sides, as the product creates them.
	hp := NewCrudPublisher(hub.JS, "hub")
	if hp == nil {
		t.Fatal("hub publisher nil")
	}
	lp2 := NewCrudPublisher(leaf.JS, "edge")
	if lp2 == nil {
		t.Fatal("leaf publisher nil")
	}
	lp2.SetCore(leaf.Conn)

	sub, err := hub.Conn.SubscribeSync("app.crud.todo.create")
	if err != nil {
		t.Fatal(err)
	}
	hub.Conn.Flush()
	time.Sleep(1 * time.Second)
	lp2.Publish(CrudOpCreate, "u", &CrudOpData{ID: "x", Title: "t"})
	if _, err := sub.NextMsg(5 * time.Second); err != nil {
		t.Fatalf("hub core subscriber: %v", err)
	}
}
