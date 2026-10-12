<script lang="ts">
  import { onMount } from 'svelte';
  import { getEmbyConfig, listCameras, listEmbyItems, listIPTVChannels, listIPTVSources, listStreams } from '$lib/api';
  import { t } from '$lib/i18n';
  import { ArrowLeft, ArrowRight, Camera, Film, FolderOpen, Link2, Plus, Radio, RefreshCw, Tv, X } from 'lucide-svelte';
  import IPTV from './IPTV.svelte';
  import Emby from './Emby.svelte';

  type SourceTab = 'overview' | 'iptv' | 'emby';
  type SourceCounts = { iptvSources: number | null; iptvChannels: number | null; embyItems: number | null; devices: number | null; externalStreams: number | null };
  let { activeTab = 'overview' }: { activeTab?: SourceTab } = $props();

  let counts = $state<SourceCounts>({ iptvSources: null, iptvChannels: null, embyItems: null, devices: null, externalStreams: null });
  let embyConfigured = $state<boolean | null>(null);
  let failed = $state<Set<string>>(new Set());
  let loading = $state(false);
  let showWizard = $state(false);

  const options = [
    { id: 'camera', icon: Camera, title: 'mediaIngest.camera', description: 'mediaIngest.cameraHint', href: '#/devices?add=1' },
    { id: 'url', icon: Link2, title: 'mediaIngest.url', description: 'mediaIngest.urlHint', href: '#/streams?create=pull' },
    { id: 'iptv', icon: Tv, title: 'mediaIngest.iptv', description: 'mediaIngest.iptvHint', href: '#/media-sources?tab=iptv' },
    { id: 'emby', icon: Film, title: 'mediaIngest.emby', description: 'mediaIngest.embyHint', href: '#/media-sources?tab=emby' },
    { id: 'push', icon: Radio, title: 'mediaIngest.push', description: 'mediaIngest.pushHint', href: '#/streams?create=push' },
  ];

  async function refreshOverview() {
    loading = true;
    const nextFailed = new Set<string>();
    const results = await Promise.allSettled([
      listIPTVSources(),
      listIPTVChannels(),
      getEmbyConfig(),
      listEmbyItems(),
      listCameras(),
      listStreams({ managed: false, limit: 1 }),
    ] as const);

    const [sources, channels, config, embyItems, devices, streams] = results;
    if (sources.status === 'fulfilled') counts.iptvSources = sources.value.length;
    else nextFailed.add('iptv');
    if (channels.status === 'fulfilled') counts.iptvChannels = channels.value.length;
    else nextFailed.add('iptv');
    if (config.status === 'fulfilled') embyConfigured = config.value.configured;
    else { embyConfigured = null; nextFailed.add('emby'); }
    if (embyItems.status === 'fulfilled') counts.embyItems = embyItems.value.length;
    else nextFailed.add('emby');
    if (devices.status === 'fulfilled') counts.devices = devices.value.length;
    else nextFailed.add('devices');
    if (streams.status === 'fulfilled') counts.externalStreams = streams.value.total;
    else nextFailed.add('streams');
    failed = nextFailed;
    loading = false;
  }

  function openTab(tab: SourceTab) {
    window.location.hash = tab === 'overview' ? '#/media-sources' : `#/media-sources?tab=${tab}`;
  }

  function closeWizard() {
    showWizard = false;
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && showWizard) closeWizard();
  }

  onMount(() => {
    if (activeTab === 'overview') void refreshOverview();
    window.addEventListener('keydown', handleKeydown);
    return () => window.removeEventListener('keydown', handleKeydown);
  });
</script>

