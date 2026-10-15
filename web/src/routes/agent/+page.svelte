<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../../lib/api';
  import AgentConfig from '../../lib/AgentConfig.svelte';
  import AgentBlocks from '../../lib/AgentBlocks.svelte';
  import type {
    AgentRun,
    AgentMember,
    AgentConversation,
    AgentMessage,
    AgentSettings
  } from '../../lib/models';
  let ready = $state(false),
    authenticated = $state(false),
    password = $state(''),
    tab = $state('reports'),
    busy = $state(false),
    error = $state(''),
    settings = $state<AgentSettings | null>(null);
  let runs = $state<AgentRun[]>([]),
    report = $state<AgentRun | null>(null),
    members = $state<AgentMember[]>([]),
    showMembers = $state(false),
    moreMembers = $state(false),
    runMore = $state(false);
  let conversations = $state<AgentConversation[]>([]),
    conversation = $state<AgentConversation | null>(null),
    messages = $state<AgentMessage[]>([]),
    question = $state(''),
    active = $state<AgentRun | null>(null),
    moreMessages = $state(false);
  const activeState = (r: AgentRun | null) =>
    !!r && ['queued', 'running', 'waiting_retry'].includes(r.state);
  const stateName: Record<string, string> = {
    queued: '排队中',
    running: '处理中',
    waiting_retry: '等待重试',
    complete: '完成',
    partial: '部分完成',
    failed: '失败',
    cancelled: '已取消'
  };
  const importanceName: Record<string, string> = { high: '重要', normal: '普通', low: '次要' };
  const depthName: Record<string, string> = {
    fulltext: '全文',
    feed_excerpt: '正文截取',
    title: '仅标题',
    saved_summary: '历史摘要'
  };
  const memberState: Record<string, string> = {
    pending: '待处理',
    summarized: '已总结',
    reused: '复用摘要',
    fallback: '全文缺失，降级处理',
    unavailable: '不可用'
  };
  onMount(() => {
    let stopped = false;
    void start();
    const timer = setInterval(async () => {
      try {
        if (activeState(report)) {
          const r = await api.agentRun(report!.id);
          if (!stopped) report = r;
        }
        if (activeState(active)) {
          const r = await api.agentRun(active!.id);
          if (!stopped) {
            active = r;
            if (!activeState(r) && conversation)
              messages = await api.agentMessages(conversation.id);
          }
        }
      } catch (e) {
        if (!stopped) error = (e as Error).message;
      }
    }, 2000);
    const expire = () => {
      authenticated = false;
      settings = null;
      runs = [];
      report = null;
      members = [];
      conversations = [];
      conversation = null;
      messages = [];
      active = null;
    };
    const channel = 'BroadcastChannel' in window ? new BroadcastChannel('shiliu-session') : null;
    if (channel)
      channel.onmessage = (e) => {
        if (e.data === 'logout') expire();
      };
    window.addEventListener('shiliu:session-expired', expire);
    return () => {
      stopped = true;
      clearInterval(timer);
      window.removeEventListener('shiliu:session-expired', expire);
      channel?.close();
    };
  });
  async function start() {
    try {
      authenticated = (await api.session()).authenticated;
      if (authenticated) await load();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      ready = true;
    }
  }
  async function action(fn: () => Promise<void>) {
    busy = true;
    error = '';
    try {
      await fn();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
  async function load() {
    settings = await api.agentSettings();
    runs = await api.agentRuns();
    runMore = runs.length === 30;
    conversations = await api.agentConversations();
    const id = new URLSearchParams(location.search).get('report');
    if (id) await openReport(id);
    else if (runs.length) await openReport(runs[0].id);
  }
  async function openReport(id: string) {
    report = await api.agentRun(id);
    members = [];
    showMembers = false;
    history.replaceState(null, '', `/agent?report=${encodeURIComponent(id)}`);
  }
  async function generate() {
    report = await api.agentReport();
    runs = await api.agentRuns();
    showMembers = false;
    members = [];
  }
  async function control(action: 'cancel' | 'retry') {
    if (report) report = await api.agentControl(report.id, action);
  }
  async function listMembers(more = false) {
    if (!report) return;
    const page = await api.agentMembers(report.id, more ? members.at(-1)?.article_id : '');
    members = more ? [...members, ...page] : page;
    moreMembers = page.length === 50;
    showMembers = true;
  }
  async function newConversation(reportID?: string) {
    conversation = await api.agentConversation(reportID);
    conversations = await api.agentConversations();
    messages = [];
    active = null;
    tab = 'chat';
    question = '';
  }
  async function openConversation(c: AgentConversation) {
    conversation = c;
    messages = await api.agentMessages(c.id);
    moreMessages = messages.length === 50;
    const last = messages.at(-1);
    active = last?.run_id ? await api.agentRun(last.run_id) : null;
  }
  async function send() {
    if (!question.trim() || activeState(active)) return;
    const text = question.trim();
    if (!conversation) await newConversation();
    active = await api.agentSend(conversation!.id, text);
    question = '';
    messages = await api.agentMessages(conversation!.id);
    conversations = await api.agentConversations();
  }
  async function switchTab(next: string) {
    tab = next;
    if (next !== 'config') settings = await api.agentSettings();
  }
  const fmt = (value: string) => new Date(value).toLocaleString('zh-CN');
</script>

<svelte:head
  ><title>Agent · 拾流 RSS</title><meta
    name="description"
    content="未读晨报与阅读助理"
  /></svelte:head
>
<div class="agent-page">
  <header>
    <a class="back" href="/">← 返回阅读</a>
    <h1>阅读助理</h1>
    <nav aria-label="Agent 页面">
      {#each [['reports', '晨报'], ['chat', '对话'], ['config', '模型配置']] as [id, label]}<button
          class:chosen={tab === id}
          onclick={() => action(() => switchTab(id))}>{label}</button
        >{/each}
    </nav>
  </header>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if !ready}<p class="empty">正在加载…</p>
  {:else if !authenticated}<form
      class="login"
      onsubmit={(e) => {
        e.preventDefault();
        action(async () => {
          await api.login(password);
          password = '';
          authenticated = true;
          await load();
        });
      }}
    >
      <h2>登录拾流</h2>
      <label
        >密码<input
          type="password"
          autocomplete="current-password"
          bind:value={password}
          required
        /></label
      ><button class="button" disabled={busy}>登录</button>
    </form>
  {:else if tab === 'config'}<main class="config-wrap"><AgentConfig /></main>
  {:else}
    {#if !settings?.enabled}<p class="notice">
        Agent 尚未启用。<button class="text-link" onclick={() => (tab = 'config')}
          >配置 Base URL、模型和 Key</button
        >
      </p>{/if}
    <main class="workspace">
      <aside>
        {#if tab === 'reports'}<button
            class="button"
            disabled={busy || !settings?.enabled}
            onclick={() => action(generate)}>生成未读晨报</button
          >
          <h2>报告历史</h2>
          {#each runs as r}<button
              class="history-item"
              class:selected={report?.id === r.id}
              onclick={() => action(() => openReport(r.id))}
              ><span>{fmt(r.cutoff)}</span><small>{stateName[r.state]} · {r.total} 篇未读</small
              ></button
            >{:else}<p class="muted">尚无报告</p>{/each}{#if runMore}<button
              class="button secondary"
              onclick={() =>
                action(async () => {
                  const page = await api.agentRuns('report', runs.at(-1)?.id);
                  runs = [...runs, ...page];
                  runMore = page.length === 30;
                })}>更早报告</button
            >{/if}
        {:else}<button
            class="button"
            disabled={busy || !settings?.enabled}
            onclick={() => action(() => newConversation())}>新对话</button
          >
          <h2>对话历史</h2>
          {#each conversations as c}<button
              class="history-item"
              class:selected={conversation?.id === c.id}
              onclick={() => action(() => openConversation(c))}
              ><span>{c.title}</span><small
                >{fmt(c.updated_at)}{c.report_id ? ' · 报告对话' : ''}</small
              ></button
            >{/each}{#if conversations.length >= 50}<button
              class="button secondary"
              onclick={() =>
                action(async () => {
                  conversations = [
                    ...conversations,
                    ...(await api.agentConversations(conversations.at(-1)?.id))
                  ];
                })}>更早对话</button
            >{/if}{/if}
      </aside>
      <section class="content">
        {#if tab === 'reports'}
          {#if report}<div class="report-heading">
              <div>
                <h2>未读晨报</h2>
                <p class="muted">截止 {fmt(report.cutoff)} · {stateName[report.state]}</p>
              </div>
              <div class="button-row">
                {#if activeState(report)}<button
                    class="button secondary"
                    disabled={busy}
                    onclick={() => action(() => control('cancel'))}>取消</button
                  >{:else if report.state !== 'complete'}<button
                    class="button secondary"
                    disabled={busy || !settings?.enabled}
                    onclick={() => action(() => control('retry'))}>续作 / 重试</button
                  >{/if}<button
                  class="button secondary"
                  disabled={busy || !settings?.enabled}
                  onclick={() => action(() => newConversation(report!.id))}>围绕报告提问</button
                >
              </div>
            </div>
            <div class="coverage" role="status">
              <span>全部未读 <b>{report.total}</b></span><span
                >已处理 <b>{report.completed}</b></span
              >{#if report.result?.counts}{#each [['summarized', '新总结'], ['reused', '复用'], ['fulltext', '全文'], ['fallback', '降级'], ['unavailable', '不可用'], ['pending', '待处理']] as [id, label]}<span
                    >{label} <b>{report.result.counts[id] || 0}</b></span
                  >{/each}{/if}
            </div>
            {#if activeState(report)}<progress
                max={Math.max(report.total, 1)}
                value={report.completed}
              ></progress>
              <p class="muted">
                后台持续处理，可以离开此页面。{report.error === 'waiting_for_fulltext'
                  ? '正在等待重要源的全文。'
                  : report.error}
              </p>{:else if report.error}<p class="notice">未完成原因：{report.error}</p>{/if}
            <AgentBlocks blocks={report.result?.blocks} />
            {#each report.result?.groups || [] as group, i}{#if i === 0 || report.result.groups[i - 1].category_title !== group.category_title}<h2
                  class="category-heading"
                >
                  {group.category_title}
                </h2>{/if}
              <article class="source-group">
                <h3>
                  {group.title}
                  <small>{importanceName[group.importance]} · {group.total} 篇未读</small>
                </h3>
                <AgentBlocks blocks={group.blocks} />{#if !group.blocks?.length}<p class="muted">
                    本来源尚无已完成的摘要。
                  </p>{/if}
              </article>{/each}
            {#if report.result?.trace?.length}<details>
                <summary>处理说明</summary>{#each report.result.trace as t}<p class="muted">
                    {t}
                  </p>{/each}
              </details>{/if}
            <div class="member-heading">
              <button
                class="button secondary"
                disabled={busy}
                onclick={() => action(() => listMembers())}>查看文章与覆盖明细</button
              >
            </div>
            {#if showMembers}<div class="members">
                {#each members as m}<details>
                    <summary
                      ><a href={`/?scope=all&article=${encodeURIComponent(m.article_id)}`}
                        >{m.title}</a
                      ><small
                        >{m.feed_title} · {memberState[m.state]} · {depthName[m.depth] ||
                          '尚未读取'}</small
                      ></summary
                    ><AgentBlocks blocks={m.card} />{#if m.error}<p class="muted">{m.error}</p>{/if}
                  </details>{/each}{#if moreMembers}<button
                    class="button secondary"
                    disabled={busy}
                    onclick={() => action(() => listMembers(true))}>加载更多文章</button
                  >{/if}
              </div>{/if}
          {:else}<div class="empty">
              <h2>每天的未读，整理成一份晨报</h2>
              <p>按分类、来源重要度和未读量聚合。默认每天北京时间 08:00 生成，也可立即生成。</p>
            </div>{/if}
        {:else}
          <h2>{conversation?.title || '与阅读助理对话'}</h2>
          {#if conversation?.report_id}<p class="muted">
              本次对话围绕报告 #{conversation.report_id} 的固定成员。
            </p>{/if}
          {#if moreMessages && conversation}<button
              class="button secondary"
              onclick={() =>
                action(async () => {
                  const page = await api.agentMessages(conversation!.id, messages[0]?.id);
                  messages = [...page, ...messages];
                  moreMessages = page.length === 50;
                })}>更早消息</button
            >{/if}
          <div class="messages" aria-live="polite">
            {#each messages as m}<article class:user={m.role === 'user'} class="chat-message">
                <small>{m.role === 'user' ? '你' : '阅读助理'}</small>{#if m.text}<p>
                    {m.text}
                  </p>{/if}<AgentBlocks
                  blocks={m.result?.blocks}
                />{#if m.result?.trace?.length}<details>
                    <summary>检索依据</summary>{#each m.result.trace as t}<p class="muted">
                        {t}
                      </p>{/each}
                  </details>{/if}
              </article>{:else}<p class="empty">
                可以查询未读数量、来源状况，或提问文章里的具体内容。完整未读汇总请生成晨报。
              </p>{/each}
          </div>
          {#if activeState(active)}<p class="notice" role="status">{stateName[active!.state]}…</p>
            <button
              class="button secondary"
              onclick={() =>
                action(async () => {
                  active = await api.agentControl(active!.id, 'cancel');
                })}>取消回答</button
            >{:else if active?.state === 'failed'}<p class="notice">{active.error}</p>
            <button
              class="button secondary"
              onclick={() =>
                action(async () => {
                  active = await api.agentControl(active!.id, 'retry');
                })}>重试回答</button
            >{/if}
          <form
            class="composer"
            onsubmit={(e) => {
              e.preventDefault();
              action(send);
            }}
          >
            <textarea
              aria-label="向阅读助理提问"
              disabled={busy || activeState(active) || !settings?.enabled}
              placeholder="提问… Enter 发送，Shift+Enter 换行"
              rows="3"
              bind:value={question}
              onkeydown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
                  e.preventDefault();
                  if (!busy && !activeState(active) && settings?.enabled) action(send);
                }
              }}></textarea><button
              class="button"
              disabled={busy || activeState(active) || !settings?.enabled || !question.trim()}
              >发送</button
            >
          </form>
        {/if}
      </section>
    </main>
  {/if}
</div>

<style>
  .agent-page {
    max-width: 1440px;
    margin: auto;
    padding: 25px 32px;
    min-height: 100vh;
  }
  header {
    display: flex;
    align-items: center;
    gap: 25px;
    border-bottom: 1px solid var(--line);
    padding-bottom: 20px;
    flex-wrap: wrap;
  }
  h1 {
    font-size: 21px;
    margin: 0;
  }
  h2 {
    font-size: 18px;
  }
  h3 {
    font-size: 16px;
  }
  .back {
    color: var(--muted);
    font-size: 13px;
  }
  nav {
    display: flex;
    gap: 6px;
    margin-left: auto;
  }
  nav button {
    padding: 8px 15px;
    border-radius: 6px;
  }
  .chosen,
  .history-item.selected {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .workspace {
    display: grid;
    grid-template-columns: 245px minmax(0, 1fr);
    gap: 35px;
    margin-top: 25px;
  }
  aside {
    border-right: 1px solid var(--line);
    padding-right: 22px;
  }
  aside h2 {
    font-size: 12px;
    color: var(--muted);
    margin: 25px 0 10px;
  }
  .history-item {
    display: flex;
    flex-direction: column;
    text-align: left;
    gap: 5px;
    padding: 12px 9px;
    border-radius: 6px;
    width: 100%;
    overflow-wrap: anywhere;
  }
  .history-item small,
  small {
    font-size: 12px;
    color: var(--muted);
    font-weight: 400;
  }
  .content {
    min-width: 0;
    max-width: 950px;
    padding-bottom: 50px;
  }
  .report-heading {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 15px;
    flex-wrap: wrap;
  }
  .muted {
    color: var(--muted);
    font-size: 13px;
    line-height: 1.7;
  }
  .coverage {
    display: flex;
    gap: 15px;
    flex-wrap: wrap;
    padding: 16px 0;
    font-size: 13px;
    border-block: 1px solid var(--line);
    margin: 20px 0;
  }
  .coverage b {
    font-variant-numeric: tabular-nums;
  }
  .source-group {
    padding: 7px 0 20px;
    border-bottom: 1px solid var(--line);
  }
  .source-group h3 small {
    margin-left: 10px;
  }
  .category-heading {
    margin: 30px 0 8px;
  }
  progress {
    width: 100%;
    height: 5px;
    accent-color: var(--accent);
  }
  summary {
    cursor: pointer;
    font-size: 13px;
  }
  details {
    padding: 10px 0;
  }
  .members details {
    border-bottom: 1px solid var(--line);
  }
  .members small {
    display: block;
    margin-top: 5px;
  }
  .member-heading {
    margin-top: 30px;
  }
  .members a {
    color: var(--accent);
  }
  .config-wrap {
    padding-top: 30px;
    max-width: 700px;
    margin: auto;
  }
  .empty {
    padding: 45px 0;
    color: var(--muted);
    line-height: 1.8;
  }
  .chat-message {
    padding: 18px 0;
    border-bottom: 1px solid var(--line);
  }
  .chat-message.user {
    background: var(--accent-soft);
    padding: 15px;
    border-radius: 8px;
    margin-top: 16px;
  }
  .chat-message > p {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .composer {
    display: flex;
    gap: 12px;
    align-items: flex-end;
    margin-top: 25px;
  }
  .composer textarea {
    flex: 1;
    min-width: 0;
    resize: vertical;
  }
  .login {
    max-width: 380px;
    margin: 80px auto;
    display: grid;
    gap: 20px;
  }
  .login label {
    display: grid;
    gap: 10px;
  }
  .text-link {
    color: var(--accent);
    text-decoration: underline;
  }
  .button-row {
    flex-wrap: wrap;
  }
  @media (max-width: 750px) {
    .agent-page {
      padding: 18px 16px;
    }
    header {
      gap: 15px;
    }
    nav {
      width: 100%;
      margin: 0;
    }
    .workspace {
      grid-template-columns: 1fr;
      gap: 20px;
    }
    aside {
      border-right: 0;
      border-bottom: 1px solid var(--line);
      padding-bottom: 15px;
      max-height: 230px;
      overflow: auto;
    }
    .report-heading .button-row {
      gap: 6px;
    }
  }
</style>
