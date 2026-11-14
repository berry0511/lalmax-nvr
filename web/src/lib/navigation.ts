// Navigation information architecture (v1).
// Links deliberately retain the existing hash routes; only their presentation changes.

export interface NavigationItem {
  id: string;
  labelKey: string;
  icon: string;
  href: string;
}

export interface NavigationGroup {
  id: string;
  labelKey: string;
  icon: string;
  children: NavigationItem[];
}

export interface NavigationSelection {
  itemId: string;
  groupId: string | null;
}

export const NAV_OVERVIEW: NavigationItem = {
  id: 'overview',
  labelKey: 'nav.v1.overview',
  icon: 'overview',
  href: '#/stats',
};

export const NAV_GROUPS: NavigationGroup[] = [
  {
    id: 'live',
    labelKey: 'nav.v1.live',
    icon: 'live',
    children: [
      { id: 'dashboard', labelKey: 'nav.v1.monitor', icon: 'monitor', href: '#/dashboard' },
      { id: 'map', labelKey: 'nav.v1.map', icon: 'map', href: '#/map' },
      { id: 'sfu', labelKey: 'nav.v1.sfu', icon: 'sfu', href: '/sfu-meet/' },
    ],
  },
  {
    id: 'devices',
    labelKey: 'nav.v1.devices',
    icon: 'devices',
    children: [
      { id: 'device-list', labelKey: 'nav.v1.deviceList', icon: 'monitor', href: '#/devices' },
      { id: 'device-groups', labelKey: 'nav.v1.deviceGroups', icon: 'folder', href: '#/device-groups' },
      { id: 'gb-catalog', labelKey: 'nav.v1.gbCatalog', icon: 'network', href: '#/gb-channels' },
    ],
  },
  {
    id: 'media',
    labelKey: 'nav.v1.media',
    icon: 'media',
    children: [
      { id: 'media-sources', labelKey: 'nav.v1.sources', icon: 'link', href: '#/media-sources' },
      { id: 'streams', labelKey: 'nav.v1.streams', icon: 'media', href: '#/streams' },
      { id: 'relay', labelKey: 'nav.v1.relay', icon: 'send', href: '#/relay' },
    ],
  },
  {
    id: 'recordings',
    labelKey: 'nav.v1.recordings',
    icon: 'recordings',
    children: [
      { id: 'recordings-list', labelKey: 'nav.v1.recordingSearch', icon: 'recordings', href: '#/recordings' },
      { id: 'recording-plans', labelKey: 'nav.v1.recordingPlans', icon: 'calendar', href: '#/recording-plans' },
    ],
  },
  {
    id: 'events',
    labelKey: 'nav.v1.events',
    icon: 'events',
    children: [
      { id: 'event-records', labelKey: 'nav.v1.eventRecords', icon: 'events', href: '#/events' },
      { id: 'ai', labelKey: 'nav.v1.ai', icon: 'brain', href: '#/ai' },
    ],
  },
  {
    id: 'system',
    labelKey: 'nav.v1.system',
    icon: 'system',
    children: [
      { id: 'users', labelKey: 'nav.v1.users', icon: 'users', href: '#/users' },
      { id: 'logs', labelKey: 'nav.v1.logs', icon: 'logs', href: '#/logs' },
      { id: 'voip', labelKey: 'nav.v1.voip', icon: 'phone', href: '#/voip' },
      { id: 'settings', labelKey: 'nav.v1.settings', icon: 'system', href: '#/settings' },
    ],
  },
];

/**
 * Normalize for nav highlighting, not for routing.
 * App.svelte continues to own the actual hash router and its query parameters.
 */
export function navigationPath(location: string): string {
  let path = location.trim();
  const hashIndex = path.indexOf('#');
  if (hashIndex >= 0) path = path.slice(hashIndex + 1);
  path = path.split('?')[0] || '/';
  if (!path.startsWith('/')) path = '/' + path;
  return path.replace(/\/+$/, '') || '/';
}

/** Resolve a page and its parent group, including detail pages and legacy aliases. */
export function findNavigationSelection(location: string): NavigationSelection | null {
  const path = navigationPath(location);
  if (path === '/sfu-meet' || path.startsWith('/sfu-meet/')) {
    return { itemId: 'sfu', groupId: 'live' };
  }
  const segment = path.split('/')[1] || '';
  const selection: Record<string, NavigationSelection> = {
    '': { itemId: 'dashboard', groupId: 'live' },
    dashboard: { itemId: 'dashboard', groupId: 'live' },
    surveillance: { itemId: 'dashboard', groupId: 'live' },
    health: { itemId: 'dashboard', groupId: 'live' },
    status: { itemId: 'dashboard', groupId: 'live' },
    live: { itemId: path === '/live' ? 'device-list' : 'dashboard', groupId: path === '/live' ? 'devices' : 'live' },
    map: { itemId: 'map', groupId: 'live' },
    devices: { itemId: 'device-list', groupId: 'devices' },
    cameras: { itemId: 'device-list', groupId: 'devices' },
    'device-groups': { itemId: 'device-groups', groupId: 'devices' },
    'gb-channels': { itemId: 'gb-catalog', groupId: 'devices' },
    'media-sources': { itemId: 'media-sources', groupId: 'media' },
    iptv: { itemId: 'media-sources', groupId: 'media' },
    emby: { itemId: 'media-sources', groupId: 'media' },
    streams: { itemId: 'streams', groupId: 'media' },
    player: { itemId: 'streams', groupId: 'media' },
    relay: { itemId: 'relay', groupId: 'media' },
    recordings: { itemId: 'recordings-list', groupId: 'recordings' },
    'recording-plans': { itemId: 'recording-plans', groupId: 'recordings' },
    events: { itemId: 'event-records', groupId: 'events' },
    ai: { itemId: 'ai', groupId: 'events' },
    'ai-detection': { itemId: 'ai', groupId: 'events' },
    users: { itemId: 'users', groupId: 'system' },
    logs: { itemId: 'logs', groupId: 'system' },
    'operation-logs': { itemId: 'logs', groupId: 'system' },
    voip: { itemId: 'voip', groupId: 'system' },
    settings: { itemId: 'settings', groupId: 'system' },
    stats: { itemId: 'overview', groupId: null },
  };
  return selection[segment] ?? null;
}