# Experimental Tencent-style WebRTC signaling adapter

Status: **JSON/SDP signaling adapter only — official TCPlayer Demo interoperability NOT verified.**

This adds a second signaling contract alongside (not instead of) the existing
NVR WHEP endpoint. It follows the publicly described Tencent/CDN-style
`POST /rtc/v1/play` + `POST /rtc/v1/unplay` JSON protocol. The implementation
reuses LALMax WHEP for media and ICE; it does not create another publisher or
issue GB28181 INVITE.

## Endpoints (NVR HTTP API port)

- `POST /rtc/v1/play`, `Content-Type: application/json`
- `POST /rtc/v1/unplay`, `Content-Type: application/json`

Both routes **require the same NVR authentication as /api/cameras/.../stream/webrtc**.

Example: existing live channel must be published first:

```json
{
  "streamurl": "webrtc://YOUR_NVR_HOST:19090/live/device-id:channel-id",
  "sdp": "v=0\r\n...browser SDP offer...\r\n"
}
```

Success:

```json
{
  "code": 200,
  "msg": "OK",
  "data": {
    "sdp": "v=0\r\n...SDP answer...\r\n",
    "sessionid": "32-character random session ID"
  }
}
```

Unplay:

```json
{"sessionid":"SESSION_ID_FROM_PLAY"}
```

The adapter only maps `/live/<existing-published-stream-id>` to a local stream.
It never fetches `streamurl` remotely. Session IDs are random and stored
server-side; `unplay` releases the corresponding upstream WHEP session.

## Important limitations

1. **The official TCPlayer WebRTC Demo has not been confirmed to use this
   published JSON signaling contract.** Its SDK may use different Tencent
   proprietary endpoints/headers/responses. Merely changing an address to
   `webrtc://` does not establish interoperability.
2. The external Tencent test page runs over HTTPS and will not normally be
   permitted to signal an HTTP endpoint due to mixed-content restrictions.
   A trusted HTTPS hostname and deliberate CORS policy would be required.
3. These endpoints intentionally inherit NVR auth and do not expose streams
   anonymously or accept NVR credentials in third-party URL query strings.
   The external demo may not be able to provide these credentials.
4. Existing WHEP, RTSP, RTMP, HLS and GB28181 routes are unchanged.
5. Audio codecs, B-frames and browser H.264 profile support are not converted
   by this signaling adapter.
6. Sessions have a bounded in-memory registry. Explicit `unplay` is required
   for deterministic release; crash/disconnect expiry behavior is not yet
   independently validated.

## Validation gate before enabling in production

- `go test ./internal/api -run 'TestTencent' -count=1`
- `go test ./internal/api ./internal/media/... -count=1`
- `go build ./cmd/lalmax-nvr`
- Validate SDP/ICE end-to-end using a client implementing the JSON contract.
- Capture the **official** TCPlayer demo browser Network requests and SDP
  handshake (redact tokens and credentials), then compare method, path,
  payload and headers with this adapter. Do not claim the official Demo works
  until that direct test passes.
- Verify repeat play/stop, tab refresh, WHEP DELETE cleanup, no duplicate
  GB28181 INVITEs, and TLS/CORS/auth constraints on the actual NVR host.

Reference: Tencent Cloud developer article *WebRTC接口设计与CDN集成方案*,
which describes a JSON `/rtc/v1/play` API. This public design is not proof
of the private SDK's live behavior.
