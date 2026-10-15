<script lang="ts">
  import {
    ArrowLeft,
    ArrowUp,
    ArrowDown,
    Star,
    Check,
    ExternalLink,
    FileText,
    Bookmark,
    LoaderCircle,
    PanelLeftClose,
    PanelLeftOpen
  } from '@lucide/svelte';
  import FeedIcon from './FeedIcon.svelte';
  import type { Article, ArticlePatch, Job, Feed } from './models';
  import { api } from './api';
  import { externalURL } from './links';
  let {
    article,
    onclose,
    onpatch,
    onmove,
    feed,
    ontogglesidebar,
    sidebarCollapsed,
    navOpen,
    isMobile,
    onsource,
    sourceHref
  }: {
    article: Article;
    feed?: Feed;
    ontogglesidebar: () => void;
    sidebarCollapsed: boolean;
    navOpen: boolean;
    isMobile: boolean;
    onsource: (e: MouseEvent, id: string) => void;
    sourceHref: string;
    onclose: () => void;
    onpatch: (p: ArticlePatch) => Promise<void>;
    onmove: (n: number) => void;
  } = $props();
  let note = $state(''),
    tags = $state(''),
    saving = $state(false),
    error = $state(''),
    job = $state<Job | null>(null);
  $effect(() => {
    if (job && ['queued', 'running', 'waiting_retry'].includes(job.state)) {
      const timeout = setTimeout(() => {
        void checkJob();
      }, 2000);
      return () => clearTimeout(timeout);
    }
  });
  let activeId = $state('');
  $effect(() => {
    if (activeId !== article.id) {
      activeId = article.id;
      note = article.note;
      tags = (article.tags || []).join(', ');
      error = '';
      job = null;
    }
  });
  const content = $derived(
    article.content?.find((c) => c.kind === 'fulltext' && c.state === 'available') ||
      article.content?.find((c) => c.state === 'available')
  );
  async function save() {
    saving = true;
    error = '';
    try {
      await onpatch({
        note,
        tags: tags
          .split(/[,，]/)
          .map((t) => t.trim())
          .filter(Boolean)
      });
    } catch (e) {
      error = (e as Error).message;
    } finally {
      saving = false;
    }
  }
  async function fulltext() {
    error = '';
    try {
      job = await api.fulltext(article.id);
    } catch (e) {
      error = (e as Error).message;
    }
  }
  async function checkJob() {
    if (!job) return;
    try {
      job = await api.job(job.id);
      if (job.state === 'succeeded') await onpatch({});
    } catch (e) {
      error = (e as Error).message;
    }
  }
</script>

