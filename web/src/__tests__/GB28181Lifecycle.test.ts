import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import Devices from '../routes/Devices.svelte';
import {
  listCameras, listGB28181Devices, playGB28181Stream, startCamera, stopCamera, stopGB28181Stream,
  type Camera,
} from '$lib/api';
import { t } from '$lib/i18n';

vi.mock('$lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('$lib/api')>(),
  listCameras: vi.fn(),
  listGB28181Devices: vi.fn(),
  playGB28181Stream: vi.fn(),
  stopGB28181Stream: vi.fn(),
  startCamera: vi.fn().mockResolvedValue(undefined),
  stopCamera: vi.fn().mockResolvedValue(undefined),
  getHealthStatus: vi.fn().mockResolvedValue({}),
  listArchives: vi.fn().mockResolvedValue({ archives: [] }),
  listProtocols: vi.fn().mockResolvedValue([]),
  xiaomiDevices: vi.fn().mockResolvedValue({ devices: [] }),
  subscribeNvrEvents: vi.fn(() => () => {}),
}));

let playing: boolean;
const camera = { id: 'device:channel', name: 'Channel camera', protocol: 'gb28181', enabled: true };

beforeEach(() => {
  playing = true;
  vi.clearAllMocks();
  vi.mocked(listCameras).mockResolvedValue([]);
  vi.mocked(listGB28181Devices).mockImplementation(async () => ({
    devices: [{
      device_id: 'device', is_online: true, address: 'local',
      channels: [{ channel_id: 'channel', is_playing: playing, media_active: playing }],
    }],
  }));
  vi.mocked(stopGB28181Stream).mockImplementation(async () => {
    playing = false;
    return { status: 'stopped' };
  });
  vi.mocked(playGB28181Stream).mockImplementation(async () => {
    playing = true;
    return { ssrc: 'new-session', stream_id: 'device:channel' };
  });
});
afterEach(cleanup);

it('displays an active dialog with no recorder and permits stop and play without a camera', async () => {
  const view = render(Devices, { initialTab: 'gb28181' });
  expect(await view.findByText('点播会话已建立')).toBeTruthy();
  expect(view.getByText('媒体有数据')).toBeTruthy();
  expect(view.getByText('未录像')).toBeTruthy();
  await fireEvent.click(view.getByTitle('停止推流'));
  expect(await view.findByText('未点播')).toBeTruthy();
  expect(stopGB28181Stream).toHaveBeenCalledWith('device', 'channel');
  await fireEvent.click(view.getByTitle('开始推流'));
  expect(await view.findByText('点播会话已建立')).toBeTruthy();
  expect(playGB28181Stream).toHaveBeenCalledWith({ device_id: 'device', channel_id: 'channel' });
});

it('restarts the SIP session once and never uses Camera start/stop', async () => {
  const view = render(Devices, { initialTab: 'gb28181' });
  const restart = await view.findByTitle('重启推流');
  await Promise.all([fireEvent.click(restart), fireEvent.click(restart)]);
  await waitFor(() => expect(playGB28181Stream).toHaveBeenCalledTimes(1));
  expect(stopGB28181Stream).toHaveBeenCalledTimes(1);
  expect(vi.mocked(stopGB28181Stream).mock.invocationCallOrder[0])
    .toBeLessThan(vi.mocked(playGB28181Stream).mock.invocationCallOrder[0]);
  expect(startCamera).not.toHaveBeenCalled();
  expect(stopCamera).not.toHaveBeenCalled();
});

it('refreshes server state after a failed stop and does not play again', async () => {
  const view = render(Devices, { initialTab: 'gb28181' });
  vi.mocked(stopGB28181Stream).mockRejectedValueOnce(new Error('BYE rejected'));
  const restart = await view.findByTitle('重启推流') as HTMLButtonElement;
  const previousQueries = vi.mocked(listGB28181Devices).mock.calls.length;
  await fireEvent.click(restart);
  await waitFor(() => expect(listGB28181Devices).toHaveBeenCalledTimes(previousQueries + 2));
  await waitFor(() => expect(restart.disabled).toBe(false));
  expect(view.getByText('点播会话已建立')).toBeTruthy();
  expect(playGB28181Stream).not.toHaveBeenCalled();
});

it('shows the recorder independently and uses only Camera APIs to restart it', async () => {
  playing = false;
  vi.mocked(listCameras).mockResolvedValue([{ ...camera, status: 'recording' } as Camera]);
  const view = render(Devices, { initialTab: 'gb28181' });
  expect(await view.findByText('未点播')).toBeTruthy();
  expect(view.getByText('录像中')).toBeTruthy();
  await fireEvent.click(view.getByTitle('重启录像'));
  await fireEvent.click(await view.findByRole('button', { name: t('common.confirm') }));
  await waitFor(() => expect(startCamera).toHaveBeenCalledWith(camera.id));
  expect(stopCamera).toHaveBeenCalledWith(camera.id);
  expect(playGB28181Stream).not.toHaveBeenCalled();
  expect(stopGB28181Stream).not.toHaveBeenCalled();
});
