<script lang="ts">
  import type { AgentBlock } from './models';
  let {
    blocks = [],
    reportId = '',
    prefix = '',
    onarticle
  }: {
    blocks?: AgentBlock[];
    reportId?: string;
    prefix?: string;
    onarticle?: (event: MouseEvent, articleId: string, reportId: string, anchor: string) => void;
  } = $props();
  function articleURL(articleId: string, url: string, anchor: string) {
    if (!reportId) return url;
    return `/?${new URLSearchParams({ scope: 'all', article: articleId, report: reportId, anchor })}`;
  }
</script>

{#each blocks as block, i}<div class:limitation={block.kind === 'limitation'} class="agent-block">
    <p>
      {#if block.kind === 'analysis'}<span class="kind">分析</span>{/if}{block.text}
    </p>
    {#if block.citations?.length}<div class="citations">
        {#each block.citations as c, j}{@const anchor = `${prefix}-b${i}-c${j}`}
          <a
            id={reportId ? anchor : undefined}
            href={articleURL(c.article_id, c.url, anchor)}
            onclick={(e) => onarticle?.(e, c.article_id, reportId, anchor)}>{c.title}</a
          >{/each}
      </div>{/if}
  </div>{/each}

<style>
  .agent-block {
    margin: 14px 0;
    line-height: 1.85;
  }
  .agent-block p {
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .kind {
    font-size: 11px;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 3px;
    padding: 1px 4px;
    margin-right: 8px;
  }
  .limitation {
    color: var(--muted);
    font-size: 13px;
  }
  .citations {
    display: flex;
    flex-wrap: wrap;
    gap: 5px 12px;
    margin-top: 5px;
    font-size: 12px;
  }
  .citations a {
    color: var(--accent);
    text-decoration: underline;
    text-underline-offset: 3px;
  }
</style>
