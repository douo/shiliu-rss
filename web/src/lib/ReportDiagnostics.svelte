<script lang="ts">
  import type { AgentRun } from './models';
  let {
    report,
    message = '',
    scope = ''
  }: { report?: AgentRun | null; message?: string; scope?: string } = $props();
  const reasons: Record<string, string> = {
    daily_budget_exhausted:
      '当日调用次数或 token 预算不足，后续模型处理已停止；可调整预算或在预算重置后续作。',
    run_deadline_exceeded: '本次任务已超过配置的运行窗口，已保存的摘要保留；续作会复用已完成结果。',
    invalid_model_output:
      '模型输出在重试后仍未通过 JSON 结构、引用 ID 或内容格式校验，该步骤的输出未被采用。',
    context_limit_exceeded: '请求内容与预留输出超过配置的上下文上限，该模型请求未发送。',
    provider_connection_failed: '无法连接模型服务，或请求连接超时。',
    api_key_unavailable: '无法取得已配置的模型密钥。',
    configuration_changed: 'Agent 配置已变更，原任务按旧配置停止。',
    agent_processing_failed: '处理出现未分类的内部错误。',
    waiting_for_fulltext: '重要源的全文获取任务尚未结束，晨报正在等待正文。'
  };
  const counts = $derived(report?.result?.counts || {});
  const traces = $derived(report?.result?.trace || []);
  const recordedReasons = $derived([
    ...new Set([
      ...(report?.error ? [report.error] : []),
      ...traces.flatMap(
        (text) =>
          text.match(
            /(?:invalid_model_output|daily_budget_exhausted|run_deadline_exceeded|context_limit_exceeded|provider_[a-z_0-9]+|api_key_unavailable|agent_processing_failed|configuration_changed)/g
          ) || []
      )
    ])
  ]);
  function describe(code: string) {
    return (
      reasons[code] ||
      (/^provider_http_\d+$/.test(code)
        ? `模型服务返回 HTTP ${code.slice(-3)}，请求未成功。`
        : '服务记录了此错误码，但没有更细的原因；不能据此推断具体故障。')
    );
  }
</script>

{#if message}<p>{message}</p>{/if}
{#if scope}<p>当前范围：{scope}。下列统计为整份晨报的成员统计。</p>{/if}
{#if report}
  <dl>
    <div>
      <dt>任务 / 配置版本</dt>
      <dd>#{report.id} / v{report.config_version}</dd>
    </div>
    <div>
      <dt>快照截止</dt>
      <dd>{new Date(report.cutoff).toLocaleString('zh-CN')}</dd>
    </div>
    <div>
      <dt>成员处理</dt>
      <dd>{report.completed} / {report.total} 篇</dd>
    </div>
    <div>
      <dt>实质摘要</dt>
      <dd>
        {counts.summarized_articles ?? '未记录'} 篇；其中未读 {counts.unread_summarized ?? '未记录'} 篇
      </dd>
    </div>
    <div>
      <dt>待处理 / 不可用</dt>
      <dd>{counts.pending || 0} / {counts.unavailable || 0} 篇</dd>
    </div>
    <div>
      <dt>降级 / 全文依据</dt>
      <dd>{counts.fallback || 0} / {counts.fulltext || 0} 篇</dd>
    </div>
  </dl>
  <h4>执行状态与已记录原因</h4>
  <p>
    {#if ['queued', 'running', 'waiting_retry'].includes(report.state)}任务仍在后台运行，统计会继续变化。{:else if report.state === 'partial'}任务已以部分完成结束，当前没有自动续作；可使用“续作
      / 重试”继续。{:else if report.state === 'complete'}任务已完成。完成表示处理流程结束，不保证每篇均使用全文。{:else}任务已结束，状态码为
      {report.state}。{/if}
  </p>
  {#each recordedReasons as code}<p><code>{code}</code>：{describe(code)}</p>{/each}
  {#if report.state === 'partial' && !recordedReasons.length}<p>
      此报告没有记录具体失败码。成员统计只能说明覆盖范围，不能确定聚合失败的底层原因；旧报告缺失的诊断信息无法追补。
    </p>{/if}
{/if}
<h4>处理流程与数据边界</h4>
<p>
  晨报先固定截止时全部未读（含归档），再生成或复用单篇摘要，最后分批聚合并按分类、来源整理。“已处理”包含降级和失败提示，不等于全部成功总结；“全文”表示使用全文依据，不是所有文章均有全文。
</p>
<p>
  聚合失败时可能只显示每个来源前 8
  个块，单篇已保存摘要仍保留在“查看文章与覆盖明细”。卡片是派生摘要，并不保证包含原文；失败提示卡片不算实质摘要。
</p>
<p>
  重要源可按策略补抓全文；正文缺失、已清理或抓取失败时会降级。普通源和次要源按配置使用摘要、截取文本或标题，不能据此宣称已阅读全文。
</p>
{#if traces.length}<h4>服务端处理记录</h4>
  <ul>
    {#each traces as trace}<li>{trace}</li>{/each}
  </ul>{/if}