{#if activeTab === 'overview'}
  <main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-7">
    <div class="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-4 mb-7">
      <div>
        <p class="text-xs font-semibold uppercase tracking-[0.16em] th-text-muted mb-2">{t('mediaIngest.kicker')}</p>
        <h1 class="text-2xl font-bold th-text-primary">{t('mediaIngest.title')}</h1>
        <p class="text-sm th-text-secondary mt-2 max-w-2xl">{t('mediaIngest.subtitle')}</p>
      </div>
      <div class="flex gap-2 shrink-0">
        <button class="btn btn-secondary btn-sm" onclick={() => void refreshOverview()} disabled={loading}>
          <RefreshCw size={15} class={loading ? 'spin' : ''} />
          {t('common.refresh')}
        </button>
        <button class="btn btn-primary btn-sm" onclick={() => showWizard = true}>
          <Plus size={16} />
          {t('mediaIngest.addSource')}
        </button>
      </div>
    </div>

    <section class="card border th-border overflow-hidden" aria-label={t('mediaIngest.inventory')}>
      <div class="flex items-center justify-between px-5 py-4 border-b th-border">
        <div>
          <h2 class="text-base font-semibold th-text-primary">{t('mediaIngest.inventory')}</h2>
          <p class="text-xs th-text-muted mt-1">{t('mediaIngest.inventoryHint')}</p>
        </div>
        {#if loading}
          <span class="text-xs th-text-muted" aria-live="polite">{t('common.loading')}</span>
        {/if}
      </div>

      <div class="divide-y th-border">
        <a href="#/devices" class="group flex items-center gap-4 px-5 py-4 hover:th-bg-secondary transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px]">
          <span class="w-10 h-10 rounded-lg th-bg-secondary flex items-center justify-center th-text-primary"><Camera size={19} /></span>
          <span class="min-w-0 flex-1">
            <span class="block font-medium th-text-primary">{t('mediaIngest.devices')}</span>
            <span class="block text-sm th-text-muted mt-0.5">{failed.has('devices') ? t('mediaIngest.unavailable') : t('mediaIngest.deviceCount', { count: counts.devices === null ? '—' : String(counts.devices) })}</span>
          </span>
          <span class="text-sm th-text-secondary">{t('mediaIngest.manageDevices')}</span><ArrowRight size={16} class="th-text-muted group-hover:translate-x-0.5 transition-transform" />
        </a>

        <a href="#/media-sources?tab=iptv" class="group flex items-center gap-4 px-5 py-4 hover:th-bg-secondary transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px]">
          <span class="w-10 h-10 rounded-lg th-bg-secondary flex items-center justify-center th-text-primary"><Tv size={19} /></span>
          <span class="min-w-0 flex-1">
            <span class="block font-medium th-text-primary">{t('mediaIngest.iptv')}</span>
            <span class="block text-sm th-text-muted mt-0.5">{failed.has('iptv') ? t('mediaIngest.unavailable') : t('mediaIngest.iptvCounts', { sources: counts.iptvSources === null ? '—' : String(counts.iptvSources), channels: counts.iptvChannels === null ? '—' : String(counts.iptvChannels) })}</span>
          </span>
          <span class="text-sm th-text-secondary">{t('mediaIngest.manage')}</span><ArrowRight size={16} class="th-text-muted group-hover:translate-x-0.5 transition-transform" />
        </a>

        <a href="#/media-sources?tab=emby" class="group flex items-center gap-4 px-5 py-4 hover:th-bg-secondary transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px]">
          <span class="w-10 h-10 rounded-lg th-bg-secondary flex items-center justify-center th-text-primary"><Film size={19} /></span>
          <span class="min-w-0 flex-1">
            <span class="block font-medium th-text-primary">{t('mediaIngest.emby')}</span>
            <span class="block text-sm th-text-muted mt-0.5">{failed.has('emby') ? t('mediaIngest.unavailable') : embyConfigured ? t('mediaIngest.embyReady', { count: counts.embyItems === null ? '—' : String(counts.embyItems) }) : t('mediaIngest.embyNotConfigured')}</span>
          </span>
          <span class="text-sm th-text-secondary">{t('mediaIngest.manage')}</span><ArrowRight size={16} class="th-text-muted group-hover:translate-x-0.5 transition-transform" />
        </a>

        <a href="#/streams" class="group flex items-center gap-4 px-5 py-4 hover:th-bg-secondary transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px]">
          <span class="w-10 h-10 rounded-lg th-bg-secondary flex items-center justify-center th-text-primary"><Radio size={19} /></span>
          <span class="min-w-0 flex-1">
            <span class="block font-medium th-text-primary">{t('mediaIngest.externalStreams')}</span>
            <span class="block text-sm th-text-muted mt-0.5">{failed.has('streams') ? t('mediaIngest.unavailable') : t('mediaIngest.externalCount', { count: counts.externalStreams === null ? '—' : String(counts.externalStreams) })}</span>
          </span>
          <span class="text-sm th-text-secondary">{t('mediaIngest.manageStreams')}</span><ArrowRight size={16} class="th-text-muted group-hover:translate-x-0.5 transition-transform" />
        </a>
      </div>
    </section>

    <div class="grid md:grid-cols-2 gap-4 mt-5">
      <div class="rounded-lg border th-border p-4 flex gap-3">
        <FolderOpen size={18} class="th-text-muted shrink-0 mt-0.5" />
        <p class="text-sm th-text-secondary">{t('mediaIngest.modelHint')}</p>
      </div>
      <a href="#/streams" class="rounded-lg border th-border p-4 flex items-center gap-3 hover:th-bg-secondary transition-colors">
        <Radio size={18} class="th-text-muted shrink-0" />
        <span class="text-sm th-text-secondary flex-1">{t('mediaIngest.runtimeHint')}</span>
        <ArrowRight size={16} class="th-text-muted" />
      </a>
    </div>
  </main>

  {#if showWizard}
    <div class="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4" role="presentation" onclick={(event) => { if (event.target === event.currentTarget) closeWizard(); }}>
      <dialog open class="w-full max-w-2xl max-h-[90vh] overflow-y-auto rounded-xl border th-border th-bg-primary shadow-xl" aria-labelledby="source-wizard-title">
        <div class="flex items-start justify-between gap-4 px-5 py-4 border-b th-border">
          <div>
            <h2 id="source-wizard-title" class="text-lg font-semibold th-text-primary">{t('mediaIngest.wizardTitle')}</h2>
            <p class="text-sm th-text-muted mt-1">{t('mediaIngest.wizardHint')}</p>
          </div>
          <button class="btn btn-ghost btn-sm" onclick={closeWizard} aria-label={t('mediaIngest.close')}><X size={18} /></button>
        </div>
        <div class="divide-y th-border">
          {#each options as option (option.id)}
            <a href={option.href} onclick={closeWizard} class="flex items-center gap-4 px-5 py-4 hover:th-bg-secondary transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-inset">
              <span class="w-10 h-10 rounded-lg th-bg-secondary flex items-center justify-center th-text-primary"><option.icon size={19} /></span>
              <span class="min-w-0 flex-1"><span class="block font-medium th-text-primary">{t(option.title)}</span><span class="block text-sm th-text-muted mt-0.5">{t(option.description)}</span></span>
              <ArrowRight size={16} class="th-text-muted" />
            </a>
          {/each}
        </div>
        <div class="px-5 py-4 border-t th-border">
          <p class="text-xs font-semibold th-text-muted mb-2">{t('mediaIngest.planned')}</p>
          <div class="flex flex-wrap gap-2">
            <span class="badge badge-secondary">{t('mediaIngest.jellyfin')}</span>
            <span class="badge badge-secondary">{t('mediaIngest.plex')}</span>
            <span class="badge badge-secondary">{t('mediaIngest.localFiles')}</span>
          </div>
          <p class="text-xs th-text-muted mt-2">{t('mediaIngest.plannedHint')}</p>
        </div>
      </dialog>
    </div>
  {/if}
{:else}
  <main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 pt-5">
    <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-5">
      <button class="btn btn-ghost btn-sm self-start" onclick={() => openTab('overview')}><ArrowLeft size={16} />{t('mediaIngest.backToOverview')}</button>
      <nav class="flex items-center gap-1 p-1 rounded-lg th-bg-secondary" aria-label={t('mediaIngest.sourceTypes')}>
        <button class={`px-3 py-1.5 rounded-md text-sm transition-colors ${activeTab === 'iptv' ? 'th-bg-primary th-text-primary shadow-sm' : 'th-text-secondary hover:th-text-primary'}`} onclick={() => openTab('iptv')}>{t('nav.iptv')}</button>
        <button class={`px-3 py-1.5 rounded-md text-sm transition-colors ${activeTab === 'emby' ? 'th-bg-primary th-text-primary shadow-sm' : 'th-text-secondary hover:th-text-primary'}`} onclick={() => openTab('emby')}>{t('nav.emby')}</button>
      </nav>
    </div>
  </main>
  {#if activeTab === 'emby'}<Emby />{:else}<IPTV />{/if}
{/if}
