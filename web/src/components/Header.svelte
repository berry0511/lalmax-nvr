<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { t } from '$lib/i18n';
  import { logout } from '$lib/api';
  import { getEffectiveTheme, getPreference, setPreference } from '$lib/preferences';
  import SidebarNav from './SidebarNav.svelte';
  import LanguageSwitcher from './LanguageSwitcher.svelte';
  import ThemeToggle from './ThemeToggle.svelte';
  import Toast from './Toast.svelte';
  import { ArrowLeft, Menu, LogOut, ChevronLeft, ChevronRight, X } from 'lucide-svelte';

  // Props
  let {
    activeRoute = '',
    showBack = false,
    backLabel = ''
  }: {
    activeRoute?: string;
    showBack?: boolean;
    backLabel?: string;
  } = $props();

  // Sidebar state
  let sidebarCollapsed = $state(false);
  let mobileMenuOpen = $state(false);

  function syncSidebarWidth() {
    document.documentElement.style.setProperty('--nvr-sidebar-width', sidebarCollapsed ? '64px' : '240px');
  }

  function toggleSidebar() {
    sidebarCollapsed = !sidebarCollapsed;
    setPreference('sidebar_collapsed', sidebarCollapsed);
    syncSidebarWidth();
  }

  function toggleMobileMenu() {
    mobileMenuOpen = !mobileMenuOpen;
  }

  function closeMobileMenu() {
    mobileMenuOpen = false;
  }

  function handleEscape(event: KeyboardEvent) {
    if (event.key === 'Escape') closeMobileMenu();
  }

  function handleNavClick() {
    closeMobileMenu();
  }

  // Hash change listener to keep activeRoute in sync
  function handleHashChange() {
    const hash = window.location.hash.replace('#', '') || '/dashboard';
    activeRoute = hash;
  }

  onMount(() => {
    // Sync theme
    const effectiveTheme = getEffectiveTheme();
    document.documentElement.setAttribute('data-theme', effectiveTheme);

    sidebarCollapsed = getPreference<boolean>('sidebar_collapsed', false);
    syncSidebarWidth();
    // Sync active route from current hash
    handleHashChange();
    window.addEventListener('hashchange', handleHashChange);
    window.addEventListener('keydown', handleEscape);
  });

  onDestroy(() => {
    window.removeEventListener('hashchange', handleHashChange);
    window.removeEventListener('keydown', handleEscape);
    document.documentElement.style.removeProperty('--nvr-sidebar-width');
  });

  function goBack() {
    window.history.back();
  }
</script>

<!-- Top Header Bar -->
<header class="top-header">
  <div class="top-header-inner">
    <div class="top-header-left">
      <!-- Mobile menu button -->
      <button class="mobile-menu-btn md:hidden" onclick={toggleMobileMenu}>
        {#if mobileMenuOpen}
          <X size={20} />
        {:else}
          <Menu size={20} />
        {/if}
      </button>
      
      {#if showBack}
        <button class="back-btn" onclick={goBack}>
          <ArrowLeft size={20} />
          <span>{backLabel || t('detail.back')}</span>
        </button>
      {/if}
      <a href="#/dashboard" class="logo">
        <span class="logo-text">lalmax-nvr</span>
      </a>
    </div>
    
    <div class="top-header-right">
      <ThemeToggle />
      <LanguageSwitcher />
      <button class="btn btn-ghost logout-btn" onclick={logout}>
        <LogOut size={18} />
        <span class="logout-text">{t('nav.logout')}</span>
      </button>
    </div>
  </div>
</header>

<!-- Mobile Menu Overlay -->
{#if mobileMenuOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="mobile-overlay md:hidden" onclick={closeMobileMenu} role="button" tabindex="-1" aria-label="Close menu"></div>
{/if}

<!-- Sidebar Navigation -->
<aside class="sidebar" class:collapsed={sidebarCollapsed} class:mobile-open={mobileMenuOpen}>
  <!-- Collapse toggle button (desktop only) -->
  <button class="sidebar-toggle hidden md:flex" onclick={toggleSidebar}>
    {#if sidebarCollapsed}
      <ChevronRight size={16} />
    {:else}
      <ChevronLeft size={16} />
    {/if}
  </button>
  
  <SidebarNav activeRoute={activeRoute} collapsed={sidebarCollapsed} mobileOpen={mobileMenuOpen} onNavigate={handleNavClick} />
</aside>

<!-- Toast container -->
<Toast />

<style>
  /* Top Header */
  .top-header {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 1100;
    height: 56px;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
    box-shadow: var(--shadow-sm);
  }

  .top-header-inner {
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 1rem;
  }

  .top-header-left {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .top-header-right {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .logo {
    text-decoration: none;
    white-space: nowrap;
  }

  .logo-text {
    font-size: 1.25rem;
    font-weight: 700;
    letter-spacing: -0.025em;
    background: linear-gradient(135deg, #8b5cf6 0%, #a78bfa 40%, #38bdf8 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .back-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
    color: var(--text-secondary);
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.875rem;
    font-weight: 500;
    padding: 0.375rem 0.625rem;
    border-radius: var(--radius-sm);
    transition: all var(--duration-fast) var(--ease-out);
  }

  .back-btn:hover {
    color: var(--text-primary);
    background-color: var(--bg-tertiary);
  }

  .logout-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
  }

  .logout-text {
    display: none;
  }

  @media (min-width: 640px) {
    .logout-text {
      display: inline;
    }
  }

  /* Mobile menu button */
  .mobile-menu-btn {
    display: flex;
    background: none;
    border: none;
    color: var(--text-primary);
    cursor: pointer;
    padding: 0.5rem;
    border-radius: var(--radius-sm);
    transition: background-color var(--duration-fast) var(--ease-out);
  }

  .mobile-menu-btn:hover {
    background-color: var(--bg-tertiary);
  }

  @media (min-width: 768px) {
    .mobile-menu-btn {
      display: none;
    }
  }

  /* Mobile overlay */
  .mobile-overlay {
    position: fixed;
    inset: 0;
    z-index: 1190;
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px);
  }

  /* Sidebar */
  .sidebar {
    position: fixed;
    top: 56px;
    left: 0;
    bottom: 0;
    z-index: 1200;
    width: 240px;
    background: var(--bg-elevated);
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    transition: width var(--duration-normal) var(--ease-out);
    overflow: visible;
  }

  .sidebar.collapsed {
    width: 64px;
  }

  /* Mobile sidebar */
  @media (max-width: 767px) {
    .sidebar {
      transform: translateX(-100%);
      box-shadow: var(--shadow-lg);
      overflow: hidden;
      width: 240px;
    }

    .sidebar.mobile-open {
      transform: translateX(0);
      width: 240px;
    }

    .sidebar.collapsed { width: 240px; }
  }

  /* Desktop sidebar */
  @media (min-width: 768px) {
    .sidebar {
      transform: none !important;
    }
  }

  /* Sidebar toggle button */
  .sidebar-toggle {
    position: absolute;
    right: -12px;
    top: 12px;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all var(--duration-fast) var(--ease-out);
    z-index: 10;
  }

  .sidebar-toggle:hover {
    background: var(--color-primary);
    color: white;
    border-color: var(--color-primary);
  }

</style>
