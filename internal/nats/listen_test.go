package nats

import "testing"

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
