# Emby Library

The Emby integration publishes movies or episodes from an Emby media library as NVR live streams. Emby owns the library and transcoding; NVR ingests a compatible HLS session into `lalmax` and distributes it through NVR playback protocols.

## Configure

Open **Media Inputs → Emby Library** in the sidebar (`#/media-sources?tab=emby`; the legacy `#/emby` link still works), then enter the Emby server URL, user ID, and access token. After connecting, search movies and episodes and add them as NVR channels. The access token stays on the server and is encrypted at rest when data encryption is enabled. See the [Media Inputs guide](media-sources.md) for how Emby and IPTV fit together.

Configure the key once under **Settings → Security**. Web settings store it separately beside the config in a `.encryption-key` file with mode `0600`, and it takes effect immediately. You can also manage the key with the `NVR_ENCRYPTION_KEY` or `NVR_ENCRYPTION_KEY_FILE` environment variables; environment configuration takes precedence. The key cannot be replaced directly after setup. Back up the key file, and migrate existing ciphertext before rotating the key.

The Emby server must be reachable from the NVR host. NVR requests Emby's `PlaybackInfo` and then its HLS playlist. Emby controls media authorization and transcoding availability.

## Publication lifecycle

- Publication plays once by default. At the end, the HLS pull ends, the NVR stream goes offline, and the publish toggle is cleared.
- Enable **Loop after end** to create a fresh Emby playback session and restart the item from the beginning.
- Stopping publication cancels the pull. The managed item remains available to publish again.
- Each item has a stable NVR `stream_id`; stream details show available RTSP, HLS, HTTP-FLV, WebRTC, and other playback URLs.

Emby publication requires embedded media mode and H.264/H.265 video with optional AAC audio in the HLS output. Emby transcoding is recommended for compatibility.
