<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import { Info, X } from '@lucide/svelte';

  let {
    label,
    heading = label,
    appearance = 'text',
    closeLabel = '关闭技术说明',
    children
  }: {
    label: string;
    heading?: string;
    appearance?: 'text' | 'status';
    closeLabel?: string;
    children: Snippet;
  } = $props();
  const id = $props.id();
  let trigger: HTMLButtonElement;
  let panel: HTMLDivElement;
  let opened = $state(false);
  let pinned = false;
  let restoringFocus = false;
  let timer: ReturnType<typeof setTimeout>;

  function position() {
    if (!opened || !panel || !trigger) return;
    const rect = trigger.getBoundingClientRect();
    const belowSpace = Math.max(0, innerHeight - rect.bottom - 20);
    const aboveSpace = Math.max(0, rect.top - 20);
    // Keep the trigger reachable even when touch synthesizes mouseenter before click.
    panel.style.maxHeight = `${Math.min(560, Math.max(belowSpace, aboveSpace))}px`;
    const width = panel.offsetWidth;
    const height = panel.offsetHeight;
    panel.style.left = `${Math.max(12, Math.min(rect.left, innerWidth - width - 12))}px`;
    const below = rect.bottom + 8;
    panel.style.top = `${Math.max(12, belowSpace >= height ? below : rect.top - height - 8)}px`;
  }
  function show() {
    clearTimeout(timer);
    if (!panel.matches(':popover-open')) {
      panel.showPopover();
    }
    opened = true;
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
    class:status-trigger={appearance === 'status'}
    aria-label={label}
    aria-expanded={opened}
    aria-controls={id}
    aria-haspopup="dialog"
    popovertarget={id}
    onmouseenter={show}
    onmouseleave={leave}
    onfocus={() => {
      if (!restoringFocus) show();
    }}
    onblur={leave}
    onclick={(event) => {
      event.preventDefault();
      if (pinned) hide();
      else {
        pinned = true;
        show();
      }
    }}
    >{#if appearance === 'status'}<span class="health-dot" aria-hidden="true"></span>{:else}<Info
        size={15}
        aria-hidden="true"
      /><span>{label}</span>{/if}</button
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
  onbeforetoggle={(event) => {
    // Native dismissal can restore focus to the trigger. That focus must not reopen
    // this popover while another source's popover is opening.
    if (event.newState === 'closed') {
      restoringFocus = true;
      queueMicrotask(() => {
        restoringFocus = false;
      });
    }
  }}
  ontoggle={() => {
    opened = panel.matches(':popover-open');
    if (!opened) pinned = false;
  }}
>
  <div class="help-heading">
    <h3 id={`${id}-heading`}>{heading}</h3>
    <button class="icon-button" aria-label={closeLabel} onclick={hide}><X size={16} /></button>
  </div>
  {@render children()}
</div>

<style>
  .help-wrap {
    display: inline-flex;
    max-width: 100%;
    flex-shrink: 0;
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
  .help-trigger.status-trigger {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 32px;
    min-height: 0;
    padding: 0;
    border-radius: 6px;
    color: var(--muted);
    text-decoration: none;
  }
  .health-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--warning);
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
  .help-panel .help-heading > .icon-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 30px;
    width: 30px;
    height: 30px;
    padding: 0;
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
