<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import type { AgentSettings, AgentRun, AgentUsage } from './models';
  let settings = $state<AgentSettings | null>(null),
    key = $state(''),
    clearKey = $state(false),
    busy = $state(false),
    error = $state(''),
    message = $state(''),
    usage = $state<AgentUsage | null>(null),
    test = $state<AgentRun | null>(null);
  onMount(() => {
    let stopped = false;
    void load();
    const timer = setInterval(async () => {
      if (!test || !['queued', 'running', 'waiting_retry'].includes(test.state)) return;
      try {
        const next = await api.agentRun(test.id);
        if (!stopped) test = next;
      } catch (e) {
        if (!stopped) error = (e as Error).message;
      }
    }, 2000);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  });
  async function load() {
    try {
      settings = await api.agentSettings();
      usage = await api.agentUsage();
    } catch (e) {
      error = (e as Error).message;
    }
  }
  async function save() {
    if (!settings) return;
    busy = true;
    error = '';
    message = '';
    try {
      settings = await api.saveAgentSettings(settings, clearKey ? '' : key || undefined);
      key = '';
      clearKey = false;
      message = 'Agent 配置已保存';
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
  async function check() {
    busy = true;
    error = '';
    try {
      await save();
      if (!error) test = await api.agentTest();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

<div class="agent-config">
  <h2>Agent 配置</h2>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if message}<p class="notice" role="status">{message}</p>{/if}
  {#if settings}
    <form
      onsubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <label class="checkbox-label"
        ><input type="checkbox" bind:checked={settings.enabled} />启用 Agent</label
      >
      <label
        >Base URL<input
          type="url"
          bind:value={settings.base_url}
          placeholder="https://api.example.com/v1"
        /></label
      >
      <label
        >接口协议<select bind:value={settings.protocol}
          ><option value="responses">Responses（流式）</option><option value="chat_completions"
            >Chat Completions（流式）</option
          ></select
        ></label
      >
      <label>模型<input bind:value={settings.model} placeholder="gpt-6-luna" /></label>
      <label
        >API Key<input
          type="password"
          autocomplete="new-password"
          bind:value={key}
          placeholder={settings.key_configured
            ? '已保存；留空保留'
            : '填写服务密钥（仅保存于服务端）'}
        /></label
      >
      <label class="checkbox-label"
        ><input type="checkbox" bind:checked={clearKey} />清除已保存的 Key</label
      >
      <label class="checkbox-label"
        ><input
          type="checkbox"
          bind:checked={settings.schedule_enabled}
        />每天自动生成未读晨报</label
      >
      <div class="pair">
        <label>时间<input type="time" bind:value={settings.daily_time} required /></label><label
          >时区<input bind:value={settings.timezone} required /></label
        >
      </div>
      <p class="form-help">
        未配置地址或模型时功能停用。晨报包含归档中的未读文章，生成与阅读报告均不改变已读状态。
      </p>
      <details>
        <summary>预算与处理深度</summary>
        <div class="pair">
          <label
            >每日 Token 上限<input
              type="number"
              min="4096"
              max="1000000000"
              bind:value={settings.daily_tokens}
            /></label
          ><label
            >每日调用上限<input
              type="number"
              min="1"
              max="10000"
              bind:value={settings.daily_calls}
            /></label
          >
        </div>
        <div class="pair">
          <label
            >上下文 Token 上限<input
              type="number"
              min="4096"
              max="2000000"
              bind:value={settings.context_limit}
            /></label
          ><label
            >单次输出 Token 上限<input
              type="number"
              min="256"
              max="32768"
              bind:value={settings.max_output}
            /></label
          >
        </div>
        <div class="pair">
          <label
            >普通源截取字符<input
              type="number"
              min="100"
              max="20000"
              bind:value={settings.normal_chars}
            /></label
          ><label
            >次要源截取字符<input
              type="number"
              min="0"
              max="5000"
              bind:value={settings.low_chars}
            /></label
          >
        </div>
        <label
          >任务时限（分钟）<input
            type="number"
            min="1"
            max="180"
            bind:value={settings.run_minutes}
          /></label
        >
        <label
          >结构化输出模式<select bind:value={settings.output_mode}
            ><option value="text_json">提示词约束 JSON（兼容性优先）</option><option
              value="json_object">JSON Object</option
            ><option value="json_schema">JSON Schema</option></select
          ></label
        >
        <p class="form-help">
          重要源处理全文的全部分块。正文缺失、超时或达到预算时，报告显示实际覆盖与缺口。Token
          额度不是金额预算。
        </p>
      </details>
      <div class="button-row">
        <button class="button" disabled={busy}>保存配置</button><button
          type="button"
          class="button secondary"
          disabled={busy || !settings.enabled}
          onclick={check}>保存并测试连接</button
        >
      </div>
    </form>
    {#if test}<p class="notice" role="status">
        连接测试：{{
          queued: '排队中',
          running: '调用中',
          waiting_retry: '等待重试',
          complete: '通过',
          failed: '失败',
          cancelled: '已取消',
          partial: '部分完成'
        }[test.state] || test.state}
        {test.error}
      </p>{/if}
    {#if settings.next_report_at}<p class="form-help">
        下次晨报：{new Date(settings.next_report_at).toLocaleString('zh-CN', {
          timeZone: settings.timezone
        })}（{settings.timezone}）
      </p>{/if}
    {#if usage}<p class="form-help">
        今日：{usage.calls} 次调用 / {usage.tokens.toLocaleString()} Token；其中 {usage.unknown_calls}
        次按预留额度记账。
      </p>{/if}
  {:else}<p>正在读取配置…</p>{/if}
</div>

<style>
  .agent-config {
    max-width: 700px;
  }
  .agent-config h2 {
    margin: 0 0 20px;
  }
  form {
    display: grid;
    gap: 16px;
  }
  label {
    display: grid;
    gap: 7px;
    font-size: 14px;
  }
  .pair {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
  }
  details {
    border: 1px solid var(--line);
    padding: 14px;
    border-radius: 8px;
  }
  details label,
  details .pair {
    margin-top: 12px;
  }
  summary {
    cursor: pointer;
  }
  input,
  select {
    width: 100%;
    min-width: 0;
  }
  input[type='checkbox'] {
    width: auto;
  }
  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 9px;
  }
  .form-help {
    color: var(--muted);
    font-size: 13px;
    line-height: 1.7;
    margin: 10px 0;
  }
  .button-row {
    flex-wrap: wrap;
  }
  @media (max-width: 600px) {
    .pair {
      grid-template-columns: 1fr;
    }
  }
</style>
