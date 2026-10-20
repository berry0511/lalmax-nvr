import { describe, expect, it } from 'vitest';
import { selectFlvStreamURL } from '$lib/flv-playback';

describe('FLV playback URL routing', () => {
  it('uses the independent-stream HTTP-FLV URL instead of the camera fallback', () => {
    expect(selectFlvStreamURL('/api/streams/rtmp-room/stream.flv', '/api/cameras/rtmp-room/stream.flv'))
      .toBe('/api/streams/rtmp-room/stream.flv');
  });

  it('preserves an explicit WS-FLV URL', () => {
    expect(selectFlvStreamURL('wss://example.test/live/feed.flv', 'ws://localhost/live/feed.flv'))
      .toBe('wss://example.test/live/feed.flv');
  });

  it('uses the caller-provided fallback only without a stream URL', () => {
    expect(selectFlvStreamURL('', '/api/cameras/camera1/stream.flv'))
      .toBe('/api/cameras/camera1/stream.flv');
  });
});
