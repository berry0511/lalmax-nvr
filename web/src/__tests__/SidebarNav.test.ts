import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { setLang } from '$lib/i18n';
import SidebarNav from '../components/SidebarNav.svelte';

describe('SidebarNav grouped navigation', () => {
  beforeEach(() => {
    localStorage.clear();
    setLang('en');
  });

  afterEach(() => {
    cleanup();
    localStorage.clear();
  });

  it('auto-expands the current group and marks its child as active', async () => {
    const { getByRole } = render(SidebarNav, {
      props: { activeRoute: '/devices?tab=gb28181&subtab=records' },
    });
    await waitFor(() => expect(getByRole('link', { name: 'Device list' })).toBeTruthy());
    expect(getByRole('button', { name: 'Device management' }).getAttribute('aria-expanded')).toBe('true');
    expect(getByRole('link', { name: 'Device list' }).getAttribute('aria-current')).toBe('page');
    expect(getByRole('link', { name: 'Device list' }).getAttribute('href')).toBe('#/devices');
  });

  it('can manually collapse and reopen the active group', async () => {
    const { getByRole, queryByRole } = render(SidebarNav, {
      props: { activeRoute: '/recordings/record-123' },
    });
    const group = getByRole('button', { name: 'Recording center' });
    await waitFor(() => expect(group.getAttribute('aria-expanded')).toBe('true'));
    await fireEvent.click(group);
    await waitFor(() => expect(queryByRole('link', { name: 'Recordings' })).toBeNull());
    await fireEvent.click(group);
    expect(getByRole('link', { name: 'Recordings' }).getAttribute('aria-current')).toBe('page');
  });

  it('provides a flyout when the desktop sidebar is collapsed', async () => {
    const onNavigate = vi.fn();
    const { getByRole } = render(SidebarNav, {
      props: { activeRoute: '/dashboard', collapsed: true, onNavigate },
    });
    const button = getByRole('button', { name: 'Media and streams' });
    await fireEvent.click(button);
    expect(button.getAttribute('aria-expanded')).toBe('true');
    const sourceLink = getByRole('link', { name: 'Media inputs' });
    expect(sourceLink.getAttribute('href')).toBe('#/media-sources');
    await fireEvent.click(sourceLink);
    expect(onNavigate).toHaveBeenCalledOnce();
  });

  it('shows normal submenus in the mobile drawer even if compact is persisted', async () => {
    const { getByRole } = render(SidebarNav, {
      props: { activeRoute: '/dashboard', collapsed: true, mobileOpen: true },
    });
    const deviceButton = getByRole('button', { name: 'Device management' });
    await fireEvent.click(deviceButton);
    expect(deviceButton.getAttribute('aria-expanded')).toBe('true');
    expect(getByRole('link', { name: 'GB28181 channel catalog' })).toBeTruthy();
  });

  it('keeps the operations overview as a direct link', () => {
    const { getByRole } = render(SidebarNav, { props: { activeRoute: '/stats?tab=traffic' } });
    expect(getByRole('link', { name: 'Operations overview' }).getAttribute('aria-current')).toBe('page');
  });
});
