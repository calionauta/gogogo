// SCOPE:plugin — delivery transport: notify (an external delivery engine).
//
// notify carries the channel, not the inbox: the app writes its own in_app
// rows, and this adapter only triggers an e-mail workflow. The engine's
// contract is `POST /v1/events/trigger` with an API key, a subscriber id and a
// transaction id — the same transaction id the app uses, so a retried send is
// deduped on both sides.
//
// The workflow this adapter targets must exist in notify with the trigger
// identifier below and one `email` step rendering `{{payload.subject}}` and
// `{{payload.body}}`:
//
//	name: "gogogo-delivery"
//
// docs/notifications.md records that setup.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/calionauta/gogogo/internal/delivery"
)

// TriggerName is the workflow's triggerIdentifier in notify. It is a constant
// rather than a setting: a configurable name would fail at the first send
// instead of at review time.
const TriggerName = "gogogo-delivery"

// errBodyLimit caps the provider error text kept in a row.
const errBodyLimit = 300

// Client is the notify transport.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New builds the adapter. A missing URL or key is a wiring error: config
// validation already refused that boot, so panicking keeps the failure at the
// composition root instead of inside a delivery.
func New(baseURL, apiKey string) *Client {
	if baseURL == "" || apiKey == "" {
		panic("notify: New with empty URL or API key")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Name returns the provider name recorded on the delivery row.
func (c *Client) Name() string { return "notify" }

// triggerRequest is the documented /v1/events/trigger body.
type triggerRequest struct {
	Name string `json:"name"`
	To   struct {
		SubscriberID string `json:"subscriberId"`
		Email        string `json:"email,omitempty"`
	} `json:"to"`
	Payload struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	} `json:"payload"`
	TransactionID string `json:"transactionId,omitempty"`
}

// Send triggers the e-mail workflow. A 2xx means the run was accepted, not that
// the mail arrived — notify delivers asynchronously, which is why the app's row
// says `sent` for acceptance and leaves the provider's own delivery records as
// the deeper evidence.
func (c *Client) Send(ctx context.Context, m delivery.Message) (delivery.Receipt, error) {
	var body triggerRequest
	body.Name = TriggerName
	body.To.SubscriberID = m.RecipientID
	body.To.Email = m.To
	body.Payload.Subject = m.Subject
	body.Payload.Body = m.Body
	body.TransactionID = m.TransactionID

	raw, err := json.Marshal(body)
	if err != nil {
		return delivery.Receipt{Provider: c.Name(), Status: delivery.StatusError, Err: err}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/events/trigger", bytes.NewReader(raw))
	if err != nil {
		return delivery.Receipt{Provider: c.Name(), Status: delivery.StatusError, Err: err}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return delivery.Receipt{Provider: c.Name(), Status: delivery.StatusError, Err: err}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return delivery.Receipt{Provider: c.Name(), Status: delivery.StatusSent}, nil
	}
	// The body carries notify's error list; keep it short so a long traceback
	// never lands in a database row.
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
	statusErr := fmt.Errorf("notify: trigger status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	return delivery.Receipt{Provider: c.Name(), Status: delivery.StatusError, Err: statusErr}, statusErr
}
