<script lang="ts">
  import { Rss } from '@lucide/svelte';
  import { externalURL } from './links';
  import type { Feed } from './models';
  let { feed }: { feed?: Feed } = $props();
  let failedSource = $state('');
  const source = $derived.by(() => {
    const safe = externalURL(feed?.site_url || '') || externalURL(feed?.url || '');
    return safe ? `${new URL(safe).origin}/favicon.ico` : '';
  });
</script>

<span class="feed-icon" aria-hidden="true">
  {#if source && failedSource !== source}
    <img
      src={source}
      alt=""
      loading="lazy"
      referrerpolicy="no-referrer"
      onerror={() => {
        failedSource = source;
      }}
    />
  {:else}
    <Rss size={14} />
  {/if}
</span>
