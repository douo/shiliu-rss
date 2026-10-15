<script lang="ts">
  import {
    Plus,
    RefreshCw,
    Download,
    Upload,
    X,
    KeyRound,
    Check,
    Trash2,
    Rss,
    ShieldCheck
  } from '@lucide/svelte';
  import type { Feed, Category, Token } from './models';
  import { api } from './api';
  import { modal } from './modal';
  let {
    feeds,
    categories,
    onupdate,
    onclose
  }: { feeds: Feed[]; categories: Category[]; onupdate: () => Promise<void>; onclose: () => void } =
    $props();
  let tab = $state('feeds'),
    editing = $state<Partial<Feed> | null>(null),
    busy = $state(false),
    error = $state(''),
    message = $state('');
  let tokens = $state<Token[]>([]),
    tokenName = $state(''),
    scopes = $state(['articles:read']),
    newToken = $state(''),
    categoryTitle = $state('');
  let preview = $state<{ articles: number; contents: number; bytes: number } | null>(null),
    health = $state<Record<string, unknown> | null>(null);
  let previewSignature = '';
  $effect(() => {
    const signature = JSON.stringify(editing);
    if (signature !== previewSignature) {
      preview = null;
      previewSignature = signature;
    }
  });
  const original = $derived(feeds.find((feed) => feed.id === editing?.id));
  const policyChanged = $derived(
    !!original &&
      !!editing &&
      (original.archive !== editing.archive ||
        original.read_days !== editing.read_days ||
        original.unread_days !== editing.unread_days)
  );
  const permissions = [
    ['articles:read', '读取文章与归档'],
    ['reading:write', '更改已读、收藏与笔记'],
    ['artifacts:write', '写入摘要与分析'],
    ['fulltext:request', '显式获取全文']
  ];
  async function action(fn: () => Promise<void>) {
    busy = true;
    error = '';
    message = '';
    try {
      await fn();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
  function edit(feed?: Feed) {
    editing = feed
      ? { ...feed }
      : {
          url: '',
          title: '',
          category_id: '',
          paused: false,
          archive: false,
          interval_hours: 5,
          read_days: 7,
          unread_days: 30
        };
    preview = null;
  }
  async function saveFeed() {
    if (!editing) return;
    await api.saveFeed(editing);
    editing = null;
    await onupdate();
    message = '订阅设置已保存';
  }
  async function refresh(id: string) {
    await api.refresh(id);
    message = '已提交刷新任务';
    await onupdate();
  }
  async function importFile(event: Event) {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (file)
      await action(async () => {
        const r = await api.importOPML(await file.text());
        await onupdate();
        message = `OPML 导入完成：${r.imported} 个订阅`;
      });
  }
  async function exportFile() {
    const xml = await api.exportOPML();
    const url = URL.createObjectURL(new Blob([xml], { type: 'text/xml' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'shiliu-subscriptions.opml';
    a.click();
    URL.revokeObjectURL(url);
  }
  async function switchTab(next: string) {
    tab = next;
    if (next !== 'tokens') newToken = '';
    await action(async () => {
      if (next === 'tokens') tokens = await api.tokens();
      if (next === 'health') health = await api.health();
    });
  }
</script>

<div class="settings">
  <header class="settings-heading">
    <div>
      <p class="eyebrow">拾流 RSS</p>
      <h1>设置</h1>
    </div>
    <button class="icon-button" onclick={onclose} aria-label="关闭设置"><X size={21} /></button>
  </header>
  <nav class="settings-tabs" aria-label="设置分类">
    {#each [['feeds', '订阅与保存'], ['tokens', 'Agent 授权'], ['health', '运行状态']] as [id, label]}<button
        class:chosen={tab === id}
        onclick={() => switchTab(id)}>{label}</button
      >{/each}
  </nav>
  {#if error}<p class="error" role="alert">{error}</p>{/if}{#if message}<p
      class="notice"
      role="status"
    >
      <Check size={16} />{message}
    </p>{/if}
  {#if tab === 'feeds'}
    <div class="section-title">
      <div>
        <h2>订阅源 <small>{feeds.length}</small></h2>
        <p>后台独立更新，暂时失败后会自动重试。</p>
      </div>
      <button class="button" onclick={() => edit()}><Plus size={16} />添加订阅</button>
    </div>
    <div class="feed-settings-list">
      {#each feeds as feed}<div class="feed-setting">
          <div class="source-icon"><Rss size={18} /></div>
          <div class="feed-setting-info">
            <button class="feed-title" onclick={() => edit(feed)}>{feed.title || feed.url}</button
            ><span class="source-url">{feed.url}</span>
            <p class:error-text={feed.error}>
              {feed.paused
                ? '已暂停'
                : feed.error || `下次检查 ${new Date(feed.next_check_at).toLocaleString('zh-CN')}`} ·
              {feed.archive ? '长期存档' : '普通保存'}
            </p>
            <small
              >最近成功：{feed.last_success_at
                ? new Date(feed.last_success_at).toLocaleString('zh-CN')
                : '尚未成功'} · 最近尝试：{feed.last_attempt_at
                ? new Date(feed.last_attempt_at).toLocaleString('zh-CN')
                : '尚未尝试'}</small
            >
          </div>
          <button
            class="icon-button"
            disabled={busy || feed.paused}
            onclick={() => action(() => refresh(feed.id))}
            aria-label={`刷新 ${feed.title}`}><RefreshCw size={17} /></button
          ><button class="button secondary compact" onclick={() => edit(feed)}>编辑</button>
        </div>{/each}
    </div>
    <div class="settings-section">
      <h2>分类</h2>
      <div class="inline-form">
        <input aria-label="新分类名称" bind:value={categoryTitle} placeholder="新分类名称" /><button
          class="button secondary"
          disabled={!categoryTitle.trim() || busy}
          onclick={() =>
            action(async () => {
              await api.saveCategory(categoryTitle.trim());
              categoryTitle = '';
              await onupdate();
            })}>添加分类</button
        >
      </div>
      <p>{categories.map((c) => c.title).join(' · ') || '还没有分类'}</p>
    </div>
    <div class="settings-section">
      <h2>订阅交换</h2>
      <p>OPML 只包含订阅信息。完整文章迁入请使用本机 Miniflux 导入工具。</p>
      <div class="button-row">
        <button class="button secondary" onclick={() => action(exportFile)}
          ><Download size={16} />导出 OPML</button
        ><label class="button secondary"
          ><Upload size={16} />导入 OPML<input
            type="file"
            accept=".opml,.xml,text/xml"
            onchange={importFile}
            class="sr-only"
          /></label
        >
      </div>
    </div>
  {:else if tab === 'tokens'}
    <div class="section-title">
      <div>
        <h2>Agent 访问令牌</h2>
        <p>按用途授权；撤销后即不可再使用。令牌明文仅显示一次。</p>
      </div>
      <KeyRound size={25} />
    </div>
    <form
      onsubmit={(e) => {
        e.preventDefault();
        action(async () => {
          const r = await api.createToken(tokenName, scopes);
          newToken = r.token;
          tokens = await api.tokens();
          tokenName = '';
        });
      }}
      class="token-form"
    >
      <label for="token-name">用途名称</label><input
        id="token-name"
        required
        bind:value={tokenName}
        placeholder="例如：每周阅读整理"
      />
      <div class="permissions">
        {#each permissions as [scope, label]}<label
            ><input type="checkbox" value={scope} bind:group={scopes} />{label}</label
          >{/each}
      </div>
      <button class="button" disabled={busy || !scopes.length}><Plus size={16} />创建令牌</button>
    </form>
    {#if newToken}<div class="secret notice">
        <strong>请现在保存这个令牌</strong><code>{newToken}</code><button
          class="text-button"
          onclick={() => {
            newToken = '';
          }}>我已保存，隐藏令牌</button
        >
      </div>{/if}
    {#each tokens as token}<div class="token-row">
        <div>
          <strong>{token.name}</strong>
          <p>{token.scopes.join(' · ')}</p>
          <small
            >{token.revoked_at
              ? '已撤销'
              : `创建于 ${new Date(token.created_at).toLocaleDateString('zh-CN')}`}</small
          >
        </div>
        {#if !token.revoked_at}<button
            class="button secondary compact"
            disabled={busy}
            onclick={() =>
              action(async () => {
                await api.revoke(token.id);
                tokens = await api.tokens();
              })}><Trash2 size={15} />撤销</button
          >{/if}
      </div>{/each}
  {:else}<div class="section-title">
      <div>
        <h2>运行状态</h2>
        <p>服务端报告的健康与后台任务状态。</p>
      </div>
      <ShieldCheck size={25} />
    </div>
    {#if health}<dl class="health-list">
        {#each Object.entries(health) as [key, value]}<div>
            <dt>{key}</dt>
            <dd>{typeof value === 'object' ? JSON.stringify(value) : String(value)}</dd>
          </div>{/each}
      </dl>{:else}<p class="muted">正在读取状态…</p>{/if}{/if}
  {#if editing}<div class="modal-shade">
      <div
        use:modal={() => {
          editing = null;
        }}
        class="modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="feed-dialog-title"
        tabindex="-1"
      >
        <div class="section-title">
          <h2 id="feed-dialog-title">{editing.id ? '编辑订阅' : '添加订阅'}</h2>
          <button
            class="icon-button"
            onclick={() => {
              editing = null;
            }}
            aria-label="关闭编辑"><X size={19} /></button
          >
        </div>
        <form
          onsubmit={(e) => {
            e.preventDefault();
            action(saveFeed);
          }}
        >
          <label for="feed-url">订阅地址</label><input
            id="feed-url"
            type="url"
            required
            bind:value={editing.url}
            placeholder="https://example.com/feed.xml"
          /><label for="feed-allow-host">允许访问的内网主机（可选）</label><input
            id="feed-allow-host"
            bind:value={editing.allow_host}
            placeholder="例如：192.0.2.20（只授权此主机）"
          /><label for="feed-title">名称</label><input
            id="feed-title"
            bind:value={editing.title}
            placeholder="留空时使用订阅源名称"
          /><label for="feed-category">分类</label><select
            id="feed-category"
            bind:value={editing.category_id}
            ><option value="">未分类</option>{#each categories as c}<option value={c.id}
                >{c.title}</option
              >{/each}</select
          >
          <div class="policy-grid">
            <label
              >检查间隔（小时）<input
                type="number"
                min="1"
                max="168"
                bind:value={editing.interval_hours}
              /></label
            ><label
              >已读可见（天）<input
                type="number"
                min="1"
                max="3650"
                bind:value={editing.read_days}
              /></label
            ><label
              >未读可见（天）<input
                type="number"
                min="1"
                max="3650"
                bind:value={editing.unread_days}
              /></label
            >
          </div>
          <label class="checkbox-label"
            ><input type="checkbox" bind:checked={editing.archive} />长期存档已获取的正文</label
          ><label class="checkbox-label"
            ><input type="checkbox" bind:checked={editing.paused} />暂停自动检查</label
          >
          <p class="form-help">
            普通源正文保留 90
            天。收藏与长期存档豁免正文清理；长期存档仍按可见期限归档，不会自动获取全文。
          </p>
          {#if policyChanged}<p class="notice">
              保存后会立即按新可见期限显示或归档文章。{original?.archive && !editing.archive
                ? '切回普通保存后，超过90天且没有收藏/迁入保护的正文可能立即不可读；已清理正文不会自动恢复。'
                : ''}笔记、标签和 Agent 摘要继续保留。可先预览正文清理范围。
            </p>{/if}
          <div class="button-row">
            {#if editing.id}<button
                type="button"
                class="button secondary"
                onclick={() =>
                  action(async () => {
                    preview = await api.preview(editing!);
                  })}>预览保存影响</button
              >{/if}<button class="button" disabled={busy}>保存订阅</button>
          </div>
          {#if preview}<p class="notice">
              此策略下：{preview.articles} 篇文章、{preview.contents} 份正文、约 {Math.ceil(
                preview.bytes / 1024
              )} KiB 内容受影响。
            </p>{/if}{#if error}<p class="error" role="alert">{error}</p>{/if}
        </form>
      </div>
    </div>{/if}
</div>
