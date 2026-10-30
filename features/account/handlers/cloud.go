// SCOPE:feature — Cloud sync settings and device-token issuance on /account.
package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/cloudcfg"
	"github.com/calionauta/gogogo/internal/license"
)

// connectCloud stores the server URL and token under DATA_DIR. It applies on
// the next launch (the NATS transport is built at boot); the page says so.
func (h *Handler) connectCloud(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, sess, "Invalid form submission.")
		return
	}
	cfg := cloudcfg.Config{
		URL:   r.PostFormValue("url"),
		Token: r.PostFormValue("token"),
	}
	if aerr := cloudcfg.HostAllowed(cfg.URL, h.cfg.CloudSyncAllow); aerr != nil {
		h.renderError(w, r, sess, "Cloud sync: "+aerr.Error())
		return
	}
	if err := cloudcfg.Save(cloudcfg.Path(h.cfg.DataDir), cfg); err != nil {
		h.renderError(w, r, sess, "Cloud sync: "+err.Error())
		return
	}
	http.Redirect(w, r, "/account?notice=Cloud+sync+saved.+Restart+the+app+to+connect.", http.StatusSeeOther)
}

// disconnectCloud removes the saved connection.
func (h *Handler) disconnectCloud(w http.ResponseWriter, r *http.Request) {
	_, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := cloudcfg.Clear(cloudcfg.Path(h.cfg.DataDir)); err != nil {
		http.Error(w, "cloud sync: disconnect failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account?notice=Cloud+sync+disconnected.", http.StatusSeeOther)
}

// licenseSummary is the one line the account page shows for the gate.
func licenseSummary(cfg *config.Config) string {
	if cfg.LicenseMode == config.LicenseNone {
		return ""
	}
	if cfg.LicensePublicKey == "" {
		// Issuer-only deployment: no local activation to report.
		return ""
	}
	pub, err := license.ParsePublicKey(cfg.LicensePublicKey)
	if err != nil {
		return "misconfigured (LICENSE_PUBLIC_KEY)"
	}
	st, oerr := license.Open(cfg.LicenseFile, pub)
	if oerr != nil || !st.Valid(time.Now()) {
		return "not activated"
	}
	c := st.Claims()
	parts := []string{cfg.LicenseMode}
	if c.Plan != "" {
		parts = append(parts, c.Plan)
	}
	if c.Sub != "" {
		parts = append(parts, "account "+shortID(c.Sub))
	}
	if c.ExpiresAt > 0 {
		parts = append(parts, "until "+time.Unix(c.ExpiresAt, 0).UTC().Format("2006-01-02"))
	} else {
		parts = append(parts, "no expiry")
	}
	return strings.Join(parts, " · ")
}

// deviceToken mints a device token for the signed-in user, so a desktop can
// activate in account mode without the owner running the CLI. The token is a
// bearer credential for this account: shown once, never stored here.
func (h *Handler) deviceToken(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if h.cfg.LicenseMode != config.LicenseAccount || h.cfg.LicensePrivateKey == "" {
		h.renderError(w, r, sess, "Device tokens are not enabled on this server.")
		return
	}
	priv, err := license.ParsePrivateKey(h.cfg.LicensePrivateKey)
	if err != nil {
		http.Error(w, "device token: server key misconfigured", http.StatusInternalServerError)
		return
	}
	claims := license.Claims{
		Sub:       sess.User.ID,
		Email:     sess.User.Email,
		Plan:      "account",
		SyncURL:   h.cfg.CloudSyncURL,
		SyncToken: h.cfg.NATS.Token,
	}
	if h.cfg.DeviceTokenDays > 0 {
		claims.ExpiresAt = time.Now().UTC().AddDate(0, 0, h.cfg.DeviceTokenDays).Unix()
	}
	token, err := license.Sign(priv, claims)
	if err != nil {
		http.Error(w, "device token: sign failed", http.StatusInternalServerError)
		return
	}
	data, perr := h.pageData(r, sess)
	if perr != nil {
		http.Error(w, "account unavailable", http.StatusInternalServerError)
		return
	}
	data.DeviceToken = token
	data.Notice = "Copy this device token now — it is shown once."
	h.render(w, r, data)
}
