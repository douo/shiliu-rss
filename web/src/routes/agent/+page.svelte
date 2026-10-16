<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  onMount(() => {
    const params = new URLSearchParams(location.search);
    params.set('view', 'agent');
    void goto(`/?${params}`, { replace: true });
  });
</script>

<p class="empty">正在打开晨报…</p>
