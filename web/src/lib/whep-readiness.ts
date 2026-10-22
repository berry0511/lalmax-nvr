/** WHEP ICE is a transport state: camera playback is only ready after video. */
export function whepPlaybackReady(
  connectionState: string,
  expectVideo: boolean,
  firstVideoFrameSeen: boolean,
): boolean {
  return connectionState === 'connected' && (!expectVideo || firstVideoFrameSeen);
}

/** True only if the SDP answer negotiates a video section that can send media to this client. */
export function sdpHasActiveVideoMedia(sdp: string): boolean {
  const sections: Array<{ port: number; direction: string }> = [];
  let current: { port: number; direction: string } | null = null;

  for (const rawLine of sdp.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (line.startsWith('m=')) {
      if (current) sections.push(current);
      current = null;
      if (line.startsWith('m=video ')) {
        const match = /^m=video\s+(\d+)/.exec(line);
        if (match) current = { port: Number(match[1]), direction: 'sendrecv' };
      }
      continue;
    }
    if (current && /^a=(sendrecv|sendonly|recvonly|inactive)$/.test(line)) {
      current.direction = line.slice(2);
    }
  }
  if (current) sections.push(current);

  return sections.some(section =>
    section.port > 0 && (section.direction === 'sendonly' || section.direction === 'sendrecv')
  );
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
