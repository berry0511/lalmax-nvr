import { describe, it, expect } from 'vitest';
import { whepPlaybackReady, whepFirstVideoFrameTimedOut } from '$lib/whep-readiness';

describe('WHEP decoded video readiness', () => {
  it('does not equate ICE connected with a rendered GB28181 video frame', () => {
    expect(whepPlaybackReady('connected', true, false)).toBe(false);
    expect(whepPlaybackReady('connected', true, true)).toBe(true);
    expect(whepPlaybackReady('connecting', true, true)).toBe(false);
  });

  it('keeps legitimate audio-only VoIP WHEP streams playable', () => {
    expect(whepPlaybackReady('connected', false, false)).toBe(true);
    expect(whepFirstVideoFrameTimedOut(1000, 25000, false, false, true)).toBe(false);
  });

  it('recovers from initial black video after 15s but not when hidden or already decoded', () => {
    expect(whepFirstVideoFrameTimedOut(1000, 15999, true, false, true)).toBe(false);
    expect(whepFirstVideoFrameTimedOut(1000, 16000, true, false, true)).toBe(true);
    expect(whepFirstVideoFrameTimedOut(1000, 16000, true, false, false)).toBe(false);
    expect(whepFirstVideoFrameTimedOut(1000, 16000, true, true, true)).toBe(false);
  });
});
