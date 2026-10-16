<script lang="ts">
  import { X, Pencil, Rss } from '@lucide/svelte';
  import { modal } from './modal';
  import { externalURL } from './links';
  import FeedIcon from './FeedIcon.svelte';
  import type { Feed, Category } from './models';

  let {
    feed,
    category,
    feeds,
    categories,
    onclose,
    oneditfeed,
    oneditcategory,
    onfeedselect
  }: {
    feed?: Feed;
    category?: Category;
    feeds: Feed[];
    categories: Category[];
    onclose: () => void;
    oneditfeed: (id: string) => void;
    oneditcategory: (id: string) => void;
    onfeedselect: (id: string) => void;
  } = $props();
  const members = $derived(feeds.filter((f) => f.category_id === category?.id));
  const importance = { high: '重要', normal: '普通', low: '次要' };
  const time = (value: string | null) =>
    value ? new Date(value).toLocaleString('zh-CN') : '尚无记录';
</script>

<div class="modal-shade">
  <div
    class="modal list-details"
    role="dialog"
    aria-modal="true"
    aria-labelledby="list-details-heading"
    tabindex="-1"
    use:modal={onclose}
  >
    <header class="section-title">
      <h2 id="list-details-heading">{feed ? '订阅源详情' : '分类详情'}</h2>
      <button class="icon-button" aria-label="关闭详情" onclick={onclose}><X size={19} /></button>
    </header>
    <div class="context-heading">
      {#if feed}<FeedIcon {feed} />{:else}<Rss size={19} />{/if}
      <h3>{feed?.title || category?.title}</h3>
    </div>
    {#if feed}
      <dl>
        <div>
          <dt>分类</dt>
          <dd>{categories.find((c) => c.id === feed?.category_id)?.title || '未分类'}</dd>
        </div>
        <div>
          <dt>全部未读</dt>
          <dd>{feed.unread_count} 篇（含归档）</dd>
        </div>
        <div>
          <dt>订阅地址</dt>
          <dd>
            {#if externalURL(feed.url)}<a
                href={externalURL(feed.url)!}
                target="_blank"
                rel="noopener noreferrer">{feed.url}</a
              >{:else}{feed.url}{/if}
          </dd>
        </div>
        {#if externalURL(feed.site_url)}<div>
            <dt>源站</dt>
            <dd>
              <a href={externalURL(feed.site_url)!} target="_blank" rel="noopener noreferrer"
                >{feed.site_url}</a
              >
            </dd>
          </div>{/if}
        <div>
          <dt>更新状态</dt>
          <dd>
            {feed.paused ? '已暂停' : feed.error ? '抓取异常，等待重试' : '自动更新'} · 每 {feed.interval_hours}
            小时检查
          </dd>
        </div>
        {#if feed.error}<div>
            <dt>最近异常</dt>
            <dd><code>{feed.error}</code></dd>
          </div>{/if}
        <div>
          <dt>最近尝试</dt>
          <dd>{time(feed.last_attempt_at)}</dd>
        </div>
        <div>
          <dt>最近成功</dt>
          <dd>{time(feed.last_success_at)}</dd>
        </div>
        {#if !feed.paused}<div>
            <dt>下次检查</dt>
            <dd>{time(feed.next_check_at)}</dd>
          </div>{/if}
        <div>
          <dt>晨报重要度</dt>
          <dd>{importance[feed.importance]}</dd>
        </div>
        <div>
          <dt>文章可见期</dt>
          <dd>已读 {feed.read_days} 天 · 未读 {feed.unread_days} 天；收藏豁免</dd>
        </div>
        <div>
          <dt>正文保存</dt>
          <dd>
            {feed.archive
              ? '长期保存已取得的正文'
              : '普通保存：首次收录起 90 天；迁入保护和显式重取以文章期限为准'}；收藏正文保留
          </dd>
        </div>
      </dl>
      <footer>
        <button class="button" onclick={() => oneditfeed(feed!.id)}
          ><Pencil size={16} />编辑订阅</button
        >
      </footer>
    {:else if category}
      <p class="category-summary">
        {members.length} 个订阅源 · {members.reduce((n, f) => n + f.unread_count, 0)} 篇未读（含归档）
      </p>
      <div class="context-members" aria-label="分类内订阅源">
        {#each members as member (member.id)}
          <div class="context-member">
            <button class="member-source" onclick={() => onfeedselect(member.id)}
              ><FeedIcon feed={member} /><span>{member.title}</span>{#if member.unread_count}<small
                  >{member.unread_count}</small
                >{/if}</button
            >
            <button
              class="icon-button"
              aria-label={`编辑 ${member.title}`}
              onclick={() => oneditfeed(member.id)}><Pencil size={16} /></button
            >
          </div>
        {:else}<p class="empty">此分类暂无订阅源</p>{/each}
      </div>
      <footer>
        <button class="button" onclick={() => oneditcategory(category!.id)}
          ><Pencil size={16} />编辑分类</button
        >
      </footer>
    {/if}
  </div>
</div>

<style>
  .list-details {
    width: min(620px, calc(100vw - 32px));
    max-width: 620px;
  }
  .context-heading {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 6px 0 18px;
  }
  .context-heading h3 {
    min-width: 0;
    overflow-wrap: anywhere;
    font-size: 18px;
    font-weight: 600;
  }
  dl {
    margin: 0;
    font-size: 13px;
    line-height: 1.7;
  }
  dl > div {
    display: grid;
    grid-template-columns: 88px minmax(0, 1fr);
    gap: 16px;
    padding: 9px 0;
    border-bottom: 1px solid var(--line);
  }
  dt,
  .category-summary,
  .empty {
    color: var(--muted);
  }
  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  dd a {
    color: var(--accent);
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  code {
    font-size: 12px;
  }
  .category-summary {
    font-size: 13px;
    margin: 0 0 16px;
  }
  .context-members {
    border-top: 1px solid var(--line);
  }
  .context-member {
    display: flex;
    align-items: center;
    gap: 12px;
    border-bottom: 1px solid var(--line);
    padding: 9px 0;
  }
  .member-source {
    display: flex;
    flex: 1;
    min-width: 0;
    align-items: center;
    gap: 10px;
    padding: 6px 0;
    text-align: left;
    font-size: 13px;
  }
  .member-source span {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .member-source small {
    margin-left: auto;
    flex-shrink: 0;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .empty {
    padding: 20px 0;
    font-size: 13px;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    padding-top: 22px;
  }
  @media (max-width: 520px) {
    dl > div {
      grid-template-columns: 72px minmax(0, 1fr);
      gap: 12px;
    }
  }
</style>
