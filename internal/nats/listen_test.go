package nats

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestListenOptionsGuard(t *testing.T) {
	tests := []struct {
		name    string
		listen  string
		token   string
		wantErr bool
	}{
		{name: "default random loopback"},
		{name: "loopback fixed", listen: "127.0.0.1:4222"},
		{name: "public without token refused", listen: "0.0.0.0:4222", wantErr: true},
		{name: "public with token", listen: "0.0.0.0:4222", token: "t0ken"},
		{name: "bad listen", listen: "not-a-host-port", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := listenOptions(tc.listen, tc.token)
			if tc.wantErr {
				if err == nil {
					t.Fatal("guard accepted the listener")
				}
				return
			}
			if err != nil {
				t.Fatalf("listenOptions: %v", err)
			}
			if tc.token != "" && opts.Authorization != tc.token {
				t.Fatalf("token not wired: %+v", opts.Authorization)
			}
		})
	}
}

// TestStartEmbeddedOnWithToken pins the process's own client against a
// token-protected listener. StartEmbeddedOn used to dial its own server
// without credentials, so every boot with NATS_TOKEN fell back to the
// in-memory broadcaster with "Authorization Violation". Red-proof: drop the
// natsio.Token option from the dial and this fails.
func TestStartEmbeddedOnWithToken(t *testing.T) {
	t.Parallel()
	// A real ephemeral port: nats-server treats Port 0 as "no listener",
	// and StartEmbeddedOn's own guard wants an explicit host:port anyway.
	ln, lerr := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if lerr != nil {
		t.Fatalf("reserve port: %v", lerr)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	h, err := StartEmbeddedOn(t.TempDir(), addr, "smoke-token")
	if err != nil {
		t.Fatalf("StartEmbeddedOn: %v", err)
	}
	t.Cleanup(h.Close)
	if h.Conn == nil || !h.Conn.IsConnected() {
		t.Fatal("own client is not connected to the token-protected server")
	}
	if _, err := h.JS.AccountInfo(); err != nil {
		t.Fatalf("JetStream unreachable through the authenticated client: %v", err)
	}
}

func TestLeafOptionsGuard(t *testing.T) {
	t.Parallel()
	if opts, err := leafOptions("", ""); err != nil || opts.Port != 0 {
		t.Fatalf("empty leaf listen = %+v, %v; want disabled", opts, err)
	}
	if _, err := leafOptions("not-a-host-port", "t"); err == nil {
		t.Fatal("accepted a malformed leaf listener")
	}
	if _, err := leafOptions("0.0.0.0:7422", ""); err == nil {
		t.Fatal("exposed a leaf listener without a token")
	}
	opts, err := leafOptions("0.0.0.0:7422", "t0ken")
	if err != nil {
		t.Fatalf("leafOptions: %v", err)
	}
	if opts.Username != "t0ken" {
		t.Fatalf("leaf credential = %q, want the token", opts.Username)
	}
}

// TestLeafNodeForwardsThroughHubListener is the sync transport proof: a hub
// started with StartEmbeddedWithLeaf accepts a leaf node, and a message
// published on the leaf reaches a subscriber on the hub. Without the leaf
// listener the leaf never attaches (NumLeafNodes=0) and the CRUD wire stays
// local — the gap the desktop sync smoke exposed.
func TestLeafNodeForwardsThroughHubListener(t *testing.T) {
	clientAddr := reserveAddr(t)
	leafAddr := reserveAddr(t)
	hub, err := StartEmbeddedWithLeaf(t.TempDir(), clientAddr, "tok", leafAddr)
	if err != nil {
		t.Fatalf("StartEmbeddedWithLeaf: %v", err)
	}
	t.Cleanup(hub.Close)

	_, leafPort, _ := net.SplitHostPort(leafAddr)
	leaf, err := StartLeafNode(t.TempDir(), "nats://tok@127.0.0.1:"+leafPort)
	if err != nil {
		t.Fatalf("StartLeafNode: %v", err)
	}
	t.Cleanup(leaf.Close)

	deadline := time.Now().Add(10 * time.Second)
	for hub.Server.NumLeafNodes() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("leaf never attached to the hub's leaf listener")
		}
		time.Sleep(50 * time.Millisecond)
	}

	sub, err := hub.Conn.SubscribeSync("sync.test")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if ferr := hub.Conn.Flush(); ferr != nil {
		t.Fatalf("flush: %v", ferr)
	}
	// Subscription interest crosses the leaf asynchronously; publishing
	// before it lands is a no-op, not a transport failure.
	time.Sleep(500 * time.Millisecond)
	if perr := leaf.Conn.Publish("sync.test", []byte("hello")); perr != nil {
		t.Fatalf("publish: %v", perr)
	}
	msg, err := sub.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("message did not cross the leaf: %v", err)
	}
	if string(msg.Data) != "hello" {
		t.Fatalf("payload = %q", msg.Data)
	}
}

func reserveAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}
