package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// DeliveryConfig picks the transport for channels the app cannot deliver
// itself. The inbox is not one of them: in_app is written by the notifications
// feature, so a provider can never become a second owner of it.
//
// The transport is "off" by default. A clone never sends anything externally,
// and a deployment opts in explicitly — the same stance as GOAI_API_KEY and
// CREDITS_ENABLED.
type DeliveryConfig struct {
	// Transport is "off", "smtp" or "notify".
	Transport string
	// Notify is the external engine's endpoint. URL and APIKey are both
	// required when Transport is "notify"; Validate fails the boot otherwise.
	Notify struct {
		URL    string
		APIKey string
	}
}

// loadDeliveryEnv reads the delivery env. Split from Load for the file-size
// gate, matching loadAuthEnv.
func (cfg *Config) loadDeliveryEnv() {
	cfg.Delivery.Transport = strings.ToLower(getEnv("DELIVERY_TRANSPORT", "off"))
	cfg.Delivery.Notify.URL = getEnv("NOTIFY_URL", "")
	cfg.Delivery.Notify.APIKey = os.Getenv("NOTIFY_API_KEY")
}

// Validate fails the boot on a delivery configuration that cannot work. It is
// called once from run(), before anything is built: a missing endpoint is a
// deploy error, not a runtime 500 discovered by a user.
func (cfg *Config) Validate() error {
	switch cfg.Delivery.Transport {
	case "", "off":
		return nil
	case "smtp":
		if cfg.Auth.SMTP.Addr == "" || cfg.Auth.SMTP.From == "" {
			return errors.New("config: DELIVERY_TRANSPORT=smtp needs SMTP_ADDR and SMTP_FROM")
		}
	case "notify":
		if cfg.Delivery.Notify.URL == "" || cfg.Delivery.Notify.APIKey == "" {
			return errors.New("config: DELIVERY_TRANSPORT=notify needs NOTIFY_URL and NOTIFY_API_KEY")
		}
	default:
		return fmt.Errorf("config: DELIVERY_TRANSPORT=%q is not off|smtp|notify", cfg.Delivery.Transport)
	}
	return nil
}
