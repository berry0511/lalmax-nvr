# Events

The Events page brings camera health, recorder recovery, AI detections, and MQTT triggers into one timeline. Filter by camera, source, and status, browse pages, and acknowledge open events. The page subscribes to server events and refreshes the list when an update arrives; it can also be refreshed manually.

## Event sources

| Source | Contents |
|--------|----------|
| `health` | Camera connection lost/restored, stream anomalies, and video freeze/recovery events |
| `recorder` | Recorder recovery after a disconnect, including downtime, retry count, and recording reference |
| `ai` | Object detections reported by browser inference or a webhook detector; repeats for the same camera and class set are suppressed for 30 seconds |
| `mqtt` | Camera actions executed by an MQTT trigger, such as starting or stopping recording |

Events have `open` or `acknowledged` status. Acknowledgement records the time it was handled. Event details retain the source, type, severity, message, timestamps, and any available recording ID, snapshot, and metadata. Users with operate permission can acknowledge or delete events.

## Alarm rules

The rules section lets you add rules for a camera and event source with one of these actions:

- **Record**: trigger event recording for the associated camera.
- **Webhook**: send an event notification to the configured endpoint.
- **Go to preset**: ask the camera to move to a configured PTZ preset.

Match the rule's camera, source, and action target to the device capabilities and service configuration. See [MQTT integration](mqtt-integration.md) for MQTT trigger topics and actions.

## Troubleshooting

If an event is missing, first confirm that its camera ID matches a device in the NVR, then clear source and status filters. Browser AI requires AI to be enabled, a model to load, and WebCodecs playback; seeing live video alone does not mean an object was detected. API and live subscription details are available at `/docs/` on a running NVR.
