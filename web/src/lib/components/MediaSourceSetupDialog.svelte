<script lang="ts">
  import { untrack } from 'svelte';
  import { createCamera, createStream, discoverONVIFDevices, listCameras, getEmbyConfig, saveEmbyConfig, browseEmbyLibrary, addEmbyItem, listEmbyItems, createIPTVImport, getIPTVImport, listIPTVImportItems, commitIPTVImport } from '$lib/api';
  import type { DiscoveredDevice, EmbyConfig, EmbyMediaItem, IPTVImportItem, IPTVImportJob } from '$lib/api';
  import { t } from '$lib/i18n';
  import { showToast } from '$lib/toast';
  import { ArrowLeft, ArrowRight, Camera, Check, Film, Link2, LoaderCircle, Radio, RefreshCw, Tv, X } from 'lucide-svelte';

  type SourceType = 'camera' | 'url' | 'push' | 'iptv' | 'emby';
  let { onclose, onsaved, initialSource = null }: { onclose: () => void; onsaved: () => void; initialSource?: SourceType | null } = $props();
  let selected = $state<SourceType | null>(untrack(() => initialSource));
  let saving = $state(false);
  let error = $state('');

  let name = $state('');
  let streamID = $state('');
  let sourceURL = $state('');
  let cameraProtocol = $state('rtsp');
  let username = $state('');
  let password = $state('');
  let scanningONVIF = $state(false);
  let onvifDevices = $state<DiscoveredDevice[]>([]);
  let onvifScanError = $state('');

  let emby = $state<EmbyConfig>({ name: '', base_url: '', user_id: '', token: '', configured: false, has_token: false });
  let embyItems = $state<EmbyMediaItem[]>([]);
  let embyQuery = $state('');
  let embyLoading = $state(false);
  let embyAdded = $state<Set<string>>(new Set());

  let playlistURL = $state('');
  let playlistText = $state('');
  let importJob = $state<IPTVImportJob | null>(null);
  let importItems = $state<IPTVImportItem[]>([]);
  let selectedItems = $state<Set<string>>(new Set());

  const sourceOptions = [
    { id: 'camera' as const, icon: Camera, title: 'mediaIngest.camera', description: 'mediaIngest.cameraHint' },
    { id: 'url' as const, icon: Link2, title: 'mediaIngest.url', description: 'mediaIngest.urlHint' },
    { id: 'push' as const, icon: Radio, title: 'mediaIngest.push', description: 'mediaIngest.pushHint' },
    { id: 'iptv' as const, icon: Tv, title: 'mediaIngest.iptv', description: 'mediaIngest.iptvHint' },
    { id: 'emby' as const, icon: Film, title: 'mediaIngest.emby', description: 'mediaIngest.embyHint' },
  ];

  if (untrack(() => initialSource) === 'emby') void loadEmbyConfig();

  function choose(type: SourceType) {
    selected = type;
    error = '';
    if (type === 'camera') {
      onvifDevices = [];
      onvifScanError = '';
    }
    if (type === 'emby') void loadEmbyConfig();
  }

  async function scanONVIF() {
    scanningONVIF = true;
    onvifScanError = '';
    try {
      const [result, cameras] = await Promise.all([discoverONVIFDevices(5), listCameras()]);
      const existingEndpoints = new Set(cameras.filter((camera) => camera.protocol === 'onvif' && camera.url).map((camera) => camera.url));
      onvifDevices = result.devices.filter((device) => !existingEndpoints.has(device.endpoint || device.xaddrs[0] || ''));
      onvifScanError = result.error?.message || '';
    } catch (e) {
      onvifDevices = [];
      onvifScanError = e instanceof Error ? e.message : t('mediaIngest.setup.scanFailed');
    } finally { scanningONVIF = false; }
  }

  function selectONVIFDevice(device: DiscoveredDevice) {
    name = device.name || t('onvif.deviceName');
    sourceURL = device.endpoint || device.xaddrs[0] || '';
  }

  async function loadEmbyConfig() {
    try {
      emby = await getEmbyConfig();
      if (emby.configured) embyAdded = new Set((await listEmbyItems()).map((item) => item.emby_item_id));
    }
    catch (e) { error = e instanceof Error ? e.message : t('mediaIngest.setup.loadFailed'); }
  }

  async function submit() {
    error = '';
    saving = true;
    try {
      if (selected === 'camera') {
        await createCamera({ name: name.trim(), protocol: cameraProtocol, url: sourceURL.trim(), username: username.trim() || undefined, password: password || undefined, enabled: true });
        showToast(t('mediaIngest.setup.cameraAdded'), 'success');
        onsaved(); onclose(); return;
      }
      if (selected === 'url' || selected === 'push') {
        await createStream({ stream_id: streamID.trim(), name: name.trim() || undefined, input_mode: selected === 'url' ? 'pull' : 'push', source_url: selected === 'url' ? sourceURL.trim() : undefined });
        showToast(t('mediaIngest.setup.streamCreated'), 'success');
        onsaved(); onclose(); return;
      }
      if (selected === 'emby') {
        emby = await saveEmbyConfig({ name: emby.name.trim() || undefined, base_url: emby.base_url.trim(), user_id: emby.user_id.trim(), token: emby.token || '' });
        emby.token = '';
        await searchEmby();
        embyAdded = new Set((await listEmbyItems()).map((item) => item.emby_item_id));
        showToast(t('emby.connected'), 'success');
        onsaved();
        return;
      }
      if (selected === 'iptv') {
        const job = await createIPTVImport({ name: name.trim() || undefined, playlist_url: playlistText.trim() ? undefined : playlistURL.trim(), playlist_text: playlistText.trim() || undefined });
        importJob = job;
        for (let attempt = 0; attempt < 60; attempt++) {
          const [latest, rows] = await Promise.all([getIPTVImport(job.id), listIPTVImportItems(job.id)]);
          importJob = latest;
          importItems = rows.items;
          if (latest.status === 'ready' || latest.status === 'failed' || latest.status === 'committed') break;
          await new Promise((resolve) => window.setTimeout(resolve, 500));
        }
        selectedItems = new Set(importItems.filter((item) => (item.playable || item.status === 'playable' || item.status === 'warning') && !item.drm && item.status !== 'unsupported' && item.status !== 'failed').map((item) => item.id));
      }
    } catch (e) {
      error = e instanceof Error ? e.message : t('mediaIngest.setup.saveFailed');
    } finally { saving = false; }
  }

  async function searchEmby() {
    embyLoading = true;
    error = '';
    try { embyItems = (await browseEmbyLibrary({ q: embyQuery.trim() || undefined, limit: 60 })).items; }
    catch (e) { error = e instanceof Error ? e.message : t('mediaIngest.setup.libraryFailed'); }
    finally { embyLoading = false; }
  }

  async function addEmby(item: EmbyMediaItem) {
    try { await addEmbyItem(item.id); embyAdded = new Set(embyAdded).add(item.id); showToast(t('emby.added'), 'success'); onsaved(); }
    catch (e) { error = e instanceof Error ? e.message : t('emby.addFailed'); }
  }

  function toggleIPTVItem(item: IPTVImportItem) {
    if (item.drm || item.status === 'unsupported' || item.status === 'failed') return;
    const next = new Set(selectedItems);
    if (next.has(item.id)) next.delete(item.id); else next.add(item.id);
    selectedItems = next;
  }

  async function refreshIPTVImport() {
    if (!importJob) return;
    saving = true; error = '';
    try {
      const [latest, rows] = await Promise.all([getIPTVImport(importJob.id), listIPTVImportItems(importJob.id)]);
      importJob = latest;
      importItems = rows.items;
      if (selectedItems.size === 0) selectedItems = new Set(rows.items.filter((item) => (item.playable || item.status === 'playable' || item.status === 'warning') && !item.drm && item.status !== 'unsupported' && item.status !== 'failed').map((item) => item.id));
    } catch (e) { error = e instanceof Error ? e.message : t('iptv.loadFailed'); }
    finally { saving = false; }
  }

  async function commitIPTV() {
    if (!importJob || selectedItems.size === 0) return;
    saving = true; error = '';
    try {
      const result = await commitIPTVImport(importJob.id, [...selectedItems]);
      showToast(t('iptv.commitSuccess', { count: String(result.channel_count) }), 'success');
      onsaved(); onclose();
    } catch (e) { error = e instanceof Error ? e.message : t('iptv.commitFailed'); }
    finally { saving = false; }
  }

  function handleKeydown(event: KeyboardEvent) { if (event.key === 'Escape' && !saving) onclose(); }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="fixed inset-0 z-50 bg-black/55 flex items-center justify-center p-3 sm:p-5" role="presentation" onclick={(event) => { if (event.target === event.currentTarget && !saving) onclose(); }}>
  <div class="w-full max-w-3xl max-h-[92vh] overflow-hidden rounded-2xl border th-border th-bg-primary shadow-xl flex flex-col" role="dialog" aria-modal="true" aria-labelledby="media-source-setup-title" tabindex="-1">
    <header class="flex items-start justify-between gap-4 px-5 sm:px-6 py-5 border-b th-border shrink-0">
      <div>
        <p class="text-xs font-semibold uppercase tracking-[0.14em] th-text-muted mb-1">{t('mediaIngest.addSource')}</p>
        <h2 id="media-source-setup-title" class="text-xl font-semibold th-text-primary">{selected ? t(sourceOptions.find((option) => option.id === selected)?.title || 'mediaIngest.wizardTitle') : t('mediaIngest.wizardTitle')}</h2>
        <p class="text-sm th-text-muted mt-1">{selected ? t(sourceOptions.find((option) => option.id === selected)?.description || 'mediaIngest.wizardHint') : t('mediaIngest.wizardHint')}</p>
      </div>
      <button class="btn btn-ghost" onclick={onclose} disabled={saving} aria-label={t('mediaIngest.close')}><X size={18} /></button>
    </header>

    <div class="overflow-y-auto p-5 sm:p-6">
      {#if !selected}
        <div class="grid sm:grid-cols-2 gap-3">
          {#each sourceOptions as option (option.id)}
            <button type="button" class="group min-h-32 rounded-xl border th-border p-4 text-left hover:th-bg-secondary hover:th-border-hover transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-primary)]" onclick={() => choose(option.id)}>
              <div class="flex items-start justify-between gap-3"><span class="w-10 h-10 rounded-lg th-bg-secondary flex items-center justify-center th-text-primary"><option.icon size={19} /></span><ArrowRight size={16} class="th-text-muted group-hover:translate-x-0.5 transition-transform mt-1" /></div>
              <span class="block mt-3 font-medium th-text-primary">{t(option.title)}</span>
              <span class="block text-sm th-text-muted mt-1 leading-relaxed">{t(option.description)}</span>
            </button>
          {/each}
        </div>
      {:else if selected === 'camera'}
        <form class="space-y-4" onsubmit={(event) => { event.preventDefault(); void submit(); }}>
          <div class="grid sm:grid-cols-2 gap-4">
            <label><span class="input-label">{t('mediaIngest.setup.name')}</span><input class="input mt-1 w-full" bind:value={name} required /></label>
            <label><span class="input-label">{t('mediaIngest.setup.protocol')}</span><select class="input mt-1 w-full" bind:value={cameraProtocol}><option value="rtsp">RTSP</option><option value="onvif">ONVIF</option><option value="http">HTTP</option></select></label>
          </div>
          {#if cameraProtocol === 'onvif'}
            <section class="rounded-xl border th-border p-4 space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div><h3 class="text-sm font-semibold th-text-primary">{t('onvif.discover')}</h3><p class="text-xs th-text-muted mt-1">{t('mediaIngest.setup.onvifDiscoveryHint')}</p></div>
                <button type="button" class="btn btn-secondary btn-sm" onclick={() => void scanONVIF()} disabled={scanningONVIF}>{#if scanningONVIF}<LoaderCircle size={15} class="animate-spin" />{:else}<RefreshCw size={15} />{/if}{scanningONVIF ? t('onvif.discovering') : t('onvif.discover')}</button>
              </div>
              {#if onvifScanError}<p class="text-sm th-color-danger" role="alert">{onvifScanError}</p>{/if}
              {#if scanningONVIF}<p class="text-sm th-text-muted">{t('onvif.discovering')}</p>
              {:else if onvifDevices.length > 0}<div class="max-h-44 overflow-y-auto space-y-2">{#each onvifDevices as device (device.uuid)}<article class="flex items-center justify-between gap-3 rounded-lg th-bg-secondary p-3"><div class="min-w-0"><p class="font-medium text-sm th-text-primary truncate">{device.name || t('onvif.deviceName')}</p><p class="text-xs th-text-muted truncate">{device.endpoint || device.xaddrs[0]}</p>{#if device.hardware}<p class="text-xs th-text-muted mt-0.5">{device.hardware}</p>{/if}</div><button type="button" class="btn btn-secondary btn-sm shrink-0" onclick={() => selectONVIFDevice(device)}>{t('mediaIngest.setup.useDevice')}</button></article>{/each}</div>
              {:else}<p class="text-sm th-text-muted">{t('onvif.noDevices')}</p>{/if}
            </section>
          {/if}
          <label class="block"><span class="input-label">{t('mediaIngest.setup.address')}</span><input class="input mt-1 w-full" bind:value={sourceURL} placeholder={cameraProtocol === 'onvif' ? 'http://192.168.1.10:80/onvif/device_service' : 'rtsp://192.168.1.10:554/stream'} required /></label>
          <div class="grid sm:grid-cols-2 gap-4">
            <label><span class="input-label">{t(cameraProtocol === 'onvif' ? 'onvif.username' : 'mediaIngest.setup.username')}</span><input class="input mt-1 w-full" bind:value={username} autocomplete="username" /></label>
            <label><span class="input-label">{t(cameraProtocol === 'onvif' ? 'onvif.password' : 'mediaIngest.setup.password')}</span><input class="input mt-1 w-full" type="password" bind:value={password} autocomplete="current-password" /></label>
          </div>
          {#if cameraProtocol === 'onvif'}<p class="text-xs th-text-muted -mt-2">{t('onvif.credentialsHint')}</p>{/if}
          <div class="flex justify-between items-center pt-2"><button type="button" class="btn btn-ghost" onclick={() => selected = null} disabled={saving}><ArrowLeft size={16} />{t('common.back')}</button><button class="btn btn-primary" disabled={saving || !name.trim() || !sourceURL.trim()}>{#if saving}<LoaderCircle size={16} class="animate-spin" />{/if}{t('mediaIngest.setup.addCamera')}</button></div>
        </form>
      {:else if selected === 'url' || selected === 'push'}
        <form class="space-y-4" onsubmit={(event) => { event.preventDefault(); void submit(); }}>
          <label class="block"><span class="input-label">{t('streams.streamId')}</span><input class="input mt-1 w-full" bind:value={streamID} placeholder={t('streams.streamIdPlaceholder')} required pattern="[A-Za-z0-9_-]+" /></label>
          <label class="block"><span class="input-label">{t('streams.displayName')}</span><input class="input mt-1 w-full" bind:value={name} placeholder={t('streams.displayNamePlaceholder')} /></label>
          {#if selected === 'url'}<label class="block"><span class="input-label">{t('streams.sourceURL')}</span><input class="input mt-1 w-full" bind:value={sourceURL} placeholder={t('streams.sourceURLPlaceholder')} required /><p class="text-xs th-text-muted mt-1">{t('streams.sourceURLHint')}</p></label>{/if}
          <div class="flex justify-between items-center pt-2"><button type="button" class="btn btn-ghost" onclick={() => selected = null} disabled={saving}><ArrowLeft size={16} />{t('common.back')}</button><button class="btn btn-primary" disabled={saving || !streamID.trim() || (selected === 'url' && !sourceURL.trim())}>{#if saving}<LoaderCircle size={16} class="animate-spin" />{/if}{t('streams.create')}</button></div>
        </form>
      {:else if selected === 'emby'}
        <div class="space-y-5">
          <section class="rounded-xl border th-border p-4 sm:p-5 space-y-4">
            <h3 class="font-semibold th-text-primary">{t('emby.connection')}</h3>
            <div class="grid sm:grid-cols-2 gap-4">
              <label><span class="input-label">{t('emby.serverName')}</span><input class="input mt-1 w-full" bind:value={emby.name} placeholder="Emby" /></label>
              <label><span class="input-label">{t('emby.serverURL')}</span><input class="input mt-1 w-full" bind:value={emby.base_url} placeholder="http://emby.local:8096" /></label>
              <label><span class="input-label">{t('emby.userId')}</span><input class="input mt-1 w-full" bind:value={emby.user_id} /></label>
              <label><span class="input-label">{t('emby.accessToken')}</span><input class="input mt-1 w-full" type="password" bind:value={emby.token} placeholder={emby.has_token ? t('emby.tokenSaved') : ''} /></label>
            </div>
            <div class="flex justify-between items-center"><button class="btn btn-ghost" onclick={() => selected = null} disabled={saving}><ArrowLeft size={16} />{t('common.back')}</button><button class="btn btn-primary" onclick={() => void submit()} disabled={saving || !emby.base_url.trim() || !emby.user_id.trim() || (!emby.has_token && !emby.token)}>{#if saving}<LoaderCircle size={16} class="animate-spin" />{/if}{t('emby.connect')}</button></div>
          </section>
          {#if emby.configured}
            <section class="space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-3"><div><h3 class="font-semibold th-text-primary">{t('emby.library')}</h3><p class="text-xs th-text-muted mt-1">{t('emby.libraryHint')}</p></div><form class="flex gap-2" onsubmit={(event) => { event.preventDefault(); void searchEmby(); }}><input class="input input-sm w-48" bind:value={embyQuery} placeholder={t('emby.search')} /><button class="btn btn-secondary btn-sm" disabled={embyLoading}>{t('emby.searchButton')}</button></form></div>
              {#if embyLoading}<p class="text-sm th-text-muted">{t('common.loading')}</p>{:else if embyItems.length === 0}<p class="text-sm th-text-muted">{t('emby.noLibraryItems')}</p>{:else}<div class="grid sm:grid-cols-2 gap-3">{#each embyItems as item (item.id)}<article class="rounded-lg border th-border p-4 flex items-center justify-between gap-3"><div class="min-w-0"><div class="font-medium th-text-primary line-clamp-2">{item.name}</div><div class="text-xs th-text-muted mt-1">{item.item_type}</div></div><button class="btn btn-secondary btn-sm shrink-0" disabled={embyAdded.has(item.id)} onclick={() => void addEmby(item)}>{embyAdded.has(item.id) ? t('emby.added') : t('emby.addToNvr')}</button></article>{/each}</div>{/if}
            </section>
          {/if}
        </div>
      {:else}
        {#if !importJob}
          <form class="space-y-4" onsubmit={(event) => { event.preventDefault(); void submit(); }}>
            <label class="block"><span class="input-label">{t('iptv.importName')}</span><input class="input mt-1 w-full" bind:value={name} placeholder={t('iptv.importNameHint')} /></label>
            <label class="block"><span class="input-label">{t('iptv.playlistURL')}</span><input class="input mt-1 w-full" bind:value={playlistURL} placeholder="https://example.com/live.m3u" /></label>
            <label class="block"><span class="input-label">{t('iptv.playlistText')}</span><textarea class="input mt-1 w-full min-h-28" bind:value={playlistText} placeholder="#EXTM3U"></textarea></label>
            <p class="text-xs th-text-muted">{t('mediaIngest.setup.iptvHint')}</p>
            <div class="flex justify-between items-center pt-2"><button type="button" class="btn btn-ghost" onclick={() => selected = null} disabled={saving}><ArrowLeft size={16} />{t('common.back')}</button><button class="btn btn-primary" disabled={saving || (!playlistURL.trim() && !playlistText.trim())}>{#if saving}<LoaderCircle size={16} class="animate-spin" />{/if}{t('iptv.import')}</button></div>
          </form>
        {:else}
          <section class="space-y-4">
            <div class="flex items-start justify-between gap-3"><div><h3 class="font-semibold th-text-primary">{t('iptv.reviewTitle')}</h3><p class="text-sm th-text-muted mt-1">{importJob.name} · {importJob.status} · {importItems.length}/{importJob.total_items}</p></div><div class="flex gap-2"><button class="btn btn-secondary btn-sm" onclick={() => void refreshIPTVImport()} disabled={saving} aria-label={t('common.refresh')}><RefreshCw size={15} class={saving ? 'animate-spin' : ''} /></button><button class="btn btn-primary btn-sm" onclick={() => void commitIPTV()} disabled={saving || selectedItems.size === 0 || importJob.status !== 'ready'}><Check size={15} />{t('iptv.commit', { count: String(selectedItems.size) })}</button></div></div>
            <div class="max-h-[48vh] overflow-y-auto divide-y th-border rounded-lg border th-border">{#each importItems as item (item.id)}<label class="flex items-center gap-3 p-3"><input type="checkbox" checked={selectedItems.has(item.id)} disabled={item.drm || item.status === 'unsupported' || item.status === 'failed'} onchange={() => toggleIPTVItem(item)} /><span class="min-w-0 flex-1"><span class="block text-sm font-medium th-text-primary">{item.name}</span><span class="block text-xs th-text-muted mt-0.5">{item.group_name || t('iptv.ungrouped')} · {item.status}</span></span>{#if item.drm}<span class="badge badge-warning">DRM</span>{/if}</label>{/each}</div>
            {#if importJob.status === 'failed'}<p class="text-sm th-color-danger">{importJob.error || t('iptv.importFailed')}</p>{/if}
          </section>
        {/if}
      {/if}
      {#if error}<p class="mt-4 rounded-lg border border-red-300/40 bg-red-500/5 p-3 text-sm th-color-danger" role="alert">{error}</p>{/if}
    </div>
  </div>
</div>
