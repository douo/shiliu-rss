<script lang="ts">
  import FeedIcon from './FeedIcon.svelte';
  import HoverDetails from './HoverDetails.svelte';
  import type { Feed } from './models';

  let {
    feed,
    selected = false,
    onselect
  }: { feed: Feed; selected?: boolean; onselect: (id: string) => void } = $props();
  const explanations: Record<string, string> = {
    dns_failed: '域名解析失败，未能取得源站的 IP 地址。',
    connect_failed: '连接源站失败，可能是站点不可达或连接超时。',
    network_failed: '网络请求未完成，可能涉及超时、TLS 或连接中断。',
    read_failed: '读取响应内容失败，或响应体超过大小限制。',
    response_too_large: '响应内容超过系统允许的大小。',
    invalid_feed: '响应内容未能解析为有效的 RSS、Atom 或 JSON Feed。',
    too_many_entries: '订阅内容中的文章数量超过单次解析上限。',
    invalid_url: '订阅地址无效，或使用了不受支持的协议。',
    invalid_address: '目标网络地址无效。',
    private_address_blocked: '目标解析到受限制的内网或保留地址，请求已被网络策略阻止。',
    too_many_redirects: '跳转次数超过系统上限。',
    invalid_redirect: '源站返回了无效或不允许的跳转地址。'
  };
  const explanation = $derived.by(() => {
    const code = feed.error;
    if (explanations[code]) return explanations[code];
    const status = /^http_(\d{3})$/.exec(code)?.[1];
    if (status === '404') return '源站返回 HTTP 404，当前订阅地址未找到。';
    if (status === '403') return '源站返回 HTTP 403，拒绝了此次订阅请求。';
    if (status === '401') return '源站返回 HTTP 401，此地址要求认证。';
    if (status === '429') return '源站返回 HTTP 429，请求频率受到限制。';
    if (status && Number(status) >= 500) return `源站返回 HTTP ${status}，服务器暂时无法完成请求。`;
    if (status) return `源站返回 HTTP ${status}，此次抓取未成功。`;
    return '服务记录了以下异常，当前没有更细的原因说明。';
  });
  const time = (value: string | null) =>
    value ? new Date(value).toLocaleString('zh-CN') : '尚无记录';
</script>

<div class="sidebar-feed" class:selected>
  <button
    class="source-link"
    class:chosen={selected}
    aria-label={feed.title || '未命名订阅'}
    aria-current={selected ? 'page' : undefined}
    title={feed.title}
    onclick={() => onselect(feed.id)}
  >
    <FeedIcon {feed} /><span class="source-title">{feed.title || '未命名订阅'}</span>
    {#if feed.unread_count > 0}<small
        class="unread-count"
        aria-label={`${feed.unread_count} 篇未读`}
        title={`${feed.unread_count} 篇未读（含归档）`}>{feed.unread_count}</small
      >{/if}
  </button>
  {#if feed.error}<HoverDetails
      label={`查看${feed.title || '未命名订阅'}的异常`}
      heading="订阅源异常"
      appearance="status"
      closeLabel="关闭异常说明"
    >
      <p><strong>{feed.title || '未命名订阅'}</strong></p>
      <p>{explanation}</p>
      <h4>服务记录</h4>
      <p><code>{feed.error}</code></p>
      <dl>
        <div>
          <dt>最近尝试</dt>
          <dd>{time(feed.last_attempt_at)}</dd>
        </div>
        <div>
          <dt>最近成功</dt>
          <dd>{time(feed.last_success_at)}</dd>
        </div>
        <div>
          <dt>{feed.paused ? '更新状态' : '预计下次检查'}</dt>
          <dd>{feed.paused ? '已暂停' : time(feed.next_check_at)}</dd>
        </div>
      </dl>
      <p>
        {feed.paused
          ? '此源已暂停自动更新，恢复后才会继续检查。'
          : '后台会按失败退避和站点限流策略继续尝试；已收录的文章仍可阅读。'}
      </p>
    </HoverDetails>{/if}
</div>

<style>
  .sidebar-feed {
    display: flex;
    align-items: center;
    min-width: 0;
    border-radius: 6px;
  }
  .sidebar-feed:hover,
  .sidebar-feed.selected {
    background: var(--accent-soft);
  }
  .sidebar-feed > .source-link {
    display: flex;
    flex: 1;
    min-width: 0;
    width: auto;
    align-items: center;
    gap: 9px;
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 12px;
    text-align: left;
    line-height: 1.5;
  }
  .source-title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .unread-count {
    flex-shrink: 0;
    margin-left: auto;
    padding-left: 3px;
    color: var(--muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    line-height: 1.5;
  }
  .sidebar-feed:has(:global(.status-trigger)) > .source-link {
    padding-right: 2px;
  }
</style>
