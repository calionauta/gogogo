package config

import "testing"

// TestValidateDelivery pins the fail-fast contract: an impossible transport
// combination is refused at boot, naming the missing variable, instead of
// turning into a 500 the first time a user schedules an e-mail.
func TestValidateDelivery(t *testing.T) {
	cases := []struct {
		name      string
		transport string
		url       string
		key       string
		smtp      bool
		wantErr   bool
	}{
		{name: "default off", transport: "", wantErr: false},
		{name: "explicit off", transport: "off", wantErr: false},
		{name: "notify complete", transport: "notify", url: "http://notify:3000", key: "k", wantErr: false},
		{name: "notify without url", transport: "notify", key: "k", wantErr: true},
		{name: "notify without key", transport: "notify", url: "http://notify:3000", wantErr: true},
		{name: "smtp complete", transport: "smtp", smtp: true, wantErr: false},
		{name: "smtp without config", transport: "smtp", wantErr: true},
		{name: "unknown transport", transport: "carrier-pigeon", wantErr: true},
	}
	for _, tc := range cases {
		cfg := &Config{}
		cfg.Delivery.Transport = tc.transport
		cfg.Delivery.Notify.URL = tc.url
		cfg.Delivery.Notify.APIKey = tc.key
		if tc.smtp {
			cfg.Auth.SMTP.Addr = "smtp.example.com:587"
			cfg.Auth.SMTP.From = "noreply@example.com"
		}
		err := cfg.Validate()
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: Validate() error = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}
