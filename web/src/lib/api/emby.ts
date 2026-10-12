import { apiRequest } from './client';

export interface EmbyConfig {
  name: string;
  base_url: string;
  user_id: string;
  token?: string;
  configured: boolean;
  has_token: boolean;
}

export interface EmbyMediaItem {
  id: string;
  name: string;
  item_type: string;
  overview?: string;
  image_tag?: string;
  run_time_ticks?: number;
}

export interface EmbyManagedItem extends EmbyMediaItem {
  emby_item_id: string;
  stream_id: string;
  publish_enabled: boolean;
  loop: boolean;
}

export async function getEmbyConfig(): Promise<EmbyConfig> { return apiRequest('/emby/config'); }
export async function saveEmbyConfig(data: { name?: string; base_url: string; user_id: string; token: string }): Promise<EmbyConfig> {
  return apiRequest('/emby/config', { method: 'PUT', body: JSON.stringify(data) });
}
export async function browseEmbyLibrary(params?: { q?: string; limit?: number; offset?: number }): Promise<{ items: EmbyMediaItem[]; total: number }> {
  const query = new URLSearchParams();
  if (params?.q) query.set('q', params.q);
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.offset) query.set('offset', String(params.offset));
  const encoded = query.toString();
  return apiRequest(`/emby/library${encoded ? `?${encoded}` : ''}`);
}
export async function listEmbyItems(): Promise<EmbyManagedItem[]> {
  const res = await apiRequest<{ items: EmbyManagedItem[] }>('/emby/items');
  return res.items ?? [];
}
export async function addEmbyItem(id: string): Promise<EmbyManagedItem> {
  return apiRequest('/emby/items', { method: 'POST', body: JSON.stringify({ emby_item_id: id }) });
}
export async function updateEmbyItem(id: string, data: { publish_enabled?: boolean; loop?: boolean }): Promise<EmbyManagedItem> {
  return apiRequest(`/emby/items/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(data) });
}
export async function deleteEmbyItem(id: string): Promise<void> {
  await apiRequest(`/emby/items/${encodeURIComponent(id)}`, { method: 'DELETE' });
}
