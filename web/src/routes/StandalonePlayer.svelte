<script lang="ts">
  import WebRTCPlayer from '../components/WebRTCPlayer.svelte';
  import { streamMediaURL } from '$lib/api';

  let { streamId }: { streamId: string } = $props();
</script>

<svelte:head>
  <title>{streamId} - WebRTC Player</title>
</svelte:head>

<div class="standalone-player">
  <WebRTCPlayer
    cameraId={streamId}
    cameraName={streamId}
    whepUrl={streamMediaURL(streamId, 'webrtc')}
    expanded={true}
  />
</div>

<style>
  .standalone-player {
    position: fixed;
    inset: 0;
    width: 100vw;
    height: 100dvh;
    overflow: hidden;
    background: #000;
  }
  .standalone-player :global(video) {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
</style>
