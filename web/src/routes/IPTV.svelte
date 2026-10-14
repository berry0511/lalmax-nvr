<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    commitIPTVImport,
    checkIPTVChannel,
    createIPTVImport,
    deleteIPTVChannel,
    deleteIPTVSource,
    getStream,
    getIPTVChannelPlayback,
    getIPTVImport,
    listIPTVChannels,
    listIPTVImportItems,
    listIPTVSources,
    refreshIPTVSource,
    updateIPTVChannel,
    updateIPTVSource,
  } from '$lib/api';
  import type { IPTVChannel, IPTVImportItem, IPTVImportJob, IPTVPlaybackDetails, IPTVSource } from '$lib/api';
  import { t } from '$lib/i18n';
  import { showToast } from '$lib/toast';
  import VideoPlayer from '../components/VideoPlayer.svelte';
  import {
    CalendarClock,
    Check,
    CircleAlert,
    CircleCheck,
    Copy,
    ExternalLink,
    Eye,
    EyeOff,
    ListVideo,
    LoaderCircle,
    Plus,
    Radio,
    RefreshCw,
    Search,
    Star,
    Trash2,
    Tv,
    Upload,
    X,
  } from 'lucide-svelte';

  type WorkspaceTab = 'channels' | 'sources';
  let tab = $state<WorkspaceTab>('channels');
  let loading = $state(true);
  let importing = $state(false);
  let committing = $state(false);
  let refreshing = $state<Record<string, boolean>>({});
  let savingSource = $state<Record<string, boolean>>({});
  let visibleSourceURLs = $state<Record<string, boolean>>({});
  let error = $state('');
  let playlistURL = $state('');
  let playlistText = $state('');
  let requestHeadersText = $state('');
  let importName = $state('');
  let job = $state<IPTVImportJob | null>(null);
  let items = $state<IPTVImportItem[]>([]);
  let selected = $state<Set<string>>(new Set());
  let selectionTouched = $state(false);
  let enrolledExternalIDs = $state<Set<string>>(new Set());
  let sources = $state<IPTVSource[]>([]);
  let channels = $state<IPTVChannel[]>([]);
  let allChannels = $state<IPTVChannel[]>([]);
  let groupFilter = $state('');
  let membershipFilter = $state<'all' | 'available' | 'joined'>('all');
  let sourceFilter = $state('');
  let search = $state('');
  let showFavorites = $state(false);
  let showImportForm = $state(false);
  let active = $state<(IPTVChannel & { playback: IPTVPlaybackDetails }) | null>(null);
  let publishBusy = $state<Record<string, boolean>>({});
  let checkBusy = $state<Record<string, boolean>>({});
  let pollTimer: number | undefined;

  let selectableItems = $derived(items.filter((item) => item.playable || item.status === 'warning' || item.status === 'playable'));
  let pendingProbeCount = $derived(items.filter((item) => item.status === 'pending' || item.status === 'probing').length);
  let completedProbeCount = $derived(Math.max(0, Math.min(job?.total_items ?? items.length, items.length - pendingProbeCount)));
  let playableProbeCount = $derived(selectableItems.filter((item) => !item.drm && item.status !== 'unsupported' && item.status !== 'failed').length);
  let failedProbeCount = $derived(items.filter((item) => item.status === 'failed').length);
  let unsupportedProbeCount = $derived(items.filter((item) => item.status === 'unsupported').length);
  let probeProgress = $derived(job?.total_items ? Math.min(100, Math.round((completedProbeCount / job.total_items) * 100)) : 0);
  let probeActive = $derived(job?.status === 'probing' || pendingProbeCount > 0);
  let playableCount = $derived(allChannels.filter((ch) => ch.playable && ch.probe_status === 'playable').length);
  let attentionCount = $derived(allChannels.filter((ch) => !ch.playable || ch.probe_status === 'warning' || ch.probe_status === 'failed').length);
  let favoriteCount = $derived(allChannels.filter((ch) => ch.favorite).length);
  let visibleChannels = $derived(channels.filter((ch) => ch.playable && (membershipFilter === 'all' || (membershipFilter === 'joined' ? ch.publish_enabled : !ch.publish_enabled))));
  let groups = $derived([...new Set(allChannels.filter((ch) => ch.playable).map((ch) => ch.group_name).filter((group): group is string => Boolean(group)))].sort((a, b) => a.localeCompare(b)));

  function isSelectable(item: IPTVImportItem) {
    return !item.drm && item.status !== 'unsupported' && item.status !== 'failed' && (item.playable || item.status === 'warning' || item.status === 'playable');
  }

  async function loadSources() {
    sources = await listIPTVSources();
  }

  async function loadManaged() {
    const [src, ch, all] = await Promise.all([
      listIPTVSources(),
      listIPTVChannels({ source_id: sourceFilter || undefined, group: groupFilter || undefined, q: search || undefined, favorite: showFavorites ? true : undefined }),
      listIPTVChannels(),
    ]);
    sources = src;
    channels = ch;
    allChannels = all;
  }

  async function refreshImport() {
    if (!job) return;
    job = await getIPTVImport(job.id);
    const res = await listIPTVImportItems(job.id);
    items = res.items;
    if (!selectionTouched) {
      selected = new Set(items.filter((item) => isSelectable(item) && !enrolledExternalIDs.has(item.external_id)).map((item) => item.id));
    }
  }

  async function submitImport() {
    importing = true;
    error = '';
    try {
      let headers: Record<string, string> | undefined;
      if (requestHeadersText.trim()) {
        const parsed: unknown = JSON.parse(requestHeadersText);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || Object.values(parsed).some((value) => typeof value !== 'string')) {
          throw new Error(t('iptv.invalidHeaders'));
        }
        headers = parsed as Record<string, string>;
      }
      job = await createIPTVImport({
        name: importName.trim() || undefined,
        playlist_url: playlistText.trim() ? undefined : playlistURL.trim(),
        playlist_text: playlistText.trim() || undefined,
        headers,
      });
      selected = new Set();
      selectionTouched = false;
      enrolledExternalIDs = new Set();
      items = [];
      await refreshImport();
      tab = 'sources';
      startPolling();
    } catch (e) {
      error = e instanceof Error ? e.message : t('iptv.importFailed');
    } finally {
      importing = false;
    }
  }

  function startPolling() {
    stopPolling();
    pollTimer = window.setInterval(async () => {
      if (job && !['ready', 'failed', 'committed'].includes(job.status)) {
        await refreshImport().catch(() => undefined);
      }
      await loadSources().catch(() => undefined);
      if ((!job || ['ready', 'failed', 'committed'].includes(job.status)) && !sources.some((source) => source.refresh_status === 'running')) stopPolling();
    }, 2000);
  }

  function stopPolling() {
    if (pollTimer) {
      window.clearInterval(pollTimer);
      pollTimer = undefined;
    }
  }

  function toggleItem(id: string, item: IPTVImportItem) {
    if (!isSelectable(item) || enrolledExternalIDs.has(item.external_id)) return;
    selectionTouched = true;
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selected = next;
  }

  function selectPlayable() {
    selectionTouched = true;
    selected = new Set(selectableItems.filter((item) => isSelectable(item) && !enrolledExternalIDs.has(item.external_id)).map((item) => item.id));
  }

  async function continueSourceEnrollment(source: IPTVSource) {
    if (!source.import_job_id) return;
    committing = true;
    try {
      const [latestJob, result, managed] = await Promise.all([
        getIPTVImport(source.import_job_id),
        listIPTVImportItems(source.import_job_id),
        listIPTVChannels({ source_id: source.id }),
      ]);
      job = latestJob;
      items = result.items;
      enrolledExternalIDs = new Set(managed.map((channel) => channel.external_id));
      selectionTouched = false;
      selected = new Set(items.filter((item) => isSelectable(item) && !enrolledExternalIDs.has(item.external_id)).map((item) => item.id));
      showImportForm = false;
      tab = 'sources';
      error = '';
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.loadFailed'), 'error');
    } finally {
      committing = false;
    }
  }

  async function commitSelected() {
    if (!job) return;
    committing = true;
    try {
      const continuing = job.status === 'committed';
      const res = await commitIPTVImport(job.id, [...selected]);
      showToast(t('iptv.commitSuccess', { count: String(res.channel_count) }), 'success');
      if (continuing) {
        const [result, managed] = await Promise.all([
          listIPTVImportItems(job.id),
          listIPTVChannels({ source_id: res.source.id }),
        ]);
        items = result.items;
        enrolledExternalIDs = new Set(managed.map((channel) => channel.external_id));
        selectionTouched = false;
        selected = new Set(items.filter((item) => isSelectable(item) && !enrolledExternalIDs.has(item.external_id)).map((item) => item.id));
        await loadManaged();
        return;
      }
      job = null;
      items = [];
      selected = new Set();
      selectionTouched = false;
      enrolledExternalIDs = new Set();
      showImportForm = false;
      sourceFilter = res.source.id;
      tab = 'channels';
      await loadManaged();
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.commitFailed'), 'error');
    } finally {
      committing = false;
    }
  }

  async function refreshSource(source: IPTVSource) {
    refreshing = { ...refreshing, [source.id]: true };
    try {
      await refreshIPTVSource(source.id);
      await loadSources();
      showToast(t('iptv.sourceRefreshQueued'), 'info');
      startPolling();
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.sourceRefreshFailed'), 'error');
    } finally {
      refreshing = { ...refreshing, [source.id]: false };
    }
  }

  async function updateSource(source: IPTVSource, patch: { enabled?: boolean; refresh_interval_sec?: number }) {
    savingSource = { ...savingSource, [source.id]: true };
    try {
      await updateIPTVSource(source.id, patch);
      await loadSources();
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.sourceUpdateFailed'), 'error');
    } finally {
      savingSource = { ...savingSource, [source.id]: false };
    }
  }

  async function copySourceURL(source: IPTVSource) {
    if (!source.playlist_url) return;
    try {
      await navigator.clipboard.writeText(source.playlist_url);
      showToast(t('iptv.sourceURLCopied'), 'success');
    } catch {
      showToast(t('iptv.sourceURLCopyFailed'), 'error');
    }
  }

  async function removeSource(source: IPTVSource) {
    if (!window.confirm(t('iptv.removeSourceConfirm', { name: source.name }))) return;
    try {
      await deleteIPTVSource(source.id);
      if (sourceFilter === source.id) sourceFilter = '';
      await loadManaged();
      showToast(t('iptv.sourceRemoved'), 'success');
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.deleteFailed'), 'error');
    }
  }

  async function playChannel(ch: IPTVChannel) {
    try {
      const playback = await getIPTVChannelPlayback(ch.id);
      active = { ...ch, playback };
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.playbackFailed'), 'error');
    }
  }

  async function toggleFavorite(ch: IPTVChannel) {
    try {
      await updateIPTVChannel(ch.id, { favorite: !ch.favorite });
      await loadManaged();
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.updateFailed'), 'error');
    }
  }

  async function checkChannel(ch: IPTVChannel) {
    checkBusy = { ...checkBusy, [ch.id]: true };
    try {
      const checked = await checkIPTVChannel(ch.id);
      channels = channels.map((item) => item.id === checked.id ? checked : item);
      if (active?.id === checked.id) active = { ...active, ...checked };
      showToast(t('iptv.checkComplete', { status: statusLabel(checked.probe_status) }), checked.probe_status === 'playable' ? 'success' : 'info');
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.checkFailed'), 'error');
    } finally {
      checkBusy = { ...checkBusy, [ch.id]: false };
    }
  }

  async function removeChannel(ch: IPTVChannel) {
    if (!window.confirm(t('iptv.removeChannelConfirm', { name: ch.name }))) return;
    try {
      await deleteIPTVChannel(ch.id);
      if (active?.id === ch.id) active = null;
      await loadManaged();
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.deleteFailed'), 'error');
    }
  }

  async function waitForPublishedStream(ch: IPTVChannel, waitForStart = false): Promise<boolean> {
    const attempts = waitForStart ? 12 : 1;
    for (let i = 0; i < attempts; i++) {
      try {
        const stream = await getStream(ch.stream_id);
        if (stream.active && stream.play_urls?.length) return true;
      } catch {
        // A new HLS pull needs a few seconds before lal registers it.
      }
      if (i + 1 < attempts) await new Promise((resolve) => window.setTimeout(resolve, 1000));
    }
    return false;
  }

  async function togglePublish(ch: IPTVChannel) {
    publishBusy = { ...publishBusy, [ch.id]: true };
    try {
      const enabled = !ch.publish_enabled;
      await updateIPTVChannel(ch.id, { publish_enabled: enabled });
      await loadManaged();
      if (enabled) {
        const ready = await waitForPublishedStream(ch, true);
        showToast(ready ? t('iptv.publishReady') : t('iptv.publishStarting'), ready ? 'success' : 'info');
      } else {
        showToast(t('iptv.publishDisabled'), 'success');
      }
    } catch (e) {
      showToast(e instanceof Error ? e.message : t('iptv.publishFailed'), 'error');
    } finally {
      publishBusy = { ...publishBusy, [ch.id]: false };
    }
  }

  function statusLabel(status: string): string {
    return t(`iptv.status.${status}`) || status || t('iptv.status.unknown');
  }

  function formatDate(value?: string): string {
    if (!value) return t('iptv.neverRefreshed');
    return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
  }

  function sourceInterval(source: IPTVSource): number {
    return source.refresh_interval_sec || 21600;
  }

  onMount(async () => {
    try {
      await loadManaged();
      if (sources.some((source) => source.refresh_status === 'running')) startPolling();
    } catch (e) {
      error = e instanceof Error ? e.message : t('iptv.loadFailed');
    } finally {
      loading = false;
    }
  });

  onDestroy(stopPolling);
