<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { PanelLeftOpen, PanelLeftClose, MessageCircle, X, CheckCheck } from '@lucide/svelte';
  import { api } from './api';
  import AgentConfig from './AgentConfig.svelte';
  import AgentBlocks from './AgentBlocks.svelte';
  import AgentChat from './AgentChat.svelte';
  import HoverDetails from './HoverDetails.svelte';
  import ReportDiagnostics from './ReportDiagnostics.svelte';
  import type { AgentRun, AgentMember, AgentConversation, AgentSettings } from './models';
  let {
    reportId = '',
    anchor = '',
    onreport,
    onarticle,
    ontogglesidebar,
    onreadchange = () => {},
    sidebarExpanded = true,
    sidebarLabel = '切换侧栏 (c)'
  }: {
    reportId?: string;
    anchor?: string;
    onreport: (id: string) => void;
    onarticle: (event: MouseEvent, articleId: string, reportId: string, anchor: string) => void;
    ontogglesidebar: () => void;
    onreadchange?: () => void | Promise<void>;
    sidebarExpanded?: boolean;
    sidebarLabel?: string;
  } = $props();
  let ready = $state(false),
    tab = $state('reports'),
    busy = $state(false),
    error = $state(''),
    settings = $state<AgentSettings | null>(null);
  let readFeedback = $state<Record<string, { message: string; failed: boolean }>>({});
  let readingGroup = $state('');
  let runs = $state<AgentRun[]>([]),
    report = $state<AgentRun | null>(null),
    members = $state<AgentMember[]>([]),
    showMembers = $state(false),
    moreMembers = $state(false),
    runMore = $state(false);
  let conversations = $state<AgentConversation[]>([]),
    conversation = $state<AgentConversation | null>(null),
    reportChatOpen = $state(false);
  let reportChat = $state<AgentChat>();
  let askButton = $state<HTMLButtonElement>();
  let conversationsEpoch = 0;
  const reportConversation = $derived(conversations.find((c) => c.report_id === report?.id));
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
      } catch (e) {
        if (!stopped) error = (e as Error).message;
      }
    }, 2000);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  });
  async function start() {
    try {
      await load();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      ready = true;
      await restoreAnchor();
    }
  }
  export async function refresh() {
    if (report) report = await api.agentRun(report.id);
  }
  let loadedReport = '';
  $effect(() => {
    if (ready && reportId && reportId !== loadedReport) void action(() => openReport(reportId));
  });
  async function restoreAnchor() {
    if (!anchor) return;
    await tick();
    const target = document.getElementById(anchor);
    const details = target?.closest('details');
    if (details) details.open = true;
    target?.scrollIntoView({ block: 'center' });
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
    const id = reportId;
    if (id) await openReport(id);
    else if (runs.length) await openReport(runs[0].id);
  }
  async function openReport(id: string) {
    const loaded = await api.agentRun(id);
    if (loaded.kind !== 'report') throw new Error('该任务不是晨报');
    if (report?.id !== id) reportChatOpen = false;
    report = loaded;
    loadedReport = id;
    readFeedback = {};
    members = [];
    showMembers = false;
    onreport(id);
    const member = /^(?:member|card)-(\d+)/.exec(anchor);
    if (member) {
      members = await api.agentMembers(id, String(BigInt(member[1]) - 1n));
      moreMembers = members.length === 50;
      showMembers = true;
    }
    await restoreAnchor();
  }
  async function generate() {
    reportChatOpen = false;
    report = await api.agentReport();
    runs = await api.agentRuns();
    showMembers = false;
    members = [];
    loadedReport = report.id;
    onreport(report.id);
    readFeedback = {};
  }
  async function markGroupRead(groupId: string) {
    if (!report) return;
    const id = report.id;
    readingGroup = groupId;
    delete readFeedback[groupId];
    try {
      const result = await api.agentMarkRead(id, groupId);
      if (report?.id === id) {
        readFeedback[groupId] = {
          message: `已将本组 ${result.marked_read} 篇总结文章标为已读`,
          failed: false
        };
      }
      const updated = await api.agentRun(id);
      if (report?.id === id) report = updated;
      await onreadchange();
    } catch (e) {
      if (report?.id === id) {
        readFeedback[groupId] = { message: (e as Error).message, failed: true };
      }
    } finally {
      readingGroup = '';
    }
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
  async function newConversation() {
    conversation = null;
    tab = 'chat';
  }
  async function conversationChanged(c: AgentConversation) {
    conversations = [c, ...conversations.filter((item) => item.id !== c.id)];
    if (!reportChatOpen) conversation = c;
    const epoch = ++conversationsEpoch;
    try {
      const updated = await api.agentConversations();
      if (epoch !== conversationsEpoch) return;
      conversations = updated;
      if (!reportChatOpen && conversation?.id === c.id)
        conversation = updated.find((item) => item.id === c.id) || c;
    } catch (e) {
      if (epoch === conversationsEpoch) error = (e as Error).message;
    }
  }
  async function openReportChat() {
    reportChatOpen = true;
    await tick();
    reportChat?.focus();
  }
  function closeReportChat() {
    reportChatOpen = false;
    askButton?.focus({ preventScroll: true });
  }
  async function switchTab(next: string) {
    reportChatOpen = false;
    tab = next;
    if (next !== 'config') settings = await api.agentSettings();
  }
  const fmt = (value: string) => new Date(value).toLocaleString('zh-CN');
</script>

<svelte:window
  onkeydown={(event) => {
    if (
      event.key === 'Escape' &&
      reportChatOpen &&
      !event.defaultPrevented &&
      !document.querySelector(':popover-open')
    ) {
      event.preventDefault();
      closeReportChat();
    }
  }}
/>

<div class="agent-page" class:has-chat={reportChatOpen}>
  <header>
    <button
      class="icon-button sidebar-toggle"
      onclick={ontogglesidebar}
      aria-label={sidebarLabel}
      title="切换侧栏 (c)"
      aria-controls="reading-sidebar"
      aria-expanded={sidebarExpanded}
    >
      {#if sidebarExpanded}<PanelLeftClose size={20} />{:else}<PanelLeftOpen size={20} />{/if}
    </button>
    <h1>晨报 · Agent</h1>
    <nav aria-label="Agent 页面">
      {#each [['reports', '晨报'], ['chat', '对话'], ['config', '模型配置']] as [id, label]}<button
          class:chosen={tab === id}
          aria-current={tab === id ? 'page' : undefined}
          onclick={() => action(() => switchTab(id))}>{label}</button
        >{/each}
    </nav>
  </header>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if !ready}<p class="empty">正在加载…</p>
  {:else if tab === 'config'}<main class="config-wrap"><AgentConfig /></main>
  {:else}
    {#if !settings?.enabled}<p class="notice">
        Agent 尚未启用。<button class="text-link" onclick={() => (tab = 'config')}
          >配置 Base URL、模型和 Key</button
        >
      </p>{/if}
    <main class="workspace">
      <aside class="history-panel" aria-label={tab === 'reports' ? '报告历史' : '对话历史'}>
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
              onclick={() => (conversation = c)}
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
                  bind:this={askButton}
                  class="button secondary"
                  aria-expanded={reportChatOpen}
                  aria-controls="report-chat"
                  disabled={busy || !settings?.enabled}
                  onclick={openReportChat}
                  ><MessageCircle size={15} aria-hidden="true" />围绕报告提问</button
                >
              </div>
            </div>
            <div class="coverage" role="status">
              <span>截止时未读 <b>{report.total}</b></span><span
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
              </p>{:else if report.error}<HoverDetails
                label="任务处理说明"
                heading="任务状态与技术报告"><ReportDiagnostics {report} /></HoverDetails
              >{/if}
            <AgentBlocks
              diagnosticReport={report}
              blocks={report.result?.blocks}
              reportId={report.id}
              prefix="overview"
              {onarticle}
            />
            {#each report.result?.groups || [] as group, i}{#if i === 0 || report.result.groups[i - 1].category_title !== group.category_title}<h2
                  class="category-heading"
                >
                  {group.category_title}
                </h2>{/if}
              <article class="source-group" aria-labelledby={`source-heading-${group.id}`}>
                <h3 id={`source-heading-${group.id}`}>
                  {group.title}
                  <small>{importanceName[group.importance]} · {group.total} 篇</small>
                </h3>
                <AgentBlocks
                  diagnosticReport={report}
                  diagnosticScope={group.title}
                  blocks={group.blocks}
                  reportId={report.id}
                  prefix={`source-${group.id}`}
                  {onarticle}
                />{#if !group.blocks?.length}<p class="muted">本来源尚无已完成的摘要。</p>{/if}
                <footer>
                  <div class="report-read-action">
                    <button
                      class="button secondary"
                      disabled={busy || activeState(report) || !group.unread_summarized}
                      onclick={() => action(() => markGroupRead(group.id))}
                      ><CheckCheck
                        size={16}
                        aria-hidden="true"
                      />{#if readingGroup === group.id}标记中…{:else}本组文章标为已读{#if group.unread_summarized}（{group.unread_summarized}）{/if}{/if}</button
                    >
                    <HoverDetails label="已读范围与技术说明" heading="本组文章批量已读 · 执行范围">
                      <p>仅处理“{group.title}”分组已总结的文章，其他分组保留原状态。</p>
                      <h4>成员判定</h4>
                      <p>
                        以服务端固定报告中的本组成员为准，覆盖所有分页和归档。成员需处于
                        summarized、reused 或 fallback
                        状态，且卡片包含非空的事实或分析块；有实质摘要的正文降级仍可纳入。
                      </p>
                      <p>
                        报告外新到的文章、pending、unavailable、已移除和仅 limitation
                        提示的文章排除。本组当前符合条件且仍未读：{group.unread_summarized ?? 0} 篇。
                      </p>
                      <h4>执行与重试</h4>
                      <p>
                        任务排队、处理中或等待重试时禁用操作。服务端在同一事务内更新阅读状态、版本和变更记录；已读文章跳过。同一幂等请求重试不会覆盖之后主动设为未读的选择。
                      </p>
                    </HoverDetails>
                  </div>
                  {#if readFeedback[group.id]}<p
                      class:notice={!readFeedback[group.id].failed}
                      class:error={readFeedback[group.id].failed}
                      role={readFeedback[group.id].failed ? 'alert' : 'status'}
                    >
                      {readFeedback[group.id].message}
                    </p>{/if}
                </footer>
              </article>{/each}
            <footer class="report-footer" role="group" aria-label="晨报末尾操作">
              <div class="report-end">
                <span></span><small>本份晨报到这里结束</small><span></span>
              </div>
              <HoverDetails label="覆盖统计与技术报告" heading="晨报生成 · 技术报告"
                ><ReportDiagnostics {report} /></HoverDetails
              >
            </footer>
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
                      ><a
                        id={`member-${m.article_id}`}
                        href={`/?scope=all&article=${encodeURIComponent(m.article_id)}&report=${report.id}&anchor=member-${m.article_id}`}
                        onclick={(e) =>
                          onarticle(e, m.article_id, report!.id, `member-${m.article_id}`)}
                        >{m.title}</a
                      ><small
                        >{m.feed_title} · {memberState[m.state]} · {depthName[m.depth] ||
                          '尚未读取'}</small
                      ></summary
                    ><AgentBlocks
                      diagnosticReport={report}
                      diagnosticScope={m.title}
                      blocks={m.card}
                      reportId={report.id}
                      prefix={`card-${m.article_id}`}
                      {onarticle}
                    />{#if m.error}<p class="muted">{m.error}</p>{/if}
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
          <div class="chat-heading">
            <h2>{conversation?.title || '与阅读助理对话'}</h2>
            {#if conversation?.report_id}<p class="muted">
                关联晨报 #{conversation.report_id}
              </p>{/if}
          </div>
          <div class="standalone-chat">
            <AgentChat
              conversationId={conversation?.id || ''}
              reportId={conversation?.report_id || ''}
              enabled={!!settings?.enabled}
              onconversation={conversationChanged}
            />
          </div>
        {/if}
      </section>
    </main>
  {/if}
  {#if reportChatOpen && report}
    <aside id="report-chat" class="report-chat" aria-label="围绕晨报提问">
      <div class="panel-heading">
        <div>
          <h2>围绕报告提问</h2>
          <p>晨报 #{report.id} · {fmt(report.cutoff)}</p>
        </div>
        <button class="icon-button" aria-label="关闭报告提问" onclick={closeReportChat}
          ><X size={18} /></button
        >
      </div>
      <AgentChat
        bind:this={reportChat}
        conversationId={reportConversation?.id || ''}
        reportId={report.id}
        enabled={!!settings?.enabled}
        onconversation={conversationChanged}
        {onarticle}
      />
    </aside>
  {/if}
</div>

<style>
  .agent-page {
    max-width: 1440px;
    margin: auto;
    padding: 28px 32px 48px;
    min-height: 100%;
  }
  .agent-page.has-chat {
    padding-right: 408px;
    max-width: none;
  }
  header {
    display: flex;
    align-items: center;
    gap: 16px;
    border-bottom: 1px solid var(--line);
    padding-bottom: 22px;
    flex-wrap: wrap;
  }
  h1 {
    font-size: 23px;
    font-weight: 650;
    line-height: 1.4;
    margin: 0;
  }
  h2 {
    font-size: 19px;
    font-weight: 650;
    line-height: 1.5;
  }
  h3 {
    font-size: 17px;
    font-weight: 600;
  }
  nav {
    display: flex;
    gap: 4px;
    margin-left: auto;
    background: var(--surface);
    border-radius: 8px;
    padding: 4px;
  }
  nav button {
    padding: 7px 12px;
    border-radius: 5px;
    font-size: 13px;
  }
  .chosen,
  .history-item.selected {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .workspace {
    display: grid;
    grid-template-columns: 166px minmax(0, 1fr);
    gap: 28px;
    margin-top: 28px;
  }
  .history-panel {
    border-right: 1px solid var(--line);
    padding-right: 18px;
    min-width: 0;
  }
  .history-panel > .button {
    width: 100%;
  }
  .history-panel h2 {
    font-size: 12px;
    color: var(--muted);
    font-weight: 500;
    margin: 24px 0 8px;
  }
  .history-item {
    display: flex;
    flex-direction: column;
    text-align: left;
    gap: 5px;
    padding: 12px 9px;
    border-radius: 7px;
    width: 100%;
    overflow-wrap: anywhere;
    font-size: 13px;
    line-height: 1.6;
  }
  small {
    font-size: 12px;
    color: var(--muted);
    font-weight: 400;
  }
  .content {
    min-width: 0;
    width: 100%;
    max-width: 850px;
    padding-bottom: 32px;
  }
  .report-heading {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 16px;
    flex-wrap: wrap;
  }
  .report-heading .muted {
    margin-top: 6px;
  }
  .report-heading .button-row {
    margin-top: 0;
    gap: 8px;
  }
  .muted {
    color: var(--muted);
    font-size: 13px;
    line-height: 1.75;
  }
  .coverage {
    display: flex;
    gap: 10px 20px;
    flex-wrap: wrap;
    padding: 16px 0;
    font-size: 12px;
    color: var(--muted);
    border-block: 1px solid var(--line);
    margin: 22px 0;
  }
  .coverage span {
    display: inline-flex;
    align-items: baseline;
    gap: 6px;
  }
  .coverage b {
    font-variant-numeric: tabular-nums;
    font-size: 14px;
    color: var(--text);
    font-weight: 600;
  }
  .source-group {
    padding: 16px 0 26px;
    border-bottom: 1px solid var(--line);
  }
  .source-group h3 {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 6px 14px;
    margin: 0 0 14px;
    overflow-wrap: anywhere;
  }
  .category-heading {
    margin: 36px 0 0;
    color: var(--accent);
    font-size: 14px;
    letter-spacing: 0.04em;
  }
  progress {
    width: 100%;
    height: 5px;
    accent-color: var(--accent);
  }
  summary {
    cursor: pointer;
    font-size: 14px;
    line-height: 1.7;
  }
  details {
    padding: 14px 0;
  }
  .members details {
    border-bottom: 1px solid var(--line);
  }
  .members small {
    display: block;
    margin-top: 6px;
  }
  .member-heading {
    margin-top: 20px;
  }
  .members a {
    color: var(--accent);
    overflow-wrap: anywhere;
  }
  .report-footer {
    margin-top: 36px;
  }
  .report-end {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 16px;
  }
  .report-end span {
    height: 1px;
    background: var(--line);
    flex: 1;
  }
  .report-end small {
    white-space: nowrap;
  }
  .report-read-action {
    display: flex;
    align-items: center;
    gap: 10px 18px;
    flex-wrap: wrap;
    margin: 24px 0 10px;
  }
  .config-wrap {
    padding-top: 28px;
    max-width: 700px;
    margin: auto;
  }
  .empty {
    padding: 44px 0;
    color: var(--muted);
    line-height: 1.8;
  }
  .empty h2 {
    color: var(--text);
    margin-bottom: 12px;
  }
  .text-link {
    color: var(--accent);
    text-decoration: underline;
  }
  .agent-page > .error,
  .agent-page > .notice {
    margin-top: 18px;
  }
  .chat-heading {
    margin-bottom: 18px;
  }
  .chat-heading .muted {
    margin-top: 6px;
  }
  .standalone-chat {
    height: max(420px, calc(100dvh - 240px));
    max-height: 850px;
  }
  .report-chat {
    position: fixed;
    inset: 0 0 0 auto;
    width: 376px;
    z-index: 5;
    display: flex;
    flex-direction: column;
    padding: 24px 20px 20px;
    background: var(--bg);
    border-left: 1px solid var(--line);
    box-shadow: -8px 0 30px #00000006;
  }
  .panel-heading {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    border-bottom: 1px solid var(--line);
    padding-bottom: 18px;
    margin-bottom: 16px;
  }
  .panel-heading h2 {
    font-size: 17px;
  }
  .panel-heading p {
    font-size: 12px;
    color: var(--muted);
    margin-top: 6px;
    line-height: 1.6;
  }
  .report-chat :global(.chat-workspace) {
    flex: 1;
  }
  @media (max-width: 1400px) {
    .has-chat .workspace {
      grid-template-columns: minmax(0, 1fr);
    }
    .has-chat .history-panel {
      display: none;
    }
  }
  @media (max-width: 1100px) {
    .agent-page {
      padding: 24px;
    }
    .workspace {
      grid-template-columns: minmax(0, 1fr);
      gap: 24px;
    }
    .history-panel {
      display: flex;
      align-items: center;
      gap: 8px;
      overflow: auto;
      border-right: 0;
      border-bottom: 1px solid var(--line);
      padding: 0 0 16px;
      max-height: 110px;
    }
    .history-panel h2 {
      display: none;
    }
    .history-panel .button,
    .history-item {
      flex: 0 0 auto;
      width: auto;
    }
    .history-item {
      max-width: 190px;
    }
  }
  @media (max-width: 900px) {
    .agent-page.has-chat {
      padding-right: 24px;
      padding-bottom: calc(72dvh + 32px);
    }
    .has-chat .history-panel {
      display: flex;
    }
    .report-chat {
      inset: auto 0 0;
      width: 100%;
      height: min(72dvh, 680px);
      border: 1px solid var(--line);
      border-radius: 16px 16px 0 0;
      padding: 18px 20px max(16px, env(safe-area-inset-bottom));
      box-shadow: 0 -12px 48px #0002;
    }
    .panel-heading {
      padding-bottom: 12px;
      margin-bottom: 8px;
    }
  }
  @media (max-width: 640px) {
    .agent-page,
    .agent-page.has-chat {
      padding: 20px 20px 40px;
    }
    .agent-page.has-chat {
      padding-bottom: calc(72dvh + 32px);
    }
    header {
      gap: 12px;
      padding-bottom: 18px;
    }
    h1 {
      font-size: 21px;
    }
    nav {
      width: 100%;
      margin: 0;
    }
    nav button {
      flex: 1;
    }
    .workspace {
      margin-top: 20px;
      gap: 20px;
    }
    .coverage {
      gap: 10px 16px;
      margin: 20px 0;
    }
    .source-group h3 {
      font-size: 17px;
    }
    .report-read-action {
      align-items: flex-start;
      flex-direction: column;
      gap: 8px;
    }
    .standalone-chat {
      height: max(400px, calc(100dvh - 355px));
    }
  }
</style>
