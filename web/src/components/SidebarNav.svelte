<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '$lib/i18n';
  import { getPreference, setPreference } from '$lib/preferences';
  import { NAV_OVERVIEW, NAV_GROUPS, findNavigationSelection } from '$lib/navigation';
  import {
    LayoutDashboard, Monitor, MapPin, Radio, FolderTree, Network,
    Link, Film, CalendarClock, Bell, Brain, Users, ClipboardList,
    Phone, Settings, Send, ChevronDown, ChevronRight,
  } from 'lucide-svelte';

  type Props = {
    activeRoute?: string;
    collapsed?: boolean;
    mobileOpen?: boolean;
    onNavigate?: () => void;
  };

  let {
    activeRoute = '',
    collapsed = false,
    mobileOpen = false,
    onNavigate = () => {},
  }: Props = $props();

  const icons: Record<string, any> = {
    overview: LayoutDashboard,
    live: Monitor,
    monitor: Monitor,
    map: MapPin,
    sfu: Radio,
    devices: Monitor,
    folder: FolderTree,
    network: Network,
    media: Radio,
    link: Link,
    send: Send,
    recordings: Film,
    calendar: CalendarClock,
    events: Bell,
    brain: Brain,
    users: Users,
    logs: ClipboardList,
    phone: Phone,
    system: Settings,
  };

  const preferenceKey = 'nav_v1_expanded_groups';
  let expandedGroups = $state<string[]>([]);
  let hydrated = $state(false);
  let flyoutGroup = $state<string | null>(null);
  let flyoutTop = $state(64);
  let lastRoute = '';
  let compact = $derived(collapsed && !mobileOpen);
  let selection = $derived(findNavigationSelection(activeRoute));

  function toggleGroup(groupId: string, event: MouseEvent) {
    if (compact) {
      if (flyoutGroup === groupId) {
        flyoutGroup = null;
        return;
      }
      const button = event.currentTarget as HTMLElement;
      const rect = button.getBoundingClientRect();
      const group = NAV_GROUPS.find((item) => item.id === groupId);
      const estimatedHeight = (group?.children.length || 0) * 42 + 52;
      flyoutTop = Math.max(58, Math.min(rect.top, window.innerHeight - estimatedHeight - 8));
      flyoutGroup = groupId;
      return;
    }
    expandedGroups = expandedGroups.includes(groupId)
      ? expandedGroups.filter((id) => id !== groupId)
      : [...expandedGroups, groupId];
  }

  function navigate() {
    flyoutGroup = null;
    onNavigate();
  }

  function dismissFlyout(event: PointerEvent) {
    if (!(event.target instanceof Element) || !event.target.closest('.nvr-sidebar-navigation')) {
      flyoutGroup = null;
    }
  }

  function handleEscape(event: KeyboardEvent) {
    if (event.key === 'Escape') flyoutGroup = null;
  }

  onMount(() => {
    const saved = getPreference<string[]>(preferenceKey, []);
    const valid = Array.isArray(saved)
      ? saved.filter((id) => NAV_GROUPS.some((group) => group.id === id))
      : [];
    const currentGroup = findNavigationSelection(activeRoute)?.groupId;
    expandedGroups = Array.from(new Set([
      ...(valid.length ? valid : currentGroup ? [currentGroup] : ['live']),
      ...(currentGroup ? [currentGroup] : []),
    ]));
    hydrated = true;
    document.addEventListener('pointerdown', dismissFlyout);
    window.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('pointerdown', dismissFlyout);
      window.removeEventListener('keydown', handleEscape);
    };
  });

  // A route change reveals the new page, without preventing users from manually
  // collapsing the current group. Group state never changes application routing.
  $effect(() => {
    const route = activeRoute;
    const groupId = findNavigationSelection(route)?.groupId;
    if (!hydrated || route === lastRoute) return;
    lastRoute = route;
    if (groupId && !expandedGroups.includes(groupId)) {
      expandedGroups = [...expandedGroups, groupId];
    }
    flyoutGroup = null;
  });

  $effect(() => {
    if (hydrated) setPreference(preferenceKey, expandedGroups);
  });

  $effect(() => {
    if (!compact) flyoutGroup = null;
  });
</script>