<article class="reader" aria-label="文章阅读">
  <div class="reader-toolbar">
    <button
      onclick={ontogglesidebar}
      class="icon-button sidebar-toggle"
      aria-label="切换侧栏 (c)"
      title="切换侧栏 (c)"
      aria-controls="reading-sidebar"
      aria-expanded={isMobile ? navOpen : !sidebarCollapsed}
      >{#if isMobile ? !navOpen : sidebarCollapsed}<PanelLeftOpen size={19} />{:else}<PanelLeftClose
          size={19}
        />{/if}</button
    >
    <button onclick={onclose} class="icon-button" aria-label="返回列表 (Esc)" title="返回列表 (Esc)"
      ><ArrowLeft size={19} /></button
    ><span class="toolbar-spacer"></span><button
      onclick={() => onmove(-1)}
      class="icon-button"
      aria-label="上一篇 (↑)"
      title="上一篇 (↑)"><ArrowUp size={18} /></button
    ><button
      onclick={() => onmove(1)}
      class="icon-button"
      aria-label="下一篇 (↓)"
      title="下一篇 (↓)"><ArrowDown size={18} /></button
    ><span class="divider"></span><button
      class:active={article.read_at}
      onclick={() =>
        onpatch({ read: !article.read_at }).catch((e) => {
          error = e.message;
        })}
      class="icon-button"
      aria-label={article.read_at ? '标为未读 (a)' : '标为已读 (a)'}
      title={article.read_at ? '标为未读 (a)' : '标为已读 (a)'}><Check size={18} /></button
    ><button
      class:active={article.favorite}
      onclick={() =>
        onpatch({ favorite: !article.favorite }).catch((e) => {
          error = e.message;
        })}
      class="icon-button"
      aria-label={article.favorite ? '取消收藏 (s)' : '收藏文章 (s)'}
      title={article.favorite ? '取消收藏 (s)' : '收藏文章 (s)'}
      ><Star size={18} fill={article.favorite ? 'currentColor' : 'none'} /></button
    ><a
      href={externalURL(article.url)}
      target="_blank"
      rel="noopener noreferrer"
      class="icon-button"
      aria-label="打开原文 (e)"
      title="打开原文 (e)"><ExternalLink size={18} /></a
    >
  </div>
  <div class="reading-content">
    <div class="eyebrow">
      <a class="feed-link" href={sourceHref} onclick={(e) => onsource(e, article.feed_id)}
        ><FeedIcon {feed} /><span>{article.feed_title}</span></a
      ><span>·</span>{new Date(article.published_at || article.first_seen_at).toLocaleDateString(
        'zh-CN',
        { year: 'numeric', month: 'long', day: 'numeric' }
      )}
    </div>
    <h1 tabindex="-1">{article.title}</h1>
    {#if article.author}<p class="article-author">{article.author}</p>{/if}
    <div class="body-info">
      <span
        ><FileText size={14} />{content?.kind === 'fulltext'
          ? '已获取全文'
          : content
            ? '订阅源内容'
            : article.body_state === 'purged'
              ? '正文已清理'
              : '尚无正文'}</span
      >{#if article.content_expires_at}<span
          >正文保留至 {new Date(article.content_expires_at).toLocaleDateString('zh-CN')}</span
        >{/if}
    </div>
    {#if content}<div class="prose prose-stone dark:prose-invert article-body">
        {@html content.html}
      </div>{:else}<div class="empty-body">
        <FileText size={28} />
        <h2>
          {article.body_state === 'purged'
            ? '这篇文章的正文已按保存策略清理'
            : '这篇文章暂时没有正文'}
        </h2>
        <p>标题、笔记、标签和 Agent 摘要仍会保留。你可以打开原文，或显式尝试获取全文。</p>
      </div>{/if}
    <div class="fulltext-row">
      <button
        onclick={fulltext}
        disabled={!!job && ['queued', 'running', 'waiting_retry'].includes(job.state)}
        class="button secondary"><FileText size={16} />获取全文</button
      ><small>仅在你请求时访问原文，不会自动补抓。</small>
    </div>
    {#if job}<div class="notice" role="status">
        全文任务：{(
          {
            queued: '等待中',
            running: '获取中',
            waiting_retry: '稍后自动重试',
            succeeded: '已完成',
            failed: '失败'
          } as Record<string, string>
        )[job.state] || job.state}{job.error ? ` · ${job.error}` : ''}
        <button onclick={checkJob} class="text-button">更新状态</button>
      </div>{/if}
    {#if article.artifacts?.length}<section class="artifacts">
        <h2><Bookmark size={17} />Agent 整理</h2>
        {#each article.artifacts as artifact}<div class="artifact">
            <p class="artifact-meta">
              {artifact.actor} · {new Date(artifact.created_at).toLocaleDateString(
                'zh-CN'
              )}{artifact.basis_hash !== article.content_hash ? ' · 依据历史内容' : ''}
            </p>
            <p class="artifact-text">{artifact.content}</p>
            <details><summary>内容依据</summary><code>{artifact.basis_hash}</code></details>
          </div>{/each}
      </section>{/if}
    <section class="notes">
      <h2>我的笔记</h2>
      <textarea aria-label="我的笔记" bind:value={note} placeholder="留下你的想法…" rows="4"
      ></textarea><label for="article-tags">标签</label><input
        id="article-tags"
        bind:value={tags}
        placeholder="用逗号分隔，例如：技术, 待读"
      />
      <div class="note-actions">
        <span>个人记录与源正文分别保存</span><button class="button" onclick={save} disabled={saving}
          >{#if saving}<LoaderCircle size={15} class="spin" />{/if}保存笔记</button
        >
      </div>
    </section>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </div>
</article>
