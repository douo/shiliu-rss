<script lang="ts">
  import type { AgentBlock, AgentRun } from './models';
  import HoverDetails from './HoverDetails.svelte';
  import ReportDiagnostics from './ReportDiagnostics.svelte';
  let {
    blocks = [],
    reportId = '',
    prefix = '',
    onarticle,
    diagnosticReport,
    diagnosticScope = ''
  }: {
    blocks?: AgentBlock[];
    reportId?: string;
    prefix?: string;
    diagnosticReport?: AgentRun;
    diagnosticScope?: string;
    onarticle?: (event: MouseEvent, articleId: string, reportId: string, anchor: string) => void;
  } = $props();
  function articleURL(articleId: string, url: string, anchor: string) {
    if (!reportId) return url;
    return `/?${new URLSearchParams({ scope: 'all', article: articleId, report: reportId, anchor })}`;
  }
</script>

{#each blocks as block, i}<div class:limitation={block.kind === 'limitation'} class="agent-block">
    {#if block.kind === 'limitation' && block.text !== '截止时没有未读文章。'}
      <HoverDetails
        label={block.text.includes('聚合')
          ? '聚合未完成 · 查看原因'
          : diagnosticReport
            ? '覆盖与处理说明'
            : '资料限制 · 查看说明'}
        heading="处理限制与技术报告"
        >{#if diagnosticReport}<ReportDiagnostics
            report={diagnosticReport}
            message={block.text}
            scope={diagnosticScope}
          />{:else}<p>{block.text}</p>
          <p>
            这段内容仅说明资料或处理限制，不代表已经完成文章内容总结。引用链接可用于查看对应文章。
          </p>{/if}</HoverDetails
      >
    {:else}<p>
        {#if block.kind === 'analysis'}<span class="kind">分析</span>{/if}{block.text}
      </p>{/if}
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
    margin: 18px 0;
    font-size: 16px;
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
    gap: 8px 14px;
    margin-top: 8px;
    font-size: 12px;
    line-height: 1.7;
  }
  .citations a {
    color: var(--accent);
    text-decoration: underline;
    text-underline-offset: 3px;
  }
</style>
