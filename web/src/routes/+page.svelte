<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { goto } from '$app/navigation';
  import { externalURL } from '../lib/links';
  import {
    Inbox,
    Archive,
    Star,
    Search,
    Settings2,
    Rss,
    ChevronRight,
    Menu,
    X,
    ArrowDown,
    CheckCheck,
    LogOut,
    LoaderCircle,
    Keyboard,
    RefreshCw
  } from '@lucide/svelte';
  import Reader from '../lib/Reader.svelte';
  import { modal } from '../lib/modal';
  import { api, APIError } from '../lib/api';
  import type { Article, ArticlePatch, Feed, Category } from '../lib/models';
  let ready = $state(false),
    authenticated = $state(false),
    password = $state(''),
    busy = $state(false),
    loading = $state(false),
    error = $state('');
  let feeds = $state<Feed[]>([]),
    categories = $state<Category[]>([]),
    articles = $state<Article[]>([]),
    selected = $state<Article | null>(null),
    cursor = $state('');
  let scope = $state('recent'),
    favorite = $state(false),
    read = $state(''),
    feedID = $state(''),
    categoryID = $state(''),
    query = $state(''),
    searchDraft = $state(''),
    from = $state(''),
    until = $state('');
  let navOpen = $state(false),
    settingsOpen = $state(false),
    SettingsComponent = $state<typeof import('../lib/Settings.svelte').default | null>(null),
    shortcutOpen = $state(false),
    activeIndex = $state(0),
    unfavoriteTarget = $state<Article | null>(null);
  let listElement = $state<HTMLDivElement>(),
    searchElement = $state<HTMLInputElement>();
  let scrollPosition = 0,
    loadGeneration = 0,
    openGeneration = 0,
    sessionEpoch = 0;
  let sessionChannel: BroadcastChannel | undefined;
  function clearSession() {
    sessionEpoch++;
    openGeneration++;
    loadGeneration++;
    authenticated = false;
    selected = null;
    articles = [];
    feeds = [];
    categories = [];
    settingsOpen = false;
    SettingsComponent = null;
    unfavoriteTarget = null;
    password = '';
    ready = true;
  }
  const title = $derived(
    query
      ? '搜索结果'
      : feedID
        ? feeds.find((f) => f.id === feedID)?.title || '订阅文章'
        : categoryID
          ? categories.find((c) => c.id === categoryID)?.title || '分类文章'
          : favorite
            ? '收藏'
            : scope === 'archive'
              ? '归档'
              : '近期文章'
  );
  const unread = $derived(articles.filter((a) => !a.read_at).length);
  const queryParams = () => ({
    scope,
    ...(favorite ? { favorite: 'true' } : {}),
    ...(feedID ? { feed_id: feedID } : {}),
    ...(categoryID ? { category_id: categoryID } : {}),
    ...(query ? { q: query } : {}),
    ...(read ? { read } : {}),
    ...(from ? { from: new Date(`${from}T00:00:00`).toISOString() } : {}),
    ...(until ? { until: nextDay(until) } : {}),
    limit: '50'
  });
  function nextDay(day: string) {
    const date = new Date(`${day}T00:00:00`);
    date.setDate(date.getDate() + 1);
    return date.toISOString();
  }
  function validDay(value: string | null) {
    return value && /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(value))
      ? value
      : '';
  }
  function syncURL(articleId?: string, replace = false) {
    const p = new URLSearchParams(queryParams());
    p.delete('limit');
    if (articleId) p.set('article', articleId);
    if (settingsOpen) p.set('settings', 'true');
    void goto(`/?${p}`, { shallow: true, replace, reset: false });
  }
  function readURL() {
    const p = new URLSearchParams(location.search);
    scope = ['recent', 'archive', 'all'].includes(p.get('scope') || '')
      ? p.get('scope')!
      : 'recent';
    favorite = p.get('favorite') === 'true';
    feedID = p.get('feed_id') || '';
    categoryID = p.get('category_id') || '';
    query = p.get('q') || '';
    searchDraft = query;
    read = ['true', 'false'].includes(p.get('read') || '') ? p.get('read')! : '';
    from = validDay(p.get('from')?.slice(0, 10) || null);
    until = validDay(p.get('until')?.slice(0, 10) || null);
    settingsOpen = p.get('settings') === 'true';
    return p.get('article');
  }
  async function sources() {
    const epoch = sessionEpoch;
    const result = await Promise.all([api.feeds(), api.categories()]);
    if (epoch !== sessionEpoch) return;
    [feeds, categories] = result;
    feeds ||= [];
    categories ||= [];
  }
  async function list(append = false) {
    const generation = ++loadGeneration;
    loading = true;
    error = '';
    try {
      const result = await api.articles({
        ...queryParams(),
        ...(append && cursor ? { cursor } : {})
      });
      if (generation !== loadGeneration) return;
      articles = append ? [...articles, ...(result.items || [])] : result.items || [];
      cursor = result.next_cursor || '';
      if (!append) activeIndex = 0;
    } catch (e) {
      handleError(e);
    } finally {
      if (generation === loadGeneration) loading = false;
    }
  }
  function handleError(e: unknown) {
    error = (e as Error).message;
    if (e instanceof APIError && e.status === 401) {
      clearSession();
    }
  }
  async function login(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      await api.login(password);
      password = '';
      authenticated = true;
      await sources();
      await list();
      const id = new URLSearchParams(location.search).get('article');
      if (id) await open(id, false);
    } catch (e) {
      handleError(e);
    } finally {
      busy = false;
    }
  }
  async function navigate(
    nextScope: string,
    options: { favorite?: boolean; feed?: string; category?: string } = {}
  ) {
    scope = nextScope;
    favorite = options.favorite || false;
    feedID = options.feed || '';
    categoryID = options.category || '';
    query = '';
    searchDraft = '';
    read = '';
    selected = null;
    settingsOpen = false;
    navOpen = false;
    from = '';
    until = '';
    syncURL();
    await list();
  }
  async function search(e?: SubmitEvent) {
    e?.preventDefault();
    query = searchDraft.trim();
    selected = null;
    syncURL();
    await list();
  }
  async function open(id: string, push = true) {
    const generation = ++openGeneration;
    error = '';
    if (!selected) scrollPosition = listElement?.scrollTop || 0;
    try {
      const loaded = await api.article(id);
      if (generation !== openGeneration) return;
      selected = loaded;
      activeIndex = Math.max(
        0,
        articles.findIndex((a) => a.id === id)
      );
      if (push) syncURL(id);
      if (!selected.read_at) await patch({ read: true });
      await tick();
      document.querySelector<HTMLElement>('.reader')?.scrollTo({ top: 0 });
      document.querySelector<HTMLElement>('.reading-content h1')?.focus({ preventScroll: true });
    } catch (e) {
      handleError(e);
    }
  }
  async function closeReader() {
    openGeneration++;
    selected = null;
    syncURL();
    await tick();
    if (listElement) listElement.scrollTop = scrollPosition;
    document
      .querySelector<HTMLButtonElement>(`[data-article-id="${articles[activeIndex]?.id}"]`)
      ?.focus({ preventScroll: true });
  }
  async function patch(value: ArticlePatch, confirmed = false) {
    if (!selected) return;
    if (value.favorite === false && !confirmed) {
      unfavoriteTarget = selected;
      return;
    }
    const target = selected,
      epoch = sessionEpoch;
    try {
      const updated = Object.keys(value).length
        ? await api.patch(target, value)
        : await api.article(target.id);
      if (epoch !== sessionEpoch) return;
      if (selected?.id === target.id) selected = updated;
      articles = articles.map((a) =>
        a.id === updated.id ? { ...a, ...updated, content: undefined, artifacts: undefined } : a
      );
    } catch (e) {
      handleError(e);
      if (e instanceof APIError && e.status === 412 && selected?.id === target.id)
        selected = await api.article(target.id);
      throw e;
    }
  }
  async function move(n: number) {
    const i = articles.findIndex((a) => a.id === selected?.id);
    const next = articles[i + n];
    if (next) await open(next.id);
  }
  async function bulkRead() {
    busy = true;
    error = '';
    const epoch = sessionEpoch;
    try {
      for (const a of articles.filter((a) => !a.read_at)) {
        const updated = await api.patch(a, { read: true });
        if (epoch !== sessionEpoch) return;
        articles = articles.map((x) => (x.id === a.id ? updated : x));
      }
    } catch (e) {
      handleError(e);
    } finally {
      busy = false;
    }
  }
  async function showSettings() {
    settingsOpen = true;
    navOpen = false;
    selected = null;
    syncURL();
    SettingsComponent = (await import('../lib/Settings.svelte')).default;
    try {
      await sources();
    } catch (e) {
      handleError(e);
    }
  }
  async function logout() {
    try {
      await api.logout();
      clearSession();
      sessionChannel?.postMessage('logout');
      void goto('/', { shallow: true, replace: true, reset: false });
    } catch (e) {
      handleError(e);
    }
  }
  function keyboard(e: KeyboardEvent) {
    if (!authenticated || e.ctrlKey || e.metaKey || e.altKey) return;
    const target = e.target as HTMLElement;
    if (target.matches('input,textarea,select,[contenteditable]')) {
      if (e.key === 'Escape') target.blur();
      return;
    }
    if (e.key === 'Escape') {
      if (unfavoriteTarget) unfavoriteTarget = null;
      else if (shortcutOpen) shortcutOpen = false;
      else if (selected) closeReader();
      else if (settingsOpen) {
        settingsOpen = false;
        syncURL();
      } else navOpen = false;
      return;
    }
    if (settingsOpen || shortcutOpen || unfavoriteTarget) return;
    if (e.key === '/') {
      e.preventDefault();
      searchElement?.focus();
    }
    if (e.key === '?') shortcutOpen = !shortcutOpen;
    if (['j', 'k'].includes(e.key)) {
      e.preventDefault();
      if (selected) move(e.key === 'j' ? 1 : -1);
      else {
        activeIndex = Math.min(
          articles.length - 1,
          Math.max(0, activeIndex + (e.key === 'j' ? 1 : -1))
        );
        document
          .querySelector<HTMLButtonElement>(`[data-article-id="${articles[activeIndex]?.id}"]`)
          ?.focus();
      }
    }
    if (
      e.key === 'Enter' &&
      !selected &&
      articles[activeIndex] &&
      (target === document.body || target.matches('[data-article-id]'))
    ) {
      e.preventDefault();
      void open(articles[activeIndex].id);
    }
    if (selected && e.key === 'm') void patch({ read: !selected.read_at }).catch(() => {});
    if (selected && e.key === 's') void patch({ favorite: !selected.favorite }).catch(() => {});
    if (selected && e.key === 'o' && externalURL(selected.url))
      window.open(externalURL(selected.url), '_blank', 'noopener,noreferrer');
  }
  onMount(() => {
    let disposed = false;
    const pop = async () => {
      const id = readURL();
      selected = null;
      await list();
      if (id) await open(id, false);
      if (settingsOpen) SettingsComponent = (await import('../lib/Settings.svelte')).default;
    };
    (async () => {
      const id = readURL();
      try {
        const session = await api.session();
        if (disposed) return;
        authenticated = session.authenticated;
        if (authenticated) {
          await sources();
          await list();
          if (id) await open(id, false);
          if (settingsOpen) SettingsComponent = (await import('../lib/Settings.svelte')).default;
        }
      } catch (e) {
        handleError(e);
      } finally {
        ready = true;
      }
    })();
    const expired = () => clearSession();
    window.addEventListener('shiliu:session-expired', expired);
    if ('BroadcastChannel' in window) {
      sessionChannel = new BroadcastChannel('shiliu-session');
      sessionChannel.onmessage = (e) => {
        if (e.data === 'logout') clearSession();
      };
    }
    window.addEventListener('popstate', pop);
    return () => {
      disposed = true;
      window.removeEventListener('popstate', pop);
      window.removeEventListener('shiliu:session-expired', expired);
      sessionChannel?.close();
    };
  });
  function clearExpired() {
    if (selected) {
      selected = {
        ...selected,
        content: selected.content?.map((c) =>
          c.expires_at && Date.parse(c.expires_at) <= Date.now()
            ? { ...c, html: '', text: '', state: 'purged' }
            : c
        )
      };
      if (!selected.content?.some((c) => c.state === 'available')) selected.body_state = 'purged';
    }
  }
  $effect(() => {
    const expiries =
      selected?.content
        ?.filter((c) => c.state === 'available' && c.expires_at)
        .map((c) => Date.parse(c.expires_at!)) || [];
    if (expiries.length) {
      const delay = Math.min(...expiries) - Date.now();
      const timer = setTimeout(clearExpired, Math.max(0, Math.min(delay, 2147483647)));
      return () => clearTimeout(timer);
    }
  });
  let revalidating = false;
  function hidePrivatePage() {
    if (authenticated) {
      ready = false;
      if (selected) selected = { ...selected, content: undefined };
      SettingsComponent = null;
    }
  }
  async function revalidate() {
    if (document.visibilityState === 'hidden' || revalidating) return;
    revalidating = true;
    const epoch = sessionEpoch;
    try {
      const session = await api.session();
      if (epoch !== sessionEpoch) return;
      if (!session.authenticated) {
        clearSession();
        return;
      }
      authenticated = true;
      clearExpired();
      if (selected) {
        const id = selected.id;
        selected = { ...selected, content: undefined };
        const updated = await api.article(id);
        if (epoch === sessionEpoch && selected?.id === id) selected = updated;
      }
      if (!ready) {
        await sources();
        await list();
        if (settingsOpen) SettingsComponent = (await import('../lib/Settings.svelte')).default;
      }
    } catch (e) {
      handleError(e);
    } finally {
      ready = true;
      revalidating = false;
    }
  }
  function date(value: string) {
    return new Date(value).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' });
  }