</script>

<div class="min-h-[100dvh] th-bg-primary">
  <main class="max-w-[1440px] mx-auto px-4 sm:px-6 xl:px-8 py-6 sm:py-8">
    <header class="flex flex-col lg:flex-row lg:items-end lg:justify-between gap-5 border-b th-border pb-6">
      <div class="max-w-3xl">
        <div class="flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.16em] th-text-tertiary">
          <Radio size={15} />
          <span>{t('iptv.eyebrow')}</span>
        </div>
        <h1 class="text-3xl sm:text-4xl tracking-tight font-semibold th-text-primary mt-3">{t('iptv.title')}</h1>
        <p class="th-text-muted mt-2 leading-relaxed">{t('iptv.newSubtitle')}</p>
      </div>
      <div class="flex gap-2">
        <button class="btn btn-secondary" onclick={() => void loadManaged()} aria-label={t('common.refresh')}>
          <RefreshCw size={16} /> <span>{t('common.refresh')}</span>
        </button>
        <button class="btn btn-primary" onclick={() => { tab = 'sources'; showImportForm = true; }}>
          <Plus size={17} /> <span>{t('iptv.addSource')}</span>
        </button>
      </div>
    </header>

    {#if error}
      <div class="mt-5 rounded-lg border border-rose-300/40 bg-rose-500/5 p-4 text-sm th-text-secondary" role="alert">{error}</div>
    {/if}

    <section class="grid grid-cols-2 xl:grid-cols-4 gap-3 sm:gap-4 mt-6" aria-label={t('iptv.overview')}>
      <div class="rounded-xl border th-border p-4 sm:p-5">
        <div class="text-xs th-text-tertiary">{t('iptv.managedChannels')}</div>
        <div class="text-2xl font-semibold th-text-primary mt-2">{channels.length}</div>
      </div>
      <div class="rounded-xl border th-border p-4 sm:p-5">
        <div class="text-xs th-text-tertiary">{t('iptv.healthyChannels')}</div>
        <div class="flex items-center gap-2 mt-2"><CircleCheck size={18} class="text-emerald-500" /><span class="text-2xl font-semibold th-text-primary">{playableCount}</span></div>
      </div>
      <div class="rounded-xl border th-border p-4 sm:p-5">
        <div class="text-xs th-text-tertiary">{t('iptv.attentionNeeded')}</div>
        <div class="flex items-center gap-2 mt-2"><CircleAlert size={18} class="text-amber-500" /><span class="text-2xl font-semibold th-text-primary">{attentionCount}</span></div>
      </div>
      <div class="rounded-xl border th-border p-4 sm:p-5">
        <div class="text-xs th-text-tertiary">{t('iptv.subscriptionSources')}</div>
        <div class="flex items-center gap-2 mt-2"><ListVideo size={18} class="th-text-muted" /><span class="text-2xl font-semibold th-text-primary">{sources.length}</span></div>
      </div>
    </section>

    <nav class="flex items-center gap-1 border-b th-border mt-7" aria-label={t('iptv.workspace')}>
      <button class="px-4 py-3 text-sm border-b-2 transition-colors" class:border-blue-500={tab === 'channels'} class:th-text-primary={tab === 'channels'} class:border-transparent={tab !== 'channels'} class:th-text-muted={tab !== 'channels'} onclick={() => tab = 'channels'}>
        <span class="inline-flex items-center gap-2"><Tv size={16} />{t('iptv.channelLibrary')}<span class="rounded-full px-2 py-0.5 text-xs th-bg-secondary">{visibleChannels.length}</span></span>
      </button>
      <button class="px-4 py-3 text-sm border-b-2 transition-colors" class:border-blue-500={tab === 'sources'} class:th-text-primary={tab === 'sources'} class:border-transparent={tab !== 'sources'} class:th-text-muted={tab !== 'sources'} onclick={() => tab = 'sources'}>
        <span class="inline-flex items-center gap-2"><ListVideo size={16} />{t('iptv.subscriptionSources')}<span class="rounded-full px-2 py-0.5 text-xs th-bg-secondary">{sources.length}</span></span>
      </button>
    </nav>

    {#if tab === 'sources'}
      <section class="grid xl:grid-cols-[minmax(0,1.5fr)_minmax(300px,0.8fr)] gap-6 mt-6 items-start">
        <div class="min-w-0">
          <div class="flex items-end justify-between gap-4 mb-4">
            <div>
              <h2 class="text-xl font-semibold th-text-primary">{t('iptv.sourcesTitle')}</h2>
              <p class="text-sm th-text-muted mt-1">{t('iptv.sourcesDescription')}</p>
            </div>
            <span class="text-xs th-text-tertiary shrink-0">{t('iptv.sourceCount', { count: String(sources.length) })}</span>
          </div>

          {#if loading}
            <div class="space-y-3" aria-label={t('common.loading')}>
              {#each [1, 2] as item}<div class="h-28 rounded-xl border th-border animate-pulse" aria-hidden="true"></div>{/each}
            </div>
          {:else if sources.length === 0}
            <div class="rounded-xl border border-dashed th-border p-8 sm:p-12 text-center">
              <ListVideo size={30} class="mx-auto th-text-tertiary" />
              <h3 class="font-medium th-text-primary mt-4">{t('iptv.noSources')}</h3>
              <p class="text-sm th-text-muted mt-1 max-w-md mx-auto">{t('iptv.noSourcesHint')}</p>
              <button class="btn btn-primary mt-5" onclick={() => showImportForm = true}><Plus size={16} />{t('iptv.addSource')}</button>
            </div>
          {:else}
            <div class="divide-y th-border rounded-xl border th-border overflow-hidden">
              {#each sources as source (source.id)}
                <article class="p-4 sm:p-5">
                  <div class="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
                    <div class="min-w-0 flex-1">
                      <div class="flex flex-wrap items-center gap-2">
                        <h3 class="font-semibold th-text-primary truncate">{source.name}</h3>
                        {#if source.refresh_status === 'running'}
                          <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs bg-blue-500/10 text-blue-600"><LoaderCircle size={13} class="animate-spin" />{t('iptv.refreshing')}</span>
                        {:else if source.last_error}
                          <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs bg-rose-500/10 text-rose-600"><CircleAlert size={13} />{t('iptv.refreshFailed')}</span>
                        {:else if source.enabled}
                          <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs bg-emerald-500/10 text-emerald-600"><CircleCheck size={13} />{t('iptv.sourceActive')}</span>
                        {:else}
                          <span class="rounded-full px-2.5 py-1 text-xs th-bg-secondary th-text-muted">{t('iptv.sourcePaused')}</span>
                        {/if}
                      </div>
                      {#if source.playlist_url}
                        <div class="flex items-start gap-2 mt-2">
                          <p id={`source-url-${source.id}`} class="min-w-0 flex-1 text-xs font-mono th-text-tertiary {visibleSourceURLs[source.id] ? 'break-all' : 'truncate'}" title={visibleSourceURLs[source.id] ? undefined : source.playlist_url}>{source.playlist_url}</p>
                          <button class="btn btn-ghost btn-sm shrink-0" aria-label={visibleSourceURLs[source.id] ? t('iptv.hideSourceURL') : t('iptv.showSourceURL')} aria-expanded={Boolean(visibleSourceURLs[source.id])} aria-controls={`source-url-${source.id}`} title={visibleSourceURLs[source.id] ? t('iptv.hideSourceURL') : t('iptv.showSourceURL')} onclick={() => visibleSourceURLs = { ...visibleSourceURLs, [source.id]: !visibleSourceURLs[source.id] }}>
                            {#if visibleSourceURLs[source.id]}<EyeOff size={14} />{t('iptv.hideSourceURL')}{:else}<Eye size={14} />{t('iptv.showSourceURL')}{/if}
                          </button>
                          <button class="btn btn-ghost btn-sm shrink-0" aria-label={t('iptv.copySourceURL')} title={t('iptv.copySourceURL')} onclick={() => void copySourceURL(source)}><Copy size={14} />{t('iptv.copySourceURL')}</button>
                        </div>
                      {:else}
                        <p class="text-xs th-text-tertiary mt-1">{t('iptv.pastedPlaylist')}</p>
                      {/if}
                      <div class="flex flex-wrap gap-x-5 gap-y-1 text-xs th-text-muted mt-3">
                        <span>{t('iptv.lastRefreshed')}: {formatDate(source.last_refreshed_at)}</span>
                        {#if source.last_refresh_new}<span class="text-amber-600">{t('iptv.newEntries', { count: String(source.last_refresh_new) })}</span>{/if}
                        {#if source.last_refresh_missing}<span class="text-amber-600">{t('iptv.missingEntries', { count: String(source.last_refresh_missing) })}</span>{/if}
                      </div>
                      {#if source.last_error}<p class="text-xs text-rose-600 mt-2">{source.last_error}</p>{/if}
                    </div>
              <div class="flex flex-wrap sm:justify-end items-center gap-2">
                      {#if source.import_job_id}<button class="btn btn-secondary btn-sm" disabled={committing} onclick={() => void continueSourceEnrollment(source)}><Plus size={15} /><span>{t('iptv.continueEnrollment')}</span></button>{/if}
                      {#if source.playlist_url}
                        <label class="sr-only" for={`interval-${source.id}`}>{t('iptv.refreshInterval')}</label>
                        <select id={`interval-${source.id}`} class="input input-sm w-auto min-w-32" value={sourceInterval(source)} disabled={savingSource[source.id]} onchange={(event) => void updateSource(source, { refresh_interval_sec: Number(event.currentTarget.value) })}>
                          <option value={3600}>{t('iptv.interval1h')}</option>
                          <option value={21600}>{t('iptv.interval6h')}</option>
                          <option value={43200}>{t('iptv.interval12h')}</option>
                          <option value={86400}>{t('iptv.interval24h')}</option>
                        </select>
                        <button class="btn btn-secondary btn-sm" disabled={source.refresh_status === 'running' || refreshing[source.id]} onclick={() => void refreshSource(source)}>
                          {#if source.refresh_status === 'running' || refreshing[source.id]}<LoaderCircle size={15} class="animate-spin" />{:else}<RefreshCw size={15} />{/if}
                          <span>{t('iptv.refreshNow')}</span>
                        </button>
                        <button class="btn btn-ghost btn-sm" aria-label={source.enabled ? t('iptv.pauseRefresh') : t('iptv.resumeRefresh')} title={source.enabled ? t('iptv.pauseRefresh') : t('iptv.resumeRefresh')} onclick={() => void updateSource(source, { enabled: !source.enabled })}>
                          {source.enabled ? t('iptv.pause') : t('iptv.resume')}
                        </button>
                      {/if}
                      <button class="btn btn-ghost btn-sm text-rose-600" aria-label={t('iptv.removeSource')} title={t('iptv.removeSource')} onclick={() => void removeSource(source)}><Trash2 size={15} /></button>
                    </div>
                  </div>
                </article>
              {/each}
            </div>
          {/if}
        </div>

        <aside class="rounded-xl border th-border p-5 sm:p-6">
          {#if showImportForm || sources.length === 0}
            <div class="flex items-start justify-between gap-3">
              <div><div class="text-xs font-semibold uppercase tracking-widest th-text-tertiary">{t('iptv.setupStep')}</div><h2 class="text-lg font-semibold th-text-primary mt-2">{t('iptv.importTitle')}</h2><p class="text-sm th-text-muted mt-1">{t('iptv.importDescription')}</p></div>
              {#if sources.length > 0}<button class="btn btn-ghost btn-sm" aria-label={t('common.close')} onclick={() => showImportForm = false}><X size={16} /></button>{/if}
            </div>
            <div class="space-y-4 mt-5">
              <label class="block"><span class="input-label">{t('iptv.importName')}</span><input class="input mt-1 w-full" bind:value={importName} placeholder={t('iptv.importNameHint')} /></label>
              <label class="block"><span class="input-label">{t('iptv.playlistURL')}</span><input class="input mt-1 w-full" bind:value={playlistURL} placeholder="https://example.com/live.m3u" autocomplete="url" /></label>
              <div class="flex items-center gap-3 text-xs th-text-tertiary"><span class="h-px flex-1 th-bg-secondary"></span>{t('iptv.orPastePlaylist')}<span class="h-px flex-1 th-bg-secondary"></span></div>
              <label class="block"><span class="input-label">{t('iptv.playlistText')}</span><textarea class="input mt-1 w-full min-h-28 font-mono text-xs" bind:value={playlistText} placeholder="#EXTM3U" spellcheck="false"></textarea></label>
              <label class="block"><span class="input-label">{t('iptv.requestHeaders')}</span><textarea class="input mt-1 w-full min-h-20 font-mono text-xs" bind:value={requestHeadersText} placeholder={'{\n  "Referer": "https://example.com/",\n  "User-Agent": "Mozilla/5.0"\n}'} spellcheck="false"></textarea><span class="block text-xs th-text-muted mt-1">{t('iptv.requestHeadersHint')}</span></label>
              <button class="btn btn-primary w-full justify-center" onclick={() => void submitImport()} disabled={importing || (!playlistURL.trim() && !playlistText.trim())}>
                {#if importing}<LoaderCircle size={16} class="animate-spin" />{:else}<Upload size={16} />{/if}<span>{importing ? t('iptv.importing') : t('iptv.scanPlaylist')}</span>
              </button>
            </div>
          {:else}
            <div class="text-xs font-semibold uppercase tracking-widest th-text-tertiary">{t('iptv.howSyncWorks')}</div>
            <h2 class="text-lg font-semibold th-text-primary mt-2">{t('iptv.refreshTitle')}</h2>
            <p class="text-sm th-text-muted mt-2 leading-relaxed">{t('iptv.refreshDescription')}</p>
            <ul class="mt-5 space-y-3 text-sm th-text-secondary">
              <li class="flex gap-3"><span class="text-emerald-600">01</span><span>{t('iptv.refreshPreserves')}</span></li>
              <li class="flex gap-3"><span class="text-emerald-600">02</span><span>{t('iptv.refreshAddsReview')}</span></li>
              <li class="flex gap-3"><span class="text-emerald-600">03</span><span>{t('iptv.refreshKeepsMissing')}</span></li>
            </ul>
            <button class="btn btn-secondary mt-6 w-full justify-center" onclick={() => showImportForm = true}><Plus size={16} />{t('iptv.addAnotherSource')}</button>
          {/if}
        </aside>
      </section>

      {#if job}
        <section class="mt-6 rounded-xl border th-border overflow-hidden">
          <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4 sm:p-5 border-b th-border">
            <div><div class="text-xs font-semibold uppercase tracking-widest th-text-tertiary">{t('iptv.validationReview')}</div><h2 class="font-semibold th-text-primary mt-1">{job.name}</h2><p class="text-xs th-text-muted mt-1">{statusLabel(job.status)}</p></div>
            <div class="flex gap-2"><button class="btn btn-secondary btn-sm" onclick={selectPlayable}>{t('iptv.selectPlayable')}</button><button class="btn btn-primary btn-sm" onclick={() => void commitSelected()} disabled={committing || selected.size === 0}><Check size={15} />{t(job.status === 'committed' ? 'iptv.addMoreChannels' : 'iptv.commit', { count: String(selected.size) })}</button></div>
          </div>
          <div class="px-4 py-4 sm:px-5 border-b th-border" aria-live="polite">
            <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
              <div class="flex items-center gap-2 min-w-0">
                {#if probeActive}
                  <LoaderCircle size={16} class="shrink-0 th-text-primary" aria-hidden="true" />
                  <span class="text-sm font-medium th-text-primary">{t('iptv.probeInProgress')}</span>
                {:else}
                  <CircleCheck size={16} class="shrink-0 text-emerald-600" aria-hidden="true" />
                  <span class="text-sm font-medium th-text-primary">{t('iptv.probeComplete')}</span>
                {/if}
                <span class="text-sm th-text-muted">{t('iptv.probeProgress', { done: String(completedProbeCount), total: String(job.total_items) })}</span>
              </div>
              <span class="text-sm font-semibold tabular-nums th-text-primary">{probeProgress}%</span>
            </div>
            <div class="mt-3 h-2 rounded-full th-bg-secondary overflow-hidden" role="progressbar" aria-label={t('iptv.probeProgressLabel')} aria-valuemin="0" aria-valuemax="100" aria-valuenow={probeProgress}>
              <div class="h-full rounded-full bg-emerald-500" style={`width: ${probeProgress}%`}></div>
            </div>
            <div class="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs th-text-muted">
              <span>{t('iptv.probePlayableCount', { count: String(playableProbeCount) })}</span>
              <span>{t('iptv.probeFailedCount', { count: String(failedProbeCount) })}</span>
              <span>{t('iptv.probeUnsupportedCount', { count: String(unsupportedProbeCount) })}</span>
              <span>{t('iptv.probePendingCount', { count: String(pendingProbeCount) })}</span>
            </div>
          </div>
          <div class="max-h-80 overflow-auto">
            <table class="w-full text-sm">
              <thead class="sticky top-0 th-bg-secondary th-text-muted"><tr><th class="text-left p-3 w-10"></th><th class="text-left p-3">{t('iptv.channel')}</th><th class="text-left p-3">{t('iptv.group')}</th><th class="text-left p-3">{t('iptv.statusLabel')}</th><th class="text-left p-3">{t('iptv.codec')}</th><th class="text-left p-3">{t('iptv.probeReason')}</th></tr></thead>
              <tbody class="divide-y th-border">{#each items as item (item.id)}{@const alreadyEnrolled = enrolledExternalIDs.has(item.external_id)}<tr><td class="p-3"><input type="checkbox" checked={selected.has(item.id)} disabled={!isSelectable(item) || alreadyEnrolled} onchange={() => toggleItem(item.id, item)} /></td><td class="p-3 th-text-primary">{item.name}{#if alreadyEnrolled}<span class="ml-2 text-xs text-emerald-600">{t('iptv.alreadyEnrolled')}</span>{/if}</td><td class="p-3 th-text-muted">{item.group_name || '—'}</td><td class="p-3">{statusLabel(item.status)}{#if item.http_status}<span class="ml-1 text-xs th-text-muted">HTTP {item.http_status}</span>{/if}</td><td class="p-3 th-text-muted">{[item.video_codec, item.audio_codec].filter(Boolean).join('/') || '—'}</td><td class="p-3 text-xs th-text-muted max-w-xs">{item.error || '—'}</td></tr>{/each}</tbody>
            </table>
          </div>
        </section>
      {/if}
    {:else}
      <section class="grid xl:grid-cols-[minmax(0,1.35fr)_minmax(360px,0.8fr)] gap-6 mt-6 items-start">
        <div class="min-w-0">
          <div class="flex flex-col sm:flex-row sm:items-center gap-3 mb-4">
            <label class="relative flex-1 min-w-0"><Search size={16} class="absolute left-3 top-1/2 -translate-y-1/2 th-text-tertiary" /><input class="input w-full pl-9" bind:value={search} oninput={() => void loadManaged()} placeholder={t('iptv.search')} /></label>
            <select class="input sm:w-48" bind:value={sourceFilter} onchange={() => void loadManaged()} aria-label={t('iptv.subscriptionSources')}><option value="">{t('iptv.allSources')}</option>{#each sources as source}<option value={source.id}>{source.name}</option>{/each}</select>
            <select class="input sm:w-44" bind:value={membershipFilter} aria-label={t('iptv.channelMembership')}><option value="all">{t('iptv.channelMembershipAll')}</option><option value="available">{t('iptv.availableToJoin')}</option><option value="joined">{t('iptv.joinedNvr')}</option></select>
            <select class="input sm:w-44" bind:value={groupFilter} onchange={() => void loadManaged()} aria-label={t('iptv.playlistGroups')}><option value="">{t('iptv.playlistGroups')}</option>{#each groups as group}<option value={group}>{group}</option>{/each}</select>
            <button class="btn btn-ghost btn-sm" class:th-text-primary={showFavorites} aria-pressed={showFavorites} onclick={() => { showFavorites = !showFavorites; void loadManaged(); }}><Star size={15} class={showFavorites ? 'fill-amber-400 text-amber-500' : ''} /><span>{favoriteCount}</span></button>
          </div>
          {#if loading}
            <div class="space-y-2">{#each [1, 2, 3, 4] as item}<div class="h-16 rounded-lg border th-border animate-pulse" aria-hidden="true"></div>{/each}</div>
          {:else if visibleChannels.length === 0}
            <div class="rounded-xl border border-dashed th-border p-10 sm:p-14 text-center">
              <Tv size={30} class="mx-auto th-text-tertiary" />
              <h2 class="font-medium th-text-primary mt-4">{t('iptv.empty')}</h2>
              <p class="text-sm th-text-muted mt-1">{sources.length ? t('iptv.noMatchingChannels') : t('iptv.emptyHint')}</p>
              <button class="btn btn-primary mt-5" onclick={() => { tab = 'sources'; showImportForm = true; }}><Plus size={16} />{t('iptv.addSource')}</button>
            </div>
          {:else}
            <div class="rounded-xl border th-border overflow-hidden">
              <div class="hidden md:grid grid-cols-[minmax(0,1fr)_130px_120px_auto] gap-4 px-4 py-2.5 text-[11px] uppercase tracking-wider th-text-tertiary th-bg-secondary">
                <span>{t('iptv.channel')}</span><span>{t('iptv.group')}</span><span>{t('iptv.statusLabel')}</span><span class="text-right">{t('iptv.actions')}</span>
              </div>
              <div class="divide-y th-border">
                {#each visibleChannels as ch (ch.id)}
                  <article class="grid md:grid-cols-[minmax(0,1fr)_130px_120px_auto] items-center gap-2 md:gap-4 px-3 sm:px-4 py-3 hover:bg-black/[0.02] dark:hover:bg-white/[0.025] transition-colors" class:th-bg-secondary={active?.id === ch.id}>
                    <button class="min-w-0 text-left" onclick={() => void playChannel(ch)}>
                      <span class="flex items-center gap-2 min-w-0"><span class="font-medium th-text-primary truncate">{ch.name}</span>{#if ch.publish_enabled}<span class="shrink-0 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] text-emerald-600">{t('iptv.published')}</span>{/if}</span>
                      <span class="block text-xs th-text-tertiary truncate mt-0.5">{[ch.video_codec, ch.audio_codec].filter(Boolean).join(' / ') || ch.stream_id}</span>
                    </button>
                    <span class="hidden md:block text-xs th-text-muted truncate">{ch.group_name || t('iptv.ungrouped')}</span>
                    <span class="flex items-center gap-1.5 text-xs {ch.probe_status === 'playable' ? 'text-emerald-600' : ch.probe_status === 'failed' || ch.probe_status === 'unsupported' ? 'text-rose-600' : 'text-amber-600'}" title={ch.last_checked_at ? `${t('iptv.lastChecked')}: ${formatDate(ch.last_checked_at)}` : t('iptv.notCheckedYet')}>
                      {#if ch.probe_status === 'playable'}<CircleCheck size={14} />{:else}<CircleAlert size={14} />{/if}{statusLabel(ch.probe_status)}
                    </span>
                    <div class="flex items-center justify-end gap-0.5">
                      <button class="btn btn-ghost btn-xs" title={ch.favorite ? t('iptv.unfavorite') : t('iptv.favorite')} aria-label={ch.favorite ? t('iptv.unfavorite') : t('iptv.favorite')} onclick={() => void toggleFavorite(ch)}><Star size={15} class={ch.favorite ? 'fill-amber-400 text-amber-500' : ''} /></button>
                      <button class="btn btn-ghost btn-xs" title={t('iptv.checkChannel')} aria-label={t('iptv.checkChannel')} disabled={checkBusy[ch.id]} onclick={() => void checkChannel(ch)}>
                        {#if checkBusy[ch.id]}<LoaderCircle size={15} class="animate-spin" />{:else}<RefreshCw size={15} />{/if}
                      </button>
                      <button class="btn btn-ghost btn-xs" title={ch.publish_enabled ? t('iptv.stopPublishing') : t('iptv.publish')} aria-label={ch.publish_enabled ? t('iptv.stopPublishing') : t('iptv.publish')} disabled={publishBusy[ch.id] || !ch.recordable} onclick={() => void togglePublish(ch)}>
                        {#if ch.publish_enabled}<Radio size={15} class="text-emerald-600" />{:else}<Radio size={15} />{/if}
                      </button>
                      <button class="btn btn-ghost btn-xs" title={t('iptv.configureRecording')} aria-label={t('iptv.configureRecording')} onclick={() => (window.location.hash = `#/recording-plans?stream_id=${encodeURIComponent(ch.stream_id)}`)}><CalendarClock size={15} /></button>
                      <button class="btn btn-ghost btn-xs text-rose-600" title={t('iptv.removeChannel')} aria-label={t('iptv.removeChannel')} onclick={() => void removeChannel(ch)}><Trash2 size={15} /></button>
                    </div>
                    {#if ch.probe_error}<p class="md:col-span-4 text-xs text-rose-600 pl-0.5">{ch.probe_error}</p>{/if}
                  </article>
                {/each}
              </div>
            </div>
          {/if}
        </div>

        <aside class="xl:sticky xl:top-5 min-w-0">
          <div class="rounded-xl border th-border overflow-hidden">
            <div class="aspect-video bg-[#111]">
              {#if active}
                <VideoPlayer cameraId={active.id} cameraName={active.name} streamUrl={active.playback.url} cameraProtocol="iptv" />
              {:else}
                <div class="h-full flex flex-col items-center justify-center text-white/55"><Tv size={26} /><span class="text-sm mt-3">{t('iptv.pickChannel')}</span></div>
              {/if}
            </div>
            {#if active}
              <div class="p-4">
                <div class="flex items-start justify-between gap-4"><div class="min-w-0"><div class="text-[11px] uppercase tracking-wider th-text-tertiary">{t('iptv.nowPlaying')}</div><h2 class="font-semibold th-text-primary truncate mt-1">{active.name}</h2><p class="text-xs th-text-muted mt-1">{active.group_name || t('iptv.ungrouped')} · {statusLabel(active.probe_status)}</p></div><button class="btn btn-ghost btn-xs" aria-label={t('common.close')} onclick={() => active = null}><X size={15} /></button></div>
                {#if active.publish_enabled}<div class="mt-4 pt-3 border-t th-border flex items-center justify-between gap-3"><span class="text-xs th-text-muted truncate">{t('iptv.publishedStream', { stream_id: active.stream_id })}</span><a class="inline-flex items-center gap-1 text-xs text-blue-500 whitespace-nowrap" href={`#/streams/${encodeURIComponent(active.stream_id)}`}>{t('iptv.viewStream')}<ExternalLink size={13} /></a></div>{/if}
              </div>
            {:else}
              <div class="p-4"><div class="font-medium th-text-primary">{t('iptv.previewTitle')}</div><p class="text-xs th-text-muted mt-1">{t('iptv.previewHint')}</p></div>
            {/if}
          </div>
        </aside>
      </section>
    {/if}
  </main>
</div>
