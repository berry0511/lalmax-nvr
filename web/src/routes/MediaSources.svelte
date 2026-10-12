<script lang="ts">
  import { onMount } from 'svelte';
  import { getEmbyConfig, listCameras, listEmbyItems, listIPTVChannels, listIPTVSources, listStreams } from '$lib/api';
  import { t } from '$lib/i18n';
  import { ArrowLeft, ArrowRight, Camera, Check, Film, Link2, Plus, Radio, RefreshCw, Tv } from 'lucide-svelte';
  import IPTV from './IPTV.svelte';
  import Emby from './Emby.svelte';
  import MediaSourceSetupDialog from '$lib/components/MediaSourceSetupDialog.svelte';

  type SourceTab = 'overview' | 'iptv' | 'emby';
  type SourceCounts = { iptvSources: number | null; iptvChannels: number | null; embyItems: number | null; devices: number | null; externalStreams: number | null };
  let { activeTab = 'overview' }: { activeTab?: SourceTab } = $props();

  let counts = $state<SourceCounts>({ iptvSources: null, iptvChannels: null, embyItems: null, devices: null, externalStreams: null });
  let embyConfigured = $state<boolean | null>(null);
  let failed = $state<Set<string>>(new Set());
  let loading = $state(false);
  let showWizard = $state(false);
  let initialSource = $state<'iptv' | 'emby' | null>(null);

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

  onMount(() => {
    if (activeTab === 'overview') void refreshOverview();
  });
</script>

