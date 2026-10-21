/** WHEP ICE is a transport state: camera playback is only ready after video. */
export function whepPlaybackReady(
  connectionState: string,
  expectVideo: boolean,
  firstVideoFrameSeen: boolean,
): boolean {
  return connectionState === 'connected' && (!expectVideo || firstVideoFrameSeen);
}

/** Distinguishes a video-only startup stall from normal audio-only VoIP. */
export function whepFirstVideoFrameTimedOut(
  connectedAt: number,
  now: number,
  expectVideo: boolean,
  firstVideoFrameSeen: boolean,
  visible: boolean,
  timeoutMs = 15000,
): boolean {
  return expectVideo && visible && !firstVideoFrameSeen && connectedAt > 0 &&
    now - connectedAt >= timeoutMs;
}
