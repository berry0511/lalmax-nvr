# Media Inputs

**Media Inputs** is the unified NVR Web UI for configuring media inputs. It brings together cameras and devices, live source URLs, IPTV playlists, Emby libraries, and encoder push, with status summaries and links to each management flow.

Open **Media Inputs** from the sidebar. Useful routes include:

- Overview: `#/media-sources`
- IPTV: `#/media-sources?tab=iptv`
- Emby: `#/media-sources?tab=emby`
- Cameras and devices: `#/devices`
- Live URLs and encoder push: `#/streams`

Legacy links `#/iptv` and `#/emby` still open the corresponding setup page under Media Inputs.

## Input types

| Type | Use | Setup |
|------|-----|-------|
| Cameras and devices | Connect ONVIF, RTSP, GB28181, and other supported devices | Add input → Camera or device; devices can also be added directly from the device page |
| Live source URL | Pull from supported RTSP, RTMP, HTTP-FLV, and other URLs | Add input → Live source URL |
| IPTV playlist | Import M3U, then select live channels to manage | Add input → IPTV playlist |
| Emby library | Browse Emby movies or episodes and add them as NVR media items | Add input → Emby library |
| Encoder push | Create a stream and configure an NVR ingest URL in the encoder | Add input → Encoder push |

Jellyfin, Plex, and local / NAS files are shown as planned types and cannot be configured yet.

## Sources, content, and runtime state

- A **source configuration** describes where NVR gets media, such as an Emby server or M3U playlist.
- A **content item** is manageable media within a source, such as an IPTV channel or Emby movie / episode.
- A **runtime stream** is a published, pushed, or pulled stream. View it in the stream manager.
- A **recording plan** determines which stream should be recorded and when. Configure it in Recording Plans.

The overview summarizes source status and counts; a configured source is not itself a live stream.

## Choose a source type

| Source | Use | Add content | Playback and publication |
|--------|-----|-------------|--------------------------|
| IPTV | Manage live channels from M3U playlists | Import an M3U URL or text, probe it, then enroll selected channels | Play directly in the browser through the NVR same-origin HLS proxy. Server-side pulls start only when a channel is published or a recording plan needs it. |
| Emby Library | Publish movies or episodes from an Emby library as NVR live streams | Connect to Emby, search for an item, and add it to NVR | NVR ingests HLS from Emby when published. The stream goes offline at the end, or restarts from the beginning when looping is enabled. |

Both source types can be published as NVR streams and played through supported NVR protocols. IPTV channels and Emby items have stable `stream_id` values for stream details; IPTV `stream_id` values can also be used in recording plans.

## IPTV workflow

1. Open **Media Inputs → IPTV playlist** and import an M3U URL or paste `#EXTM3U` text.
2. Review probe results and enroll the channels you want to keep.
3. Click a channel to play it in the browser. To play it over RTSP, HTTP-FLV, HLS, WebRTC, or another NVR protocol, enable **Publish** on its card.
4. To record it, create a recording plan that uses the channel's `stream_id`.

Browser playback uses the NVR same-origin proxy and does not depend on upstream CORS. Publication and recording share the server-side HLS pull. See the [IPTV guide](iptv.md) for details.

## Emby workflow

1. Open **Media Inputs → Emby Library**, enter the Emby server URL, user ID, and access token, then connect.
2. Search for a movie or episode and add it to NVR.
3. Enable **Publish** when needed. The stream goes offline when playback ends. Enable **Loop after end** to create a new playback session and restart the item.

The Emby server must be reachable from the NVR host, and its HLS output must use codecs supported by NVR. See the [Emby Library guide](emby.md) for configuration and lifecycle details.

## APIs and configuration

The Web UI is unified, while the server APIs remain source-specific. See the IPTV and Emby sections in the [API reference](api-reference.md#iptv) and [Emby section](api-reference.md#emby). Sensitive-data key setup is covered in the [Emby Library guide](emby.md#configure).