<nav class="sidebar-nav nvr-sidebar-navigation" aria-label={t('nav.v1.navigation')}>
  <a
    href={NAV_OVERVIEW.href}
    class="nav-link primary-link"
    class:active={selection?.itemId === NAV_OVERVIEW.id}
    aria-current={selection?.itemId === NAV_OVERVIEW.id ? 'page' : undefined}
    aria-label={compact ? t(NAV_OVERVIEW.labelKey) : undefined}
    title={compact ? t(NAV_OVERVIEW.labelKey) : undefined}
    onclick={navigate}
  >
    <LayoutDashboard size={20} />
    {#if !compact}<span class="nav-label">{t(NAV_OVERVIEW.labelKey)}</span>{/if}
  </a>

  <div class="nav-divider"></div>

  {#each NAV_GROUPS as group (group.id)}
    {@const GroupIcon = icons[group.icon] || Monitor}
    <div class="nav-group">
      <button
        type="button"
        class="nav-link nav-group-button"
        class:group-active={selection?.groupId === group.id}
        aria-label={compact ? t(group.labelKey) : undefined}
        aria-expanded={compact ? flyoutGroup === group.id : expandedGroups.includes(group.id)}
        aria-controls={compact ? 'nav-flyout-' + group.id : 'nav-submenu-' + group.id}
        title={compact ? t(group.labelKey) : undefined}
        onclick={(event) => toggleGroup(group.id, event)}
      >
        <GroupIcon size={20} />
        {#if !compact}
          <span class="nav-label">{t(group.labelKey)}</span>
          {#if expandedGroups.includes(group.id)}
            <ChevronDown size={16} class="nav-chevron" />
          {:else}
            <ChevronRight size={16} class="nav-chevron" />
          {/if}
        {/if}
      </button>
      {#if !compact && expandedGroups.includes(group.id)}
        <div class="nav-submenu" id={'nav-submenu-' + group.id}>
          {#each group.children as item (item.id)}
            {@const ItemIcon = icons[item.icon] || Monitor}
            <a
              href={item.href}
              class="nav-link nav-child"
              class:active={selection?.itemId === item.id}
              aria-current={selection?.itemId === item.id ? 'page' : undefined}
              onclick={navigate}
            >
              <ItemIcon size={16} />
              <span class="nav-label">{t(item.labelKey)}</span>
            </a>
          {/each}
        </div>
      {/if}
    </div>
  {/each}

  {#if compact && flyoutGroup}
    {@const flyout = NAV_GROUPS.find((item) => item.id === flyoutGroup)}
    {#if flyout}
      <div
        class="nav-flyout"
        id={'nav-flyout-' + flyout.id}
        style:top={flyoutTop + 'px'}
        aria-label={t(flyout.labelKey)}
      >
        <div class="flyout-title">{t(flyout.labelKey)}</div>
        {#each flyout.children as item (item.id)}
          {@const ItemIcon = icons[item.icon] || Monitor}
          <a
            href={item.href}
            class="nav-link nav-child"
            class:active={selection?.itemId === item.id}
            aria-current={selection?.itemId === item.id ? 'page' : undefined}
            onclick={navigate}
          >
            <ItemIcon size={16} />
            <span class="nav-label">{t(item.labelKey)}</span>
          </a>
        {/each}
      </div>
    {/if}
  {/if}
</nav>

<style>
  .sidebar-nav {
    flex: 1;
    overflow-y: auto;
    padding: 0.5rem;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-height: 0;
  }
  .nav-link {
    display: flex;
    align-items: center;
    width: 100%;
    gap: 0.75rem;
    min-height: 44px;
    padding: 0.65rem 0.75rem;
    border: 1px solid transparent;
    border-radius: var(--radius-md);
    background: transparent;
    color: var(--text-secondary);
    text-decoration: none;
    text-align: left;
    font-size: 0.875rem;
    font-weight: 500;
    white-space: nowrap;
    cursor: pointer;
    transition: background-color var(--duration-fast) var(--ease-out), color var(--duration-fast) var(--ease-out);
  }
  .nav-link:hover {
    color: var(--text-primary);
    background: var(--bg-tertiary);
  }
  .nav-link:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
  }
  .nav-link.active {
    color: #fff;
    background: var(--color-primary);
    border-color: var(--color-primary);
  }
  .nav-link.active:hover {
    background: var(--color-primary-hover, var(--color-primary));
  }
  .nav-group-button.group-active:not(.active) {
    color: var(--color-primary);
    background: var(--bg-tertiary);
  }
  .nav-label {
    min-width: 0;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .nav-chevron {
    flex-shrink: 0;
  }
  .nav-divider {
    height: 1px;
    margin: 0.45rem 0.5rem;
    background: var(--border);
  }
  .nav-submenu {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 2px 0 0.3rem 0.9rem;
  }
  .nav-child {
    min-height: 40px;
    padding: 0.5rem 0.65rem;
    font-size: 0.825rem;
  }
  .nav-flyout {
    position: fixed;
    z-index: 1250;
    left: 68px;
    width: 224px;
    padding: 0.45rem;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: var(--bg-elevated);
    box-shadow: var(--shadow-lg);
  }
  .flyout-title {
    padding: 0.4rem 0.65rem;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-muted);
  }
  :global(.sidebar.collapsed) .sidebar-nav {
    overflow: visible;
  }
  :global(.sidebar.collapsed) .nav-group-button,
  :global(.sidebar.collapsed) .primary-link {
    justify-content: center;
    padding: 0.65rem 0.5rem;
  }
  :global(.sidebar.collapsed) .nav-flyout .nav-child {
    justify-content: flex-start;
  }
  .sidebar-nav::-webkit-scrollbar { width: 4px; }
  .sidebar-nav::-webkit-scrollbar-thumb { background: var(--border); border-radius: 2px; }
  @media (max-width: 767px) {
    .sidebar-nav { overflow-y: auto !important; }
    .nav-flyout { display: none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .nav-link { transition: none; }
  }
</style>