{#if activeTab === 'overview'}
  <main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
    <header class="relative overflow-hidden rounded-2xl border th-border th-bg-secondary p-5 sm:p-7 mb-6">
      <div class="absolute -right-10 -top-16 w-56 h-56 rounded-full border th-border opacity-50 pointer-events-none"></div>
      <div class="absolute -right-1 -top-7 w-36 h-36 rounded-full border th-border opacity-40 pointer-events-none"></div>
      <div class="relative flex flex-col lg:flex-row lg:items-end lg:justify-between gap-5">
        <div class="max-w-2xl">
          <div class="flex items-center gap-2 mb-3">
            <span class="inline-flex items-center gap-1.5 text-xs font-semibold th-text-secondary"><Link2 size={14} />{t('mediaIngest.kicker')}</span>
            {#if loading}<span class="badge badge-secondary" aria-live="polite">{t('common.loading')}</span>{/if}
          </div>
          <h1 class="text-2xl sm:text-3xl font-bold th-text-primary">{t('mediaIngest.title')}</h1>
          <p class="text-sm sm:text-base th-text-secondary mt-2 leading-relaxed">{t('mediaIngest.subtitle')}</p>
        </div>
        <div class="flex gap-2 shrink-0">
          <button class="btn btn-secondary" onclick={() => void refreshOverview()} disabled={loading} aria-label={t('common.refresh')}>
            <RefreshCw size={16} class={loading ? 'spin' : ''} />
            <span class="hidden sm:inline">{t('common.refresh')}</span>
          </button>
          <button class="btn btn-primary" onclick={() => showWizard = true}>
            <Plus size={17} />{t('mediaIngest.addSource')}
          </button>
        </div>
      </div>
    </header>

    <section aria-label={t('mediaIngest.inventory')}>
      <div class="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-2 mb-4">
        <div>
          <h2 class="text-lg font-semibold th-text-primary">{t('mediaIngest.inventory')}</h2>
          <p class="text-sm th-text-muted mt-1">{t('mediaIngest.inventoryHint')}</p>
        </div>
      </div>

      <div class="grid sm:grid-cols-2 gap-3">
        <a href="#/devices" class="group rounded-xl border th-border th-bg-primary p-4 sm:p-5 hover:th-bg-secondary hover:-translate-y-0.5 transition duration-200 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-primary)]">
          <div class="flex items-start justify-between gap-3">
            <span class="w-11 h-11 rounded-xl th-bg-secondary flex items-center justify-center th-text-primary"><Camera size={20} /></span>
            {#if failed.has('devices')}<span class="badge badge-warning">{t('mediaIngest.unavailable')}</span>{:else if counts.devices === null}<span class="badge badge-secondary">{t('common.loading')}</span>{:else if counts.devices === 0}<span class="badge badge-secondary">{t('mediaIngest.needsSetup')}</span>{:else}<span class="badge badge-success"><Check size={12} />{t('mediaIngest.connected')}</span>{/if}
          </div>
          <div class="mt-4 flex items-end justify-between gap-3">
            <div class="min-w-0">
              <h3 class="font-semibold th-text-primary">{t('mediaIngest.devices')}</h3>
              <p class="text-sm th-text-muted mt-1 leading-relaxed">{t('mediaIngest.devicesDescription')}</p>
            </div>
            <span class="text-2xl font-semibold tabular-nums th-text-primary shrink-0">{counts.devices === null ? '—' : counts.devices}</span>
          </div>
          <div class="mt-4 pt-3 border-t th-border flex items-center justify-between text-sm">
            <span class="th-text-muted">{t('mediaIngest.managedDevices')}</span>
            <span class="inline-flex items-center gap-1.5 font-medium th-text-secondary group-hover:th-text-primary">{t('mediaIngest.manageDevices')}<ArrowRight size={15} class="group-hover:translate-x-0.5 transition-transform" /></span>
          </div>
        </a>

        <article class="group rounded-xl border th-border th-bg-primary p-4 sm:p-5">
          <div class="flex items-start justify-between gap-3">
            <span class="w-11 h-11 rounded-xl th-bg-secondary flex items-center justify-center th-text-primary"><Tv size={20} /></span>
            {#if failed.has('iptv')}<span class="badge badge-warning">{t('mediaIngest.unavailable')}</span>{:else if counts.iptvSources === null}<span class="badge badge-secondary">{t('common.loading')}</span>{:else if counts.iptvSources === 0}<span class="badge badge-secondary">{t('mediaIngest.needsSetup')}</span>{:else}<span class="badge badge-success"><Check size={12} />{t('mediaIngest.connected')}</span>{/if}
          </div>
          <div class="mt-4 flex items-end justify-between gap-3">
            <div class="min-w-0">
              <h3 class="font-semibold th-text-primary">{t('mediaIngest.iptv')}</h3>
              <p class="text-sm th-text-muted mt-1 leading-relaxed">{t('mediaIngest.iptvDescription')}</p>
            </div>
            <span class="text-2xl font-semibold tabular-nums th-text-primary shrink-0">{counts.iptvSources === null ? '—' : counts.iptvSources}</span>
          </div>
          <div class="mt-4 pt-3 border-t th-border flex items-center justify-between text-sm">
            <span class="th-text-muted">{t('mediaIngest.iptvChannelCount', { count: counts.iptvChannels === null ? '—' : String(counts.iptvChannels) })}</span>
            <div class="inline-flex items-center gap-3">
              <button class="inline-flex items-center gap-1.5 font-medium th-text-secondary hover:th-text-primary" onclick={() => { initialSource = 'iptv'; showWizard = true; }}>{t('mediaIngest.configure')}<ArrowRight size={15} /></button>
              <a href="#/media-sources?tab=iptv" class="text-xs th-text-muted hover:th-text-primary">{t('mediaIngest.manage')}</a>
            </div>
          </div>
        </article>

        <article class="group rounded-xl border th-border th-bg-primary p-4 sm:p-5">
          <div class="flex items-start justify-between gap-3">
            <span class="w-11 h-11 rounded-xl th-bg-secondary flex items-center justify-center th-text-primary"><Film size={20} /></span>
            {#if failed.has('emby')}<span class="badge badge-warning">{t('mediaIngest.unavailable')}</span>{:else if embyConfigured === null}<span class="badge badge-secondary">{t('common.loading')}</span>{:else if embyConfigured}<span class="badge badge-success"><Check size={12} />{t('mediaIngest.connected')}</span>{:else}<span class="badge badge-secondary">{t('mediaIngest.needsSetup')}</span>{/if}
          </div>
          <div class="mt-4 flex items-end justify-between gap-3">
            <div class="min-w-0">
              <h3 class="font-semibold th-text-primary">{t('mediaIngest.emby')}</h3>
              <p class="text-sm th-text-muted mt-1 leading-relaxed">{t('mediaIngest.embyDescription')}</p>
            </div>
            <span class="text-2xl font-semibold tabular-nums th-text-primary shrink-0">{counts.embyItems === null ? '—' : counts.embyItems}</span>
          </div>
          <div class="mt-4 pt-3 border-t th-border flex items-center justify-between text-sm">
            <span class="th-text-muted">{t('mediaIngest.managedMediaItems')}</span>
            <div class="inline-flex items-center gap-3">
              <button class="inline-flex items-center gap-1.5 font-medium th-text-secondary hover:th-text-primary" onclick={() => { initialSource = 'emby'; showWizard = true; }}>{t('mediaIngest.configure')}<ArrowRight size={15} /></button>
              <a href="#/media-sources?tab=emby" class="text-xs th-text-muted hover:th-text-primary">{t('mediaIngest.manage')}</a>
            </div>
          </div>
        </article>

        <a href="#/streams" class="group rounded-xl border th-border th-bg-primary p-4 sm:p-5 hover:th-bg-secondary hover:-translate-y-0.5 transition duration-200 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-primary)]">
          <div class="flex items-start justify-between gap-3">
            <span class="w-11 h-11 rounded-xl th-bg-secondary flex items-center justify-center th-text-primary"><Radio size={20} /></span>
            {#if failed.has('streams')}<span class="badge badge-warning">{t('mediaIngest.unavailable')}</span>{:else if counts.externalStreams === null}<span class="badge badge-secondary">{t('common.loading')}</span>{:else if counts.externalStreams === 0}<span class="badge badge-secondary">{t('mediaIngest.noStreams')}</span>{:else}<span class="badge badge-success"><Check size={12} />{t('mediaIngest.available')}</span>{/if}
          </div>
          <div class="mt-4 flex items-end justify-between gap-3">
            <div class="min-w-0">
              <h3 class="font-semibold th-text-primary">{t('mediaIngest.externalStreams')}</h3>
              <p class="text-sm th-text-muted mt-1 leading-relaxed">{t('mediaIngest.streamsDescription')}</p>
            </div>
            <span class="text-2xl font-semibold tabular-nums th-text-primary shrink-0">{counts.externalStreams === null ? '—' : counts.externalStreams}</span>
          </div>
          <div class="mt-4 pt-3 border-t th-border flex items-center justify-between text-sm">
            <span class="th-text-muted">{t('mediaIngest.managedStreams')}</span>
            <span class="inline-flex items-center gap-1.5 font-medium th-text-secondary group-hover:th-text-primary">{t('mediaIngest.manageStreams')}<ArrowRight size={15} class="group-hover:translate-x-0.5 transition-transform" /></span>
          </div>
        </a>
      </div>
    </section>

    <section class="mt-6 rounded-xl border th-border th-bg-secondary p-4 sm:p-5" aria-label={t('mediaIngest.workflowTitle')}>
      <div class="mb-4">
        <h2 class="text-base font-semibold th-text-primary">{t('mediaIngest.workflowTitle')}</h2>
        <p class="text-sm th-text-muted mt-1">{t('mediaIngest.workflowHint')}</p>
      </div>
      <div class="grid sm:grid-cols-3 gap-3">
        <div class="flex items-start gap-3 p-3 rounded-lg th-bg-primary border th-border">
          <span class="w-7 h-7 rounded-full th-bg-secondary flex items-center justify-center text-xs font-semibold th-text-primary shrink-0">1</span>
          <div><h3 class="text-sm font-medium th-text-primary">{t('mediaIngest.stepSource')}</h3><p class="text-xs th-text-muted mt-1">{t('mediaIngest.stepSourceHint')}</p></div>
        </div>
        <div class="flex items-start gap-3 p-3 rounded-lg th-bg-primary border th-border">
          <span class="w-7 h-7 rounded-full th-bg-secondary flex items-center justify-center text-xs font-semibold th-text-primary shrink-0">2</span>
          <div><h3 class="text-sm font-medium th-text-primary">{t('mediaIngest.stepContent')}</h3><p class="text-xs th-text-muted mt-1">{t('mediaIngest.stepContentHint')}</p></div>
        </div>
        <a href="#/streams" class="group flex items-start gap-3 p-3 rounded-lg th-bg-primary border th-border hover:th-border-hover transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-primary)]">
          <span class="w-7 h-7 rounded-full th-bg-secondary flex items-center justify-center text-xs font-semibold th-text-primary shrink-0">3</span>
          <div class="flex-1"><h3 class="text-sm font-medium th-text-primary">{t('mediaIngest.stepPublish')}</h3><p class="text-xs th-text-muted mt-1">{t('mediaIngest.stepPublishHint')}</p></div>
          <ArrowRight size={15} class="th-text-muted group-hover:translate-x-0.5 transition-transform mt-1" />
        </a>
      </div>
    </section>
  </main>

  {#if showWizard}
    <MediaSourceSetupDialog initialSource={initialSource} onclose={() => { showWizard = false; initialSource = null; }} onsaved={() => void refreshOverview()} />
  {/if}
{:else}
  <main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 pt-5">
    <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-5">
      <button class="btn btn-ghost btn-sm self-start" onclick={() => openTab('overview')}><ArrowLeft size={16} />{t('mediaIngest.backToOverview')}</button>
      <nav class="flex items-center gap-1 p-1 rounded-xl th-bg-secondary border th-border" aria-label={t('mediaIngest.sourceTypes')}>
        <button aria-current={activeTab === 'iptv' ? 'page' : undefined} class={`min-h-10 px-4 rounded-lg text-sm font-medium transition-colors ${activeTab === 'iptv' ? 'th-bg-primary th-text-primary shadow-sm' : 'th-text-secondary hover:th-text-primary'}`} onclick={() => openTab('iptv')}>{t('nav.iptv')}</button>
        <button aria-current={activeTab === 'emby' ? 'page' : undefined} class={`min-h-10 px-4 rounded-lg text-sm font-medium transition-colors ${activeTab === 'emby' ? 'th-bg-primary th-text-primary shadow-sm' : 'th-text-secondary hover:th-text-primary'}`} onclick={() => openTab('emby')}>{t('nav.emby')}</button>
      </nav>
    </div>
  </main>
  {#if activeTab === 'emby'}<Emby />{:else}<IPTV />{/if}
{/if}
