<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { Send } from '@lucide/svelte';
  import { api } from './api';
  import AgentBlocks from './AgentBlocks.svelte';
  import type { AgentConversation, AgentMessage, AgentRun } from './models';

  let {
    conversationId = '',
    reportId = '',
    enabled = false,
    onconversation = () => {},
    onarticle
  }: {
    conversationId?: string;
    reportId?: string;
    enabled?: boolean;
    onconversation?: (c: AgentConversation) => void;
    onarticle?: (event: MouseEvent, articleId: string, reportId: string, anchor: string) => void;
  } = $props();
  let conversation = $state<AgentConversation | null>(null);
  let messages = $state<AgentMessage[]>([]);
  let active = $state<AgentRun | null>(null);
  let question = $state(''),
    busy = $state(false),
    error = $state(''),
    more = $state(false);
  let thread: HTMLDivElement;
  let input: HTMLTextAreaElement;
  let loadedId: string | null = null;
  let generation = 0;
  const pending = (r: AgentRun | null) =>
    !!r && ['queued', 'running', 'waiting_retry'].includes(r.state);
  const nearBottom = () =>
    !thread || thread.scrollHeight - thread.scrollTop - thread.clientHeight < 100;
  async function scrollEnd() {
    await tick();
    thread?.scrollTo({ top: thread.scrollHeight });
  }
  export function focus() {
    input?.focus({ preventScroll: true });
  }

  $effect(() => {
    if (conversationId !== loadedId) void load(conversationId);
  });
  async function load(id: string) {
    loadedId = id;
    const request = ++generation;
    conversation = null;
    messages = [];
    active = null;
    error = '';
    question = '';
    more = false;
    busy = false;
    if (!id) return;
    busy = true;
    try {
      const page = await api.agentMessages(id);
      const run = page.at(-1)?.run_id ? await api.agentRun(page.at(-1)!.run_id) : null;
      if (request !== generation) return;
      // The ID is sufficient to continue an existing conversation; its context is stored server-side.
      conversation = { id, title: '', report_id: reportId, created_at: '', updated_at: '' };
      messages = page;
      active = run;
      more = page.length === 50;
      await scrollEnd();
    } catch (e) {
      if (request === generation) error = (e as Error).message;
    } finally {
      if (request === generation) busy = false;
    }
  }
  onMount(() => {
    let stopped = false,
      polling = false;
    const timer = setInterval(async () => {
      if (stopped || polling || !pending(active) || !conversation) return;
      polling = true;
      const id = conversation.id,
        request = generation;
      try {
        const run = await api.agentRun(active!.id);
        const page = !pending(run) ? await api.agentMessages(id) : null;
        if (stopped || request !== generation) return;
        const follow = nearBottom();
        active = run;
        if (page) {
          messages = page;
          more = page.length === 50;
        }
        if (follow) await scrollEnd();
      } catch (e) {
        if (!stopped && request === generation) error = (e as Error).message;
      } finally {
        polling = false;
      }
    }, 2000);
    return () => {
      stopped = true;
      generation++;
      clearInterval(timer);
    };
  });
  async function send() {
    if (busy || pending(active) || !enabled || !question.trim()) return;
    busy = true;
    error = '';
    const text = question.trim(),
      request = generation;
    try {
      if (!conversation) {
        const created = await api.agentConversation(reportId || undefined);
        if (request !== generation) return;
        conversation = created;
        loadedId = created.id;
        onconversation(created);
      }
      const target = conversation;
      const run = await api.agentSend(target.id, text);
      const page = await api.agentMessages(target.id);
      if (request !== generation) return;
      active = run;
      messages = page;
      question = '';
      onconversation(target);
      await scrollEnd();
      focus();
    } catch (e) {
      if (request === generation) error = (e as Error).message;
    } finally {
      if (request === generation) busy = false;
    }
  }
  async function control(action: 'cancel' | 'retry') {
    if (!active || busy) return;
    busy = true;
    error = '';
    try {
      active = await api.agentControl(active.id, action);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
  async function earlier() {
    if (!conversation || busy) return;
    busy = true;
    try {
      const height = thread.scrollHeight,
        top = thread.scrollTop;
      const page = await api.agentMessages(conversation.id, messages[0]?.id);
      messages = [...page, ...messages];
      more = page.length === 50;
      await tick();
      thread.scrollTop = top + thread.scrollHeight - height;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

<div class="chat-workspace">
  <div class="thread" bind:this={thread} role="log" aria-label="对话消息" aria-live="polite">
    {#if more}<button class="button secondary compact" disabled={busy} onclick={earlier}
        >更早消息</button
      >{/if}
    {#each messages as message}
      <article class="chat-message" class:user={message.role === 'user'}>
        <small>{message.role === 'user' ? '你' : '阅读助理'}</small>
        {#if message.text}<p>{message.text}</p>{/if}
        <AgentBlocks
          blocks={message.result?.blocks}
          {reportId}
          prefix={`chat-${message.id}`}
          {onarticle}
        />
        {#if message.result?.trace?.length}<details>
            <summary>检索依据</summary>{#each message.result.trace as trace}<p>{trace}</p>{/each}
          </details>{/if}
      </article>
    {:else}<div class="chat-empty">
        <p>{reportId ? '这份晨报里，有什么想进一步了解？' : '有什么想了解的文章或来源？'}</p>
        <small
          >{reportId
            ? '回答会结合本份报告的固定成员检索，并引用系统内的文章。'
            : '可以查询未读数量、来源状况，或文章中的具体内容。'}</small
        >
      </div>{/each}
    {#if pending(active)}<p class="chat-status" role="status">
        {active?.state === 'waiting_retry' ? '等待重试…' : '正在查阅资料并回答…'}
        <button class="text-link" disabled={busy} onclick={() => control('cancel')}>取消</button>
      </p>
    {:else if active?.state === 'failed'}<p class="error">
        回答未完成：{active.error}
        <button class="text-link" disabled={busy} onclick={() => control('retry')}>重试</button>
      </p>
    {:else if active?.state === 'cancelled'}<p class="chat-status">本次回答已取消。</p>{/if}
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <form
    class="composer"
    onsubmit={(event) => {
      event.preventDefault();
      void send();
    }}
  >
    <textarea
      bind:this={input}
      bind:value={question}
      aria-label="向阅读助理提问"
      disabled={busy || pending(active) || !enabled}
      placeholder={enabled ? '提问这份晨报或文章…' : '启用 Agent 后即可提问'}
      rows="3"
      onkeydown={(event) => {
        if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
          event.preventDefault();
          void send();
        }
      }}></textarea>
    <div class="composer-actions">
      <small>Enter 发送 · Shift+Enter 换行</small><button
        class="button compact"
        disabled={busy || pending(active) || !enabled || !question.trim()}
        ><Send size={14} aria-hidden="true" />发送</button
      >
    </div>
  </form>
</div>

<style>
  .chat-workspace {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 12px;
  }
  .thread {
    flex: 1;
    min-height: 0;
    overflow: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    padding: 4px 4px 12px;
  }
  .chat-message {
    padding: 18px 0;
    border-bottom: 1px solid var(--line);
    font-size: 14px;
    line-height: 1.8;
  }
  .chat-message.user {
    background: var(--surface);
    padding: 14px 16px;
    border: 0;
    border-radius: 10px;
    margin: 12px 0;
  }
  .chat-message > p {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin-top: 8px;
  }
  small,
  summary {
    color: var(--muted);
    font-size: 12px;
  }
  summary {
    cursor: pointer;
  }
  details {
    margin-top: 12px;
  }
  details p {
    font-size: 12px;
    color: var(--muted);
    margin-top: 8px;
    overflow-wrap: anywhere;
  }
  .chat-empty {
    padding: 36px 12px;
    line-height: 1.8;
  }
  .chat-empty p {
    font-size: 15px;
    margin-bottom: 8px;
  }
  .chat-empty small {
    display: block;
  }
  .chat-status {
    font-size: 13px;
    color: var(--muted);
    padding: 12px 0;
  }
  .text-link {
    color: var(--accent);
    text-decoration: underline;
  }
  .composer {
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg);
    padding: 10px;
    flex-shrink: 0;
  }
  .composer:focus-within {
    border-color: var(--accent);
  }
  .composer textarea {
    border: 0;
    background: transparent;
    padding: 4px;
    resize: vertical;
    max-height: 180px;
    font-size: 14px;
    line-height: 1.7;
    outline: none;
  }
  .composer-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-top: 8px;
  }
  .composer-actions small {
    font-size: 11px;
  }
  @media (pointer: coarse) {
    .composer textarea {
      font-size: 16px;
    }
    .composer-actions small {
      display: none;
    }
    .composer-actions {
      justify-content: flex-end;
    }
  }
</style>
