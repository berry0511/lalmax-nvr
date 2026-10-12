<script lang="ts">
  import { onMount } from 'svelte';
  import { addEmbyItem, browseEmbyLibrary, deleteEmbyItem, getEmbyConfig, listEmbyItems, saveEmbyConfig, updateEmbyItem } from '$lib/api';
  import type { EmbyConfig, EmbyManagedItem, EmbyMediaItem } from '$lib/api';
  import { t } from '$lib/i18n';
  import { showToast } from '$lib/toast';
  import { Film, RefreshCw, Search, Trash2, Radio, Repeat2, ExternalLink, Save } from 'lucide-svelte';

  let config = $state<EmbyConfig>({ name: '', base_url: '', user_id: '', token: '', configured: false, has_token: false });
  let saving = $state(false);
  let loading = $state(true);
  let searching = $state(false);
  let error = $state('');
  let query = $state('');
  let library = $state<EmbyMediaItem[]>([]);
  let managed = $state<EmbyManagedItem[]>([]);
  let busy = $state<Record<string, boolean>>({});

  async function load() {
    loading = true; error = '';
    try {
      config = await getEmbyConfig();
      managed = await listEmbyItems();
      if (config.configured) await searchLibrary();
    } catch (e) { error = e instanceof Error ? e.message : t('emby.loadFailed'); }
    finally { loading = false; }
  }

  async function searchLibrary() {
    searching = true;
    try { library = (await browseEmbyLibrary({ q: query.trim() || undefined, limit: 100 })).items; }
    catch (e) { error = e instanceof Error ? e.message : t('emby.libraryFailed'); }
    finally { searching = false; }
  }

  async function saveConfig() {
    saving = true; error = '';
    try {
      config = await saveEmbyConfig({ name: config.name, base_url: config.base_url, user_id: config.user_id, token: config.token || '' });
      config.token = '';
      await searchLibrary();
      showToast(t('emby.connected'), 'success');
    } catch (e) { error = e instanceof Error ? e.message : t('emby.connectFailed'); }
    finally { saving = false; }
  }

  async function add(item: EmbyMediaItem) {
    busy[item.id] = true;
    try { await addEmbyItem(item.id); await refreshManaged(); showToast(t('emby.added'), 'success'); }
    catch (e) { showToast(e instanceof Error ? e.message : t('emby.addFailed'), 'error'); }
    finally { busy[item.id] = false; }
  }

  async function refreshManaged() { managed = await listEmbyItems(); }

  async function setLoop(item: EmbyManagedItem, loop: boolean) {
    try { await updateEmbyItem(item.id, { loop }); await refreshManaged(); }
    catch (e) { showToast(e instanceof Error ? e.message : t('emby.updateFailed'), 'error'); }
  }

  async function togglePublish(item: EmbyManagedItem) {
    busy[item.id] = true;
    try { await updateEmbyItem(item.id, { publish_enabled: !item.publish_enabled, loop: item.loop }); await refreshManaged(); }
    catch (e) { showToast(e instanceof Error ? e.message : t('emby.publishFailed'), 'error'); }
    finally { busy[item.id] = false; }
  }

  async function remove(item: EmbyManagedItem) {
    busy[item.id] = true;
    try { await deleteEmbyItem(item.id); await refreshManaged(); }
    catch (e) { showToast(e instanceof Error ? e.message : t('emby.removeFailed'), 'error'); }
    finally { busy[item.id] = false; }
  }

  function isManaged(item: EmbyMediaItem) { return managed.some((entry) => entry.emby_item_id === item.id); }
  function duration(item: EmbyMediaItem) {
    if (!item.run_time_ticks) return '';
    const totalSeconds = Math.floor(item.run_time_ticks / 10_000_000);
    const hours = Math.floor(totalSeconds / 3600);
    const mins = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    if (hours > 0) return `${hours}:${String(mins).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
    return `${mins}:${String(seconds).padStart(2, '0')}`;
  }

  onMount(() => { void load(); });
</script>

<div class="p-6 max-w-6xl mx-auto space-y-6">
  <header class="flex items-center gap-3">
    <Film size={26} class="th-text-primary" />
    <div><h1 class="text-2xl font-bold th-text-primary">{t('emby.title')}</h1><p class="text-sm th-text-muted mt-1">{t('emby.subtitle')}</p></div>
  </header>

  {#if error}<div class="alert alert-error">{error}</div>{/if}

  <section class="card p-5 space-y-4">
    <h2 class="font-semibold th-text-primary">{t('emby.connection')}</h2>
    <div class="grid md:grid-cols-2 gap-4">
      <label><span class="input-label">{t('emby.serverName')}</span><input class="input mt-1 w-full" bind:value={config.name} placeholder="Emby" /></label>
      <label><span class="input-label">{t('emby.serverURL')}</span><input class="input mt-1 w-full" bind:value={config.base_url} placeholder="http://emby.local:8096" /></label>
      <label><span class="input-label">{t('emby.userId')}</span><input class="input mt-1 w-full" bind:value={config.user_id} /></label>
      <label><span class="input-label">{t('emby.accessToken')}</span><input class="input mt-1 w-full" type="password" bind:value={config.token} placeholder={config.has_token ? t('emby.tokenSaved') : ''} /></label>
    </div>
    <div class="flex items-center gap-3">
      <button class="btn btn-primary btn-sm" onclick={saveConfig} disabled={saving || !config.base_url || !config.user_id}>
        <Save size={15} /> {saving ? t('common.loading') : t('emby.connect')}
      </button>
      {#if config.configured}<span class="text-xs th-text-muted">{t('emby.connectedHint')}</span>{/if}
    </div>
  </section>

  {#if config.configured}
    <section class="card p-5 space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div><h2 class="font-semibold th-text-primary">{t('emby.library')}</h2><p class="text-xs th-text-muted mt-1">{t('emby.libraryHint')}</p></div>
        <form class="flex gap-2" onsubmit={(e) => { e.preventDefault(); void searchLibrary(); }}>
          <input class="input input-sm w-56" bind:value={query} placeholder={t('emby.search')} />
          <button class="btn btn-secondary btn-sm" disabled={searching}><Search size={15} />{t('emby.searchButton')}</button>
        </form>
      </div>
      {#if searching}<div class="text-sm th-text-muted">{t('common.loading')}</div>
      {:else if library.length === 0}<div class="text-sm th-text-muted">{t('emby.noLibraryItems')}</div>
      {:else}
        <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {#each library as item (item.id)}
            <article class="rounded-lg border th-border p-4 flex flex-col gap-3">
              <div class="flex-1"><div class="font-medium th-text-primary line-clamp-2">{item.name}</div><div class="text-xs th-text-muted mt-1">{item.item_type}{duration(item) ? ` · ${duration(item)}` : ''}</div>
                {#if item.overview}<p class="text-xs th-text-secondary mt-2 line-clamp-3">{item.overview}</p>{/if}
              </div>
              <button class="btn btn-secondary btn-sm self-start" disabled={isManaged(item) || busy[item.id]} onclick={() => add(item)}>
                {isManaged(item) ? t('emby.added') : busy[item.id] ? t('common.loading') : t('emby.addToNvr')}
              </button>
            </article>
          {/each}
        </div>
      {/if}
    </section>

    <section class="card p-5 space-y-4">
      <div class="flex items-center justify-between"><h2 class="font-semibold th-text-primary">{t('emby.managedItems')}</h2><button class="btn btn-ghost btn-sm" onclick={refreshManaged}><RefreshCw size={15} /></button></div>
      {#if managed.length === 0}<p class="text-sm th-text-muted">{t('emby.noManagedItems')}</p>
      {:else}
        <div class="space-y-3">
          {#each managed as item (item.id)}
            <article class="rounded-lg border th-border p-4 flex flex-col md:flex-row md:items-center gap-4">
              <div class="flex-1 min-w-0"><div class="font-medium th-text-primary truncate">{item.name}</div><div class="text-xs th-text-muted mt-1">{item.item_type} · {item.stream_id}</div></div>
              <label class="flex items-center gap-2 text-sm th-text-secondary"><input type="checkbox" checked={item.loop} onchange={(e) => setLoop(item, e.currentTarget.checked)} /><Repeat2 size={15} />{t('emby.loop')}</label>
              <div class="flex gap-2">
                <button class="btn btn-primary btn-sm" disabled={busy[item.id]} onclick={() => togglePublish(item)}><Radio size={15} />{item.publish_enabled ? t('emby.stopPublish') : t('emby.publish')}</button>
                <a class="btn btn-ghost btn-sm" href={`#/streams/${encodeURIComponent(item.stream_id)}`} title={t('emby.viewStream')}><ExternalLink size={15} /></a>
                <button class="btn btn-ghost btn-sm text-error" disabled={busy[item.id] || item.publish_enabled} onclick={() => remove(item)} title={t('emby.remove')}><Trash2 size={15} /></button>
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </section>
  {/if}
  {#if loading}<div class="text-sm th-text-muted">{t('common.loading')}</div>{/if}
</div>
