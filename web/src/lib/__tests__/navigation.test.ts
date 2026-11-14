import { describe, expect, it } from 'vitest';
import { NAV_OVERVIEW, NAV_GROUPS, findNavigationSelection, navigationPath } from '$lib/navigation';

describe('navigation v1 information architecture', () => {
  it('retains all 18 existing destinations in seven top-level entries', () => {
    expect(NAV_GROUPS).toHaveLength(6);
    const links = [NAV_OVERVIEW, ...NAV_GROUPS.flatMap((group) => group.children)];
    expect(links).toHaveLength(18);
    expect(new Set(links.map((item) => item.id)).size).toBe(18);
    expect(new Set(links.map((item) => item.href)).size).toBe(18);
    expect(NAV_OVERVIEW.href).toBe('#/stats');
    expect(links.some((item) => item.href === '/sfu-meet/')).toBe(true);
  });

  it.each([
    ['#/dashboard', 'dashboard', 'live'],
    ['#/surveillance', 'dashboard', 'live'],
    ['#/health', 'dashboard', 'live'],
    ['#/dashboard/health', 'dashboard', 'live'],
    ['#/map', 'map', 'live'],
    ['/sfu-meet/', 'sfu', 'live'],
    ['#/cameras', 'device-list', 'devices'],
    ['#/devices?tab=gb28181&subtab=records', 'device-list', 'devices'],
    ['#/live', 'device-list', 'devices'],
    ['#/live/device-1?from=gb28181', 'dashboard', 'live'],
    ['#/device-groups', 'device-groups', 'devices'],
    ['#/gb-channels', 'gb-catalog', 'devices'],
    ['#/media-sources?tab=iptv', 'media-sources', 'media'],
    ['#/iptv', 'media-sources', 'media'],
    ['#/emby', 'media-sources', 'media'],
    ['#/streams?create=pull', 'streams', 'media'],
    ['#/streams/app%2Fstream', 'streams', 'media'],
    ['#/player/stream-1', 'streams', 'media'],
    ['#/relay', 'relay', 'media'],
    ['#/recordings?camera_id=one', 'recordings-list', 'recordings'],
    ['#/recordings/rec-1', 'recordings-list', 'recordings'],
    ['#/recording-plans?stream_id=id', 'recording-plans', 'recordings'],
    ['#/events?camera_id=id', 'event-records', 'events'],
    ['#/ai-detection', 'ai', 'events'],
    ['#/operation-logs', 'logs', 'system'],
    ['#/logs/service', 'logs', 'system'],
    ['#/users', 'users', 'system'],
    ['#/voip', 'voip', 'system'],
    ['#/settings', 'settings', 'system'],
    ['#/stats?tab=traffic', 'overview', null],
  ])('highlights %s without changing the route', (input, itemId, groupId) => {
    expect(findNavigationSelection(input)).toEqual({ itemId, groupId });
  });

  it('ignores query strings, trailing slashes, and unknown route ids', () => {
    expect(navigationPath('#/devices/?tab=onvif')).toBe('/devices');
    expect(navigationPath('#/streams/app%2Fname?foo=bar')).toBe('/streams/app%2Fname');
    expect(findNavigationSelection('#/not-a-page')).toBeNull();
  });
});
