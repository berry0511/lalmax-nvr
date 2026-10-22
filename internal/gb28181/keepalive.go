package gb28181

import (
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

func (g *GB28181API) handlerNotify(req *sip.Request, tx sip.ServerTransaction) {
	deviceID := req.From().Address.User
	source := req.Source()

	slog.Debug("[SIP] NOTIFY received",
		"device_id", deviceID,
		"source", source,
		"call_id", req.CallID().Value(),
	)

	var msg MessageNotify
	body := req.Body()
	status := "OK"
	if len(body) > 0 {
		slog.Debug("[SIP] NOTIFY body", "device_id", deviceID, "body", string(body))
		if err := xmlUnmarshal(body, &msg); err != nil {
			slog.Error("[SIP] NOTIFY xml decode error", "device_id", deviceID, "error", err, "body", string(body))
			tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
			return
		}
		if msg.Status != "" {
			status = msg.Status
		}
	}

	g.handleKeepaliveFromRequest(deviceID, source, status, req.Transport())

	tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
}

// handleKeepaliveFromRequest restores a TCP signaling route for a previously
// registered device after restart, using a restricted source-IP compatibility
// policy. A SIP MESSAGE itself is NOT authentication; this must only be enabled
// for deployments that trust their device network/source IP.
func (g *GB28181API) handleKeepaliveFromRequest(deviceID, source, status, transport string) {
	g.restoreHistoricalTCPRoute(deviceID, source, transport)
	g.handleKeepalive(deviceID, source, status)
}

func (g *GB28181API) restoreHistoricalTCPRoute(deviceID, source, transport string) {
	if !strings.EqualFold(transport, "TCP") || filterUnknowDevices(deviceID) != nil {
		return
	}
	newHost, _, newErr := net.SplitHostPort(source)
	if newErr != nil || net.ParseIP(newHost) == nil {
		return
	}
	// Validate and update while the current map member is locked; never mutate
	// a stale Device pointer obtained before a concurrent offline deletion.
	changed := false
	if err := g.store.Change(deviceID, func(d *Device) {
		if !d.LastRegisterAt.After(time.Time{}) || d.Transport != "" {
			return
		}
		oldHost, _, oldErr := net.SplitHostPort(d.Address)
		if oldErr != nil || net.ParseIP(oldHost) == nil || oldHost != newHost {
			return
		}
		d.Transport = "TCP"
		d.Address = source
		changed = true
	}); err != nil || !changed {
		return
	}
	slog.Warn("[SIP] historical TCP route restored from matching source IP (compatibility trust)",
		"device_id", deviceID, "source", source)
}

func (g *GB28181API) handleKeepalive(deviceID, source, status string) {
	// Never allow an unauthenticated keepalive to enroll a new device. Validate
	// the registered source and persist the state transition while holding the
	// same device lock used by deletion. If deletion wins, this returns not-found
	// and cannot UPSERT the removed row back into the database.
	isOnline := status == "OK" || status == "ON"
	wasOnline, dev, err := g.store.ChangeAndSaveExistingDevice(deviceID, func(d *Device) error {
		if !d.LastRegisterAt.IsZero() && d.Address != "" {
			oldHost, _, oldErr := net.SplitHostPort(d.Address)
			newHost, _, newErr := net.SplitHostPort(source)
			if oldErr != nil || newErr != nil || oldHost != newHost {
				return ErrDeviceSourceMismatch
			}
		}
		d.LastKeepaliveAt = time.Now()
		d.IsOnline = isOnline
		// The historical route recovery above may update Address only when
		// source IP matched the stored authenticated-registration address.
		if d.Address == "" {
			d.Address = source
		}
		return nil
	})
	if err != nil {
		if err != ErrDeviceNotExist && err != ErrDeviceSourceMismatch {
			slog.Error("[SIP] failed to persist keepalive", "device_id", deviceID, "error", err)
		}
		return
	}
	if wasOnline != isOnline {
		g.store.recordOperation(deviceID, isOnline, source)
	}
	slog.Debug("[SIP] Keepalive processed", "device_id", deviceID, "status", status, "source", source)

	// Avoid starting catalog queries for a device that was removed immediately
	// after the heartbeat transaction completed.
	current, exists := g.store.Load(deviceID)
	if !exists || current != dev {
		return
	}
	hasChannels := false
	dev.Channels.Range(func(key, value any) bool {
		hasChannels = true
		return false
	})
	if !hasChannels {
		slog.Info("[SIP] Device has no channels, querying catalog", "device_id", deviceID)
		go g.sendDeviceInfoQuery(deviceID)
		go g.QueryCatalog(deviceID)
	}
}