</script>

<svelte:head
  ><title>{selected ? selected.title : settingsOpen ? '设置' : title} · 拾流 RSS</title><meta
    name="description"
    content="拾流 RSS，留住值得阅读的内容。"
  /></svelte:head
>
<svelte:window
  onkeydown={keyboard}
  onfocus={revalidate}
  onpageshow={revalidate}
  onpagehide={hidePrivatePage}
/><svelte:document onvisibilitychange={revalidate} />
{#if !ready}<main class="startup">
    <img src="/icon.svg" alt="" /><LoaderCircle class="spin" size={20} />
    <p>正在打开拾流…</p>
  </main>
{:else if !authenticated}<main class="login-page">
    <form class="login-card" onsubmit={login}>
      <img src="/icon.svg" alt="拾流石榴图标" />
      <p class="eyebrow">SHILIU · RSS</p>
      <h1>拾起信息，留住思考。</h1>
      <p class="login-intro">属于你的安静阅读空间。</p>
      <label for="password">登录密码</label><input
        id="password"
        type="password"
        autocomplete="current-password"
        required
        bind:value={password}
        placeholder="输入你的密码"
      />{#if error}<p class="error" role="alert">{error}</p>{/if}<button
        class="button"
        disabled={busy}
        >{#if busy}<LoaderCircle size={16} class="spin" />{/if}进入拾流<ChevronRight
          size={17}
        /></button
      >
    </form>
    <span class="login-footer">拾流 RSS · 为阅读留一处空白</span>
  </main>
{:else}<div class="app-shell" class:has-reader={!!selected}>
    {#if navOpen}<button
        class="nav-shade"
        aria-label="关闭导航"
        onclick={() => {
          navOpen = false;
        }}
      ></button>{/if}
    <aside class:open={navOpen} class="sidebar">
      <a
        href="/"
        class="brand"
        onclick={(e) => {
          e.preventDefault();
          navigate('recent');
        }}><img src="/icon.svg" alt="" /><span>拾流 <small>RSS</small></span></a
      >
      <div class="workspace-caption">我的阅读空间</div>
      <nav aria-label="阅读导航" class="main-nav">
        <button
          class:chosen={scope === 'recent' && !favorite && !feedID && !categoryID}
          onclick={() => navigate('recent')}
          ><Inbox size={18} />近期文章<span class="nav-hint">01</span></button
        ><button class:chosen={favorite} onclick={() => navigate('all', { favorite: true })}
          ><Star size={18} />我的收藏</button
        ><button class:chosen={scope === 'archive' && !favorite} onclick={() => navigate('archive')}
          ><Archive size={18} />归档</button
        >
      </nav>
      <div class="nav-section-label">分类</div>
      <nav aria-label="分类">
        {#each categories as category}<button
            class:chosen={categoryID === category.id}
            onclick={() => navigate('recent', { category: category.id })}
            ><span class="category-dot"></span>{category.title}</button
          >{/each}{#if !categories.length}<p class="nav-empty">在设置中整理订阅分类</p>{/if}
      </nav>
      <div class="nav-section-label">
        订阅源 <button class="icon-button" onclick={showSettings} aria-label="管理订阅"
          ><Settings2 size={13} /></button
        >
      </div>
      <nav aria-label="订阅源" class="source-nav">
        {#each feeds as feed}<button
            class:chosen={feedID === feed.id}
            onclick={() => navigate('recent', { feed: feed.id })}
            ><Rss size={14} /><span>{feed.title || new URL(feed.url).hostname}</span
            >{#if feed.error}<span class="health-dot" title="最近抓取失败"></span>{/if}</button
          >{/each}{#if !feeds.length}<button onclick={showSettings}
            ><Rss size={15} />添加第一个订阅</button
          >{/if}
      </nav>
      <div class="sidebar-bottom">
        <button onclick={showSettings}><Settings2 size={17} />设置与授权</button><button
          onclick={() => {
            shortcutOpen = true;
          }}><Keyboard size={17} />快捷键<span class="keycap">?</span></button
        ><button onclick={logout}><LogOut size={16} />退出登录</button>
        <p>少一些噪音，多一点阅读。</p>
      </div>
    </aside>
    <main class="main-pane">
      {#if settingsOpen && SettingsComponent}<SettingsComponent
          {feeds}
          {categories}
          onupdate={sources}
          onclose={() => {
            settingsOpen = false;
            syncURL();
          }}
        />
      {:else if selected}{#if error}<p class="error global-error" role="alert">
            {error}
          </p>{/if}<Reader article={selected} onclose={closeReader} onpatch={patch} onmove={move} />
      {:else}<header class="page-header">
          <div class="page-heading">
            <button
              class="icon-button mobile-menu"
              onclick={() => {
                navOpen = true;
              }}
              aria-label="打开导航"><Menu size={21} /></button
            >
            <div>
              <p class="eyebrow">YOUR DAILY READING</p>
              <h1>{title}</h1>
            </div>
            <span class="article-count">{articles.length}{cursor ? '+' : ''} 篇</span>
          </div>
          <form class="search-box" role="search" onsubmit={search}>
            <Search size={17} /><input
              aria-label="搜索文章"
              bind:this={searchElement}
              bind:value={searchDraft}
              placeholder="搜索文章、笔记与摘要"
              maxlength="128"
            /><kbd>/</kbd>
          </form>
        </header>
        <div class="list-controls">
          <div class="filter-tabs">
            <button
              class:chosen={!read}
              onclick={() => {
                read = '';
                syncURL();
                list();
              }}>全部</button
            ><button
              class:chosen={read === 'false'}
              onclick={() => {
                read = 'false';
                syncURL();
                list();
              }}>未读 <span>{unread}</span></button
            ><button
              class:chosen={read === 'true'}
              onclick={() => {
                read = 'true';
                syncURL();
                list();
              }}>已读</button
            >
          </div>
          <div class="list-actions">
            <button class="icon-button" onclick={() => list()} aria-label="更新列表"
              ><RefreshCw size={16} class={loading ? 'spin' : ''} /></button
            ><button class="text-button" disabled={busy || !unread} onclick={bulkRead}
              ><CheckCheck size={16} /><span>本页标为已读</span></button
            >
          </div>
        </div>
        {#if query || scope === 'archive'}<div class="search-filters">
            <span>搜索范围</span><select
              aria-label="搜索范围"
              bind:value={scope}
              onchange={() => {
                syncURL();
                list();
              }}
              ><option value="recent">近期文章</option><option value="archive">过期归档</option
              ><option value="all">所有文章（含归档）</option></select
            ><label
              >从 <input
                aria-label="起始日期"
                type="date"
                bind:value={from}
                onchange={() => {
                  syncURL();
                  list();
                }}
              /></label
            ><label
              >至 <input
                aria-label="截止日期"
                type="date"
                bind:value={until}
                onchange={() => {
                  syncURL();
                  list();
                }}
              /></label
            >
          </div>{/if}
        {#if error}<p class="error global-error" role="alert">{error}</p>{/if}
        <div class="article-list" bind:this={listElement} aria-busy={loading}>
          {#each articles as article, i}<button
              class="article-row"
              data-article-id={article.id}
              class:is-read={!!article.read_at}
              class:keyboard-active={i === activeIndex}
              onclick={() => open(article.id)}
              ><span class="unread-dot" class:invisible={!!article.read_at}></span>
              <div class="article-summary">
                <div class="article-source">
                  {article.feed_title}<span>·</span><time
                    datetime={article.published_at || article.first_seen_at}
                    >{date(article.published_at || article.first_seen_at)}</time
                  >{#if article.body_state === 'purged'}<span class="body-badge">正文已清理</span
                    >{/if}
                </div>
                <h2>{article.title}</h2>
                {#if article.tags?.length}<div class="article-tags">
                    {#each article.tags as tag}<span>#{tag}</span>{/each}
                  </div>{/if}
              </div>
              {#if article.favorite}<Star
                  size={15}
                  class="list-star"
                  fill="currentColor"
                />{/if}<ChevronRight size={17} class="row-chevron" /></button
            >{/each}
          {#if !articles.length && !loading}<div class="empty-state">
              <div class="empty-icon">
                {#if favorite}<Star size={29} />{:else if query}<Search size={29} />{:else}<Inbox
                    size={29}
                  />{/if}
              </div>
              <h2>
                {query
                  ? '没有找到匹配的文章'
                  : favorite
                    ? '收藏值得留下的阅读'
                    : feeds.length
                      ? '这里暂时没有文章'
                      : '从一个订阅开始'}
              </h2>
              <p>
                {query
                  ? '试试其他关键词，或将范围切换为所有文章。'
                  : feeds.length
                    ? '后台会持续检查订阅。你也可以在设置中查看源健康与下次尝试。'
                    : '添加喜欢的 RSS / Atom / JSON Feed，新的内容会安静地汇聚在这里。'}
              </p>
              {#if !feeds.length}<button class="button" onclick={showSettings}
                  ><Rss size={16} />添加订阅</button
                >{/if}
            </div>{/if}
          {#if cursor}<button
              class="load-more button secondary"
              disabled={loading}
              onclick={() => list(true)}
              >{#if loading}<LoaderCircle class="spin" size={16} />{:else}<ArrowDown
                  size={16}
                />{/if}加载更多</button
            >{/if}{#if loading && !articles.length}<div class="list-loading">
              <LoaderCircle class="spin" size={20} /><span>正在加载文章…</span>
            </div>{/if}
          {#if articles.length}<div class="list-end">
              <span></span>留一点时间，好好阅读<span></span>
            </div>{/if}
        </div>
        <footer class="list-footer">
          <span>后台自动更新 · 全文按需获取</span><button
            class="text-button"
            onclick={() => {
              shortcutOpen = true;
            }}>j / k 导航 <Keyboard size={14} /></button
          >
        </footer>{/if}
    </main>
  </div>{/if}
{#if shortcutOpen}<div class="modal-shade">
    <div
      use:modal={() => {
        shortcutOpen = false;
      }}
      class="modal shortcuts"
      role="dialog"
      aria-modal="true"
      aria-labelledby="shortcut-title"
      tabindex="-1"
    >
      <div class="section-title">
        <h2 id="shortcut-title">键盘阅读</h2>
        <button
          class="icon-button"
          onclick={() => {
            shortcutOpen = false;
          }}
          aria-label="关闭快捷键"><X size={19} /></button
        >
      </div>
      {#each [['j / k', '下一篇 / 上一篇'], ['Enter', '打开选中文章'], ['m', '切换已读'], ['s', '切换收藏'], ['o', '打开原文'], ['/', '搜索'], ['Esc', '返回列表 / 关闭面板'], ['?', '显示快捷键']] as [key, label]}<div
        >
          <kbd>{key}</kbd><span>{label}</span>
        </div>{/each}
    </div>
  </div>{/if}

{#if unfavoriteTarget}
  <div class="modal-shade">
    <div
      use:modal={() => {
        unfavoriteTarget = null;
      }}
      class="modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="unfavorite-title"
      tabindex="-1"
    >
      <div class="section-title">
        <h2 id="unfavorite-title">取消收藏前</h2>
        <button
          class="icon-button"
          aria-label="保留收藏"
          onclick={() => {
            unfavoriteTarget = null;
          }}><X size={19} /></button
        >
      </div>
      <p class="form-help">
        {feeds.find((f) => f.id === unfavoriteTarget!.feed_id)?.archive
          ? '这个源使用长期存档，当前正文继续受源策略保护。取消收藏后，文章仍会按可见期限进入归档。'
          : '取消收藏后恢复来源的可见期限与普通正文保存策略。已到期文章可能立即从近期隐藏，已到期正文可能立即不可读。'}笔记、标签与
        Agent 摘要会继续保留。
      </p>
      <div class="button-row">
        <button
          class="button secondary"
          onclick={() => {
            unfavoriteTarget = null;
          }}>保留收藏</button
        ><button
          class="button"
          onclick={() => {
            unfavoriteTarget = null;
            void patch({ favorite: false }, true).catch(() => {});
          }}>确认取消收藏</button
        >
      </div>
    </div>
  </div>
{/if}
