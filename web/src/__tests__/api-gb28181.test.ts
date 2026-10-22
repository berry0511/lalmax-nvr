import { beforeEach, describe, expect, it, vi } from 'vitest';
import { deleteGB28181Device } from '$lib/api/gb28181';
import { ApiRequestError } from '$lib/api/client';

vi.stubGlobal('sessionStorage', {
  getItem: vi.fn(() => null),
  setItem: vi.fn(), removeItem: vi.fn(),
});

describe('GB28181 offline device removal', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('uses a DELETE with URL-escaped device ID', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ status: 'deleted', device_id: 'a:b' }),
    });
    await expect(deleteGB28181Device('a:b')).resolves.toEqual({
      status: 'deleted', device_id: 'a:b',
    });
    expect(global.fetch).toHaveBeenCalledWith('/api/gb28181/devices/a%3Ab',
      expect.objectContaining({method: 'DELETE'}));
  });

  it('propagates server-side refusal to delete online devices', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false, status: 409,
      json: () => Promise.resolve({error: 'device is online'}),
    });
    await expect(deleteGB28181Device('device-online')).rejects.toThrow(ApiRequestError);
  });
});
