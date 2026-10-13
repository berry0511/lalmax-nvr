import { render, fireEvent, cleanup, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Events from '../routes/Events.svelte';
import { acknowledgeEvent, createAlarmRule, listAlarmRules, listCameras, listEvents } from '$lib/api';
import type { EventsResponse } from '$lib/api';
import { i18nState } from '$lib/i18n';

vi.mock('$lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('$lib/api')>();
  return {
    ...actual,
    listEvents: vi.fn(),
    acknowledgeEvent: vi.fn(),
    listCameras: vi.fn(),
    listAlarmRules: vi.fn(),
    createAlarmRule: vi.fn(),
    deleteAlarmRule: vi.fn(),
    subscribeNvrEvents: vi.fn(() => () => {}),
  };
});

const event = {
  id: 1,
  camera_id: 'cam-1',
  source: 'health',
  type: 'connection_lost',
  severity: 'critical',
  status: 'open',
  message: 'Camera disconnected',
  metadata: '{}',
  started_at: '2026-01-01T00:00:00Z',
  created_at: '2026-01-01T00:00:00Z',
};

beforeEach(() => {
  vi.clearAllMocks();
  i18nState.currentLang = 'en';
  vi.mocked(listCameras).mockResolvedValue([
    { id: 'cam-1', name: 'Front Door', protocol: 'rtsp', url: '', enabled: true },
    { id: 'cam-2', name: 'Garage', protocol: 'rtsp', url: '', enabled: true },
  ]);
  vi.mocked(listAlarmRules).mockResolvedValue([]);
  vi.mocked(createAlarmRule).mockResolvedValue({} as Awaited<ReturnType<typeof createAlarmRule>>);
  vi.mocked(acknowledgeEvent).mockResolvedValue({ status: 'acknowledged' });
});

afterEach(cleanup);

describe('Event Center', () => {
  it('removes acknowledged events from the open filter using server state', async () => {
    let acknowledged = false;
    vi.mocked(listEvents).mockImplementation(async (params) => ({
      events: params?.status === 'open' && acknowledged ? [] : [event],
      total: params?.status === 'open' && acknowledged ? 0 : 1,
      limit: 20,
      offset: 0,
    }));
    vi.mocked(acknowledgeEvent).mockImplementation(async () => {
      acknowledged = true;
      return { status: 'acknowledged' };
    });

    const view = render(Events);
    const statusFilter = await view.findByLabelText('Status');
    await fireEvent.change(statusFilter, { target: { value: 'open' } });
    await view.findByText('Camera disconnected');
    await fireEvent.click(view.getByTitle('Acknowledge event'));

    await waitFor(() => expect(acknowledgeEvent).toHaveBeenCalledWith(1));
    await view.findByText('No events yet');
    expect(view.queryByText('Camera disconnected')).toBeNull();
  });

  it('uses an explicit rule camera instead of the event list filter', async () => {
    vi.mocked(listEvents).mockResolvedValue({ events: [], total: 0, limit: 20, offset: 0 });
    const view = render(Events);

    await view.findAllByRole('option', { name: 'Front Door' });
    await fireEvent.change(await view.findByLabelText('Filter by Camera'), { target: { value: 'cam-1' } });
    await waitFor(() => expect(listEvents).toHaveBeenCalledWith(expect.objectContaining({ camera_id: 'cam-1' })));
    await fireEvent.change(view.getByLabelText('Rule camera'), { target: { value: 'cam-2' } });
    await fireEvent.click(view.getByRole('button', { name: 'Add rule' }));

    await waitFor(() => expect(createAlarmRule).toHaveBeenCalledWith(expect.objectContaining({ camera_id: 'cam-2' })));
  });

  it('ignores a slow response from an older filter selection', async () => {
    const staleEvent = { ...event, camera_id: 'cam-2', message: 'Stale previous filter result' };
    let resolveUnfiltered!: (value: EventsResponse) => void;
    vi.mocked(listEvents).mockImplementation((params) => {
      if (params?.status === 'open') {
        return Promise.resolve({ events: [event], total: 1, limit: 20, offset: 0 });
      }
      return new Promise((resolve) => { resolveUnfiltered = resolve; });
    });

    const view = render(Events);
    await waitFor(() => expect(resolveUnfiltered).toBeTypeOf('function'));
    await fireEvent.change(await view.findByLabelText('Status'), { target: { value: 'open' } });
    await waitFor(() => expect(listEvents).toHaveBeenCalledWith(expect.objectContaining({ status: 'open' })));
    await view.findByText('Camera disconnected');
    resolveUnfiltered({ events: [staleEvent], total: 1, limit: 20, offset: 0 });

    await waitFor(() => expect(view.queryByText('Stale previous filter result')).toBeNull());
    expect(view.getByText('Camera disconnected')).toBeTruthy();
  });
});
