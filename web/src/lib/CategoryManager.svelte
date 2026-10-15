<script lang="ts">
  import { Folder, Plus, Pencil, Trash2, X, Check, Minus, Pause, Play } from '@lucide/svelte';
  import { api, APIError } from './api';
  import type { Category, Feed } from './models';
  import { modal } from './modal';
  import FeedIcon from './FeedIcon.svelte';

  let {
    feeds,
    categories,
    onupdate,
    onedit
  }: {
    feeds: Feed[];
    categories: Category[];
    onupdate: () => Promise<void>;
    onedit: (feed: Feed | undefined, categoryId: string) => void;
  } = $props();
  let selected = $state('');
  let categorySearch = $state('');
  let feedSearch = $state('');
  let assignId = $state('');
  let busy = $state(false);
  let error = $state('');
  let message = $state('');
  let dialog = $state<'create' | 'rename' | 'delete' | null>(null);
  let target = $state<Category | null>(null);
  let title = $state('');
  const titleTooLong = $derived([...title.trim()].length > 100);
  const current = $derived(categories.find((c) => c.id === selected));
  const visibleCategories = $derived(
    categories.filter((c) =>
      c.title.toLocaleLowerCase().includes(categorySearch.trim().toLocaleLowerCase())
    )
  );
  const members = $derived(feeds.filter((f) => (f.category_id || '') === selected));
  const visibleFeeds = $derived(
    members.filter((f) =>
      `${f.title} ${f.url}`.toLocaleLowerCase().includes(feedSearch.trim().toLocaleLowerCase())
    )
  );
  const availableFeeds = $derived(feeds.filter((f) => (f.category_id || '') !== selected));
  const count = (id: string) => feeds.filter((f) => (f.category_id || '') === id).length;

  $effect(() => {
    if (selected && !categories.some((c) => c.id === selected)) selected = '';
    if (assignId && !availableFeeds.some((f) => f.id === assignId)) assignId = '';
  });
  function select(id: string) {
    selected = id;
    feedSearch = '';
    assignId = '';
    error = '';
    message = '';
  }
  function open(mode: 'create' | 'rename' | 'delete') {
    dialog = mode;
    target = current ? { ...current } : null;
    title = mode === 'rename' ? current?.title || '' : '';
    error = '';
  }
  async function action(fn: () => Promise<void>) {
    busy = true;
    error = '';
    message = '';
    try {
      await fn();
    } catch (e) {
      if (e instanceof APIError && [409, 412].includes(e.status)) {
        if (e.status === 412) dialog = null;
        const conflict =
          e.status === 412
            ? '数据已被其他操作更新，请检查当前内容后重试。'
            : dialog === 'create' || dialog === 'rename'
              ? '分类名称重复或数据冲突，请修改后重试。'
              : '数据存在冲突，请检查后重试。';
        try {
          await onupdate();
          error = conflict;
        } catch {
          error = `${conflict} 列表刷新失败，请稍后重新打开设置。`;
        }
      } else error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
  async function saveCategory() {
    if (!title.trim() || titleTooLong) return;
    if (dialog === 'create') {
      const created = await api.saveCategory(title.trim());
      await onupdate();
      selected = created.id;
      categorySearch = '';
      message = '分类已创建';
    } else if (target) {
      await api.editCategory(target, title.trim());
      await onupdate();
      message = '分类已重命名';
    }
    dialog = null;
  }
  async function removeCategory() {
    if (!target) return;
    await api.deleteCategory(target);
    await onupdate();
    select('');
    dialog = null;
    message = '分类已删除，订阅已移至未分类，文章保留';
  }
  async function move(feed: Feed, categoryId: string) {
    await api.saveFeed({ ...feed, category_id: categoryId });
    await onupdate();
    assignId = '';
    message = categoryId ? '订阅已加入分类' : '订阅已移至未分类，文章保留';
  }
</script>

<section aria-label="分类管理">
  <div class="section-title">
    <h2>分类 <small>{categories.length}</small></h2>
    <button class="button" onclick={() => open('create')}><Plus size={16} />新建分类</button>
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if message}<p class="notice" role="status"><Check size={16} />{message}</p>{/if}
  <div class="category-layout">
    <nav class="category-nav" aria-label="管理分类列表">
      <input
        aria-label="搜索分类"
        placeholder="搜索分类"
        type="search"
        bind:value={categorySearch}
      />
      <button
        class:active={selected === ''}
        aria-pressed={selected === ''}
        onclick={() => select('')}
      >
        <Folder size={17} /><span>未分类</span><small>{count('')}</small>
      </button>
      {#each visibleCategories as category (category.id)}
        <button
          class:active={selected === category.id}
          aria-pressed={selected === category.id}
          onclick={() => select(category.id)}
        >
          <Folder size={17} /><span>{category.title}</span><small>{count(category.id)}</small>
        </button>
      {/each}
      {#if categorySearch && !visibleCategories.length}<p class="empty">没有匹配的分类</p>{/if}
    </nav>
    <div class="category-detail">
      <header class="detail-heading">
        <div>
          <h3>{current?.title || '未分类'}</h3>
          <p>{members.length} 个订阅源</p>
        </div>
        {#if current}<div class="detail-actions">
            <button
              class="icon-button"
              aria-label="重命名分类"
              disabled={busy}
              onclick={() => open('rename')}><Pencil size={17} /></button
            >
            <button
              class="icon-button danger"
              aria-label="删除分类"
              disabled={busy}
              onclick={() => open('delete')}><Trash2 size={17} /></button
            >
          </div>{/if}
      </header>
      <div class="member-toolbar">
        <input
          type="search"
          aria-label="搜索分类内订阅"
          placeholder="搜索订阅名称或地址"
          bind:value={feedSearch}
        />
        <button class="button secondary" onclick={() => onedit(undefined, selected)}
          ><Plus size={16} />新增订阅</button
        >
      </div>
      {#if current}<form
          class="assign-form"
          onsubmit={(event) => {
            event.preventDefault();
            const feed = availableFeeds.find((f) => f.id === assignId);
            if (feed) action(() => move(feed, selected));
          }}
        >
          <select
            aria-label="选择已有订阅"
            bind:value={assignId}
            disabled={busy || !availableFeeds.length}
          >
            <option value="">选择已有订阅加入此分类</option>
            {#each availableFeeds as feed}<option value={feed.id}
                >{feed.title || feed.url} · {categories.find((c) => c.id === feed.category_id)
                  ?.title || '未分类'}</option
              >{/each}
          </select>
          <button class="button secondary" disabled={busy || !assignId}>加入分类</button>
        </form>{/if}
      <div class="member-list">
        {#each visibleFeeds as feed (feed.id)}<div class="member-row" data-feed-id={feed.id}>
            <FeedIcon {feed} />
            <div class="member-info">
              <button class="feed-title" onclick={() => onedit(feed, selected)}
                >{feed.title || feed.url}</button
              ><span>{feed.url}</span><small
                >{feed.paused ? '已暂停' : feed.error || '自动更新'}</small
              >
            </div>
            <div class="member-actions">
              <button
                class="icon-button"
                aria-label={`${feed.paused ? '恢复' : '暂停'} ${feed.title}`}
                disabled={busy}
                onclick={() =>
                  action(async () => {
                    await api.saveFeed({ ...feed, paused: !feed.paused });
                    await onupdate();
                  })}
                >{#if feed.paused}<Play size={16} />{:else}<Pause size={16} />{/if}</button
              >
              <button
                class="icon-button"
                aria-label={`编辑 ${feed.title}`}
                onclick={() => onedit(feed, selected)}><Pencil size={16} /></button
              >
              {#if current}<button
                  class="icon-button"
                  aria-label={`将 ${feed.title} 移至未分类`}
                  title="移至未分类"
                  disabled={busy}
                  onclick={() => action(() => move(feed, ''))}><Minus size={17} /></button
                >{/if}
            </div>
          </div>{:else}<p class="empty">
            {feedSearch ? '没有匹配的订阅' : '此分类暂无订阅'}
          </p>{/each}
      </div>
    </div>
  </div>
</section>

{#if dialog}<div class="modal-shade">
    <div
      class="modal category-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="category-dialog-title"
      tabindex="-1"
      use:modal={() => {
        if (!busy) dialog = null;
      }}
    >
      <div class="section-title">
        <h2 id="category-dialog-title">
          {dialog === 'create' ? '新建分类' : dialog === 'rename' ? '重命名分类' : '删除分类'}
        </h2>
        <button
          class="icon-button"
          aria-label="关闭分类对话框"
          disabled={busy}
          onclick={() => {
            dialog = null;
          }}><X size={19} /></button
        >
      </div>
      {#if dialog === 'delete'}<p class="delete-description">
          删除“{target?.title}”？订阅移至未分类，文章保留。
        </p>
        <div class="dialog-actions">
          <button
            class="button secondary"
            disabled={busy}
            onclick={() => {
              dialog = null;
            }}>取消</button
          ><button
            class="button delete-button"
            disabled={busy}
            onclick={() => action(removeCategory)}>确认删除分类</button
          >
        </div>
      {:else}<form
          onsubmit={(event) => {
            event.preventDefault();
            action(saveCategory);
          }}
        >
          <label for="category-name">分类名称</label><input
            id="category-name"
            required
            bind:value={title}
            aria-invalid={titleTooLong}
          />
          {#if titleTooLong}<p class="error">分类名称最多 100 个字符</p>{/if}
          <div class="dialog-actions">
            <button
              type="button"
              class="button secondary"
              disabled={busy}
              onclick={() => {
                dialog = null;
              }}>取消</button
            ><button class="button" disabled={busy || !title.trim() || titleTooLong}
              >保存分类</button
            >
          </div>
        </form>{/if}
      {#if error}<p class="error" role="alert">{error}</p>{/if}
    </div>
  </div>{/if}

<style>
  .category-layout {
    display: grid;
    grid-template-columns: minmax(150px, 220px) minmax(0, 1fr);
    gap: 24px;
  }
  .category-nav {
    display: flex;
    flex-direction: column;
    gap: 5px;
    min-width: 0;
  }
  .category-nav input {
    margin-bottom: 8px;
  }
  .category-nav button {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 12px;
    border-radius: 7px;
    text-align: left;
  }
  .category-nav button.active {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .category-nav button span {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .category-nav small {
    color: var(--muted);
  }
  .category-detail {
    min-width: 0;
    border: 1px solid var(--line);
    border-radius: 10px;
    padding: 20px;
  }
  .detail-heading,
  .detail-actions,
  .member-toolbar,
  .assign-form,
  .member-row,
  .member-actions,
  .dialog-actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .detail-heading {
    justify-content: space-between;
    margin-bottom: 20px;
  }
  .detail-heading h3 {
    font-size: 18px;
    margin: 0;
    overflow-wrap: anywhere;
  }
  .detail-heading p {
    color: var(--muted);
    font-size: 12px;
    margin-top: 6px;
  }
  .member-toolbar input,
  .assign-form select {
    flex: 1;
    min-width: 0;
  }
  .member-toolbar button,
  .assign-form button {
    flex-shrink: 0;
  }
  .assign-form {
    margin-top: 12px;
  }
  .member-list {
    margin-top: 16px;
  }
  .member-row {
    padding: 16px 0;
    border-top: 1px solid var(--line);
  }
  .member-info {
    min-width: 0;
    flex: 1;
  }
  .member-info span,
  .member-info small {
    display: block;
    color: var(--muted);
    font-size: 11px;
    overflow-wrap: anywhere;
    margin-top: 5px;
  }
  .member-info :global(.feed-title) {
    text-align: left;
    overflow-wrap: anywhere;
  }
  .member-actions {
    gap: 2px;
    flex-shrink: 0;
  }
  .empty {
    padding: 22px 8px;
    color: var(--muted);
    font-size: 13px;
  }
  .danger {
    color: var(--danger);
  }
  .category-modal {
    max-width: 440px;
  }
  .dialog-actions {
    justify-content: flex-end;
    margin-top: 22px;
  }
  .delete-description {
    line-height: 1.8;
    overflow-wrap: anywhere;
  }
  .delete-button {
    background: var(--danger);
  }
  @media (max-width: 720px) {
    .category-layout {
      grid-template-columns: minmax(0, 1fr);
      gap: 18px;
    }
    .category-nav {
      max-height: 240px;
      overflow: auto;
    }
    .category-detail {
      padding: 15px;
    }
    .member-row {
      flex-wrap: wrap;
    }
    .member-actions {
      margin-left: auto;
    }
    .member-toolbar,
    .assign-form {
      flex-wrap: wrap;
    }
    .member-toolbar input,
    .assign-form select {
      flex-basis: 100%;
    }
  }
</style>
