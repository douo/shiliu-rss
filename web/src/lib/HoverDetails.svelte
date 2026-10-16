<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import { Info, X } from '@lucide/svelte';

  let {
    label,
    heading = label,
    children
  }: { label: string; heading?: string; children: Snippet } = $props();
  const id = $props.id();
  let trigger: HTMLButtonElement;
  let panel: HTMLDivElement;
  let opened = $state(false);
  let pinned = false;
  let timer: ReturnType<typeof setTimeout>;

  function position() {
    if (!opened || !panel || !trigger) return;
    const rect = trigger.getBoundingClientRect();
    const width = panel.offsetWidth;
    const height = panel.offsetHeight;
    panel.style.left = `${Math.max(12, Math.min(rect.left, innerWidth - width - 12))}px`;
    const below = rect.bottom + 8;
    panel.style.top = `${Math.max(12, Math.min(below + height <= innerHeight - 12 ? below : rect.top - height - 8, innerHeight - height - 12))}px`;
  }
  function show() {
    clearTimeout(timer);
    if (!opened) {
      panel.showPopover();
      opened = true;
    }
    position();
  }
  function hide() {
    clearTimeout(timer);
    panel?.hidePopover();
    opened = false;
    pinned = false;
  }
  function leave() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (
        !pinned &&
        !panel.matches(':hover') &&
        !panel.contains(document.activeElement) &&
        document.activeElement !== trigger
      )
        hide();
    }, 180);
  }
  onMount(() => {
    return () => clearTimeout(timer);
  });
</script>

<svelte:window onresize={position} onscrollcapture={position} />
<span class="help-wrap">
  <button
    bind:this={trigger}
    class="help-trigger"
    aria-label={label}
    aria-expanded={opened}
    aria-controls={id}
    aria-haspopup="dialog"
    popovertarget={id}
    onmouseenter={show}
    onmouseleave={leave}
    onfocus={show}
    onblur={leave}
    onclick={(event) => {
      event.preventDefault();
      if (pinned) hide();
      else {
        pinned = true;
        show();
      }
    }}><Info size={15} aria-hidden="true" /><span>{label}</span></button
  >
</span>
<div
  bind:this={panel}
  {id}
  popover="auto"
  role="dialog"
  aria-modal="false"
  tabindex="0"
  aria-labelledby={`${id}-heading`}
  class="help-panel"
  onmouseenter={() => clearTimeout(timer)}
  onmouseleave={leave}
  onfocusout={leave}
  ontoggle={(event) => {
    opened = event.newState === 'open';
    if (!opened) pinned = false;
  }}
>
  <div class="help-heading">
    <h3 id={`${id}-heading`}>{heading}</h3>
    <button class="icon-button" aria-label="关闭技术说明" onclick={hide}><X size={16} /></button>
  </div>
  {@render children()}
</div>

<style>
  .help-wrap {
    display: inline-flex;
    max-width: 100%;
  }
  .help-trigger {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 2px;
    color: var(--muted);
    font-size: 12px;
    line-height: 1.5;
    text-align: left;
    text-decoration: underline dotted var(--line);
    text-underline-offset: 4px;
  }
  .help-trigger:hover,
  .help-trigger[aria-expanded='true'] {
    color: var(--accent);
  }
  .help-trigger :global(svg) {
    flex-shrink: 0;
  }
  .help-panel {
    position: fixed;
    inset: auto;
    margin: 0;
    width: min(440px, calc(100vw - 24px));
    max-height: min(560px, calc(100dvh - 24px));
    overflow: auto;
    overscroll-behavior: contain;
    padding: 20px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    color: var(--text);
    box-shadow: 0 12px 48px #0002;
    font-size: 13px;
    line-height: 1.75;
    overflow-wrap: anywhere;
  }
  .help-panel h3 {
    margin: 0;
    font-size: 15px;
    font-weight: 650;
  }
  .help-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 14px;
  }
  .help-panel :global(h4) {
    margin: 18px 0 6px;
    font-size: 13px;
    font-weight: 650;
  }
  .help-panel :global(p + p) {
    margin-top: 8px;
  }
  .help-panel :global(dl) {
    margin: 12px 0;
  }
  .help-panel :global(dl > div) {
    display: grid;
    grid-template-columns: 100px minmax(0, 1fr);
    gap: 12px;
    padding: 5px 0;
  }
  .help-panel :global(dt) {
    color: var(--muted);
  }
  .help-panel :global(dd) {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
  .help-panel :global(code) {
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .help-panel :global(ul) {
    padding-left: 18px;
    margin: 8px 0;
  }
  .help-panel :global(li + li) {
    margin-top: 6px;
  }
</style>
