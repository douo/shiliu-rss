import { test, expect } from '@playwright/test';
import type { AgentSettings, AgentRun, AgentConversation, AgentMessage } from '../src/lib/models';
const now = new Date().toISOString();
test('Agent: configure, report citations, and first chat message', async ({ page }, info) => {
  let settings: AgentSettings = {
    version: 1,
    enabled: false,
    configured: false,
    key_configured: false,
    name: '阅读助理',
    base_url: '',
    model: '',
    protocol: 'responses',
    output_mode: 'text_json',
    context_limit: 64000,
    max_output: 4096,
    daily_tokens: 1000000,
    daily_calls: 200,
    normal_chars: 3000,
    low_chars: 500,
    run_minutes: 30,
    schedule_enabled: true,
    daily_time: '08:00',
    timezone: 'Asia/Shanghai',
    next_report_at: null
  };
  const result = {
    blocks: [
      {
        kind: 'fact',
        text: '合成索引项目改为增量更新。',
        evidence_ids: ['a1'],
        citations: [
          {
            article_id: '1',
            title: '合成索引文章',
            url: '/?scope=all&article=1',
            basis_hash: 'synthetic',
            kind: 'feed_excerpt'
          }
        ]
      }
    ],
    groups: [],
    counts: { total: 1, summarized: 1, reused: 0, fulltext: 0, pending: 0 },
    trace: ['完整未读快照'],
    degraded: false
  };
  const report: AgentRun = {
    id: '1',
    kind: 'report',
    state: 'complete',
    error: '',
    conversation_id: '',
    high_watermark: '1',
    config_version: 2,
    total: 1,
    completed: 1,
    cutoff: now,
    created_at: now,
    updated_at: now,
    due_at: now,
    result
  };
  const conversations: AgentConversation[] = [];
  let messages: AgentMessage[] = [];
  let savedSecret = false;
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname,
      method = req.method();
    let body: unknown = {};
    if (path === '/api/v1/session') body = { authenticated: true, csrf_token: 'synthetic-csrf' };
    else if (path === '/api/v1/agent/settings') {
      if (method === 'PUT') {
        const input = req.postDataJSON();
        expect(input.api_key).toBe('synthetic-key');
        settings = {
          ...input.settings,
          version: settings.version + 1,
          configured: true,
          key_configured: true
        };
        savedSecret = true;
      }
      body = settings;
    } else if (path === '/api/v1/agent/usage') body = { tokens: 0, calls: 0, unknown_calls: 0 };
    else if (path === '/api/v1/agent/runs') body = [];
    else if (path === '/api/v1/agent/report-runs') body = report;
    else if (path === '/api/v1/agent/conversations') {
      if (method === 'POST') {
        const c = { id: 'c1', title: '新对话', report_id: '', created_at: now, updated_at: now };
        conversations.push(c);
        body = c;
      } else body = conversations;
    } else if (path === '/api/v1/agent/conversations/c1/messages') {
      if (method === 'POST') {
        expect(req.postDataJSON().text).toBe('SQLite 更新了什么？');
        messages = [
          {
            id: '1',
            role: 'user',
            text: req.postDataJSON().text,
            result: { ...result, blocks: [] },
            run_id: '2',
            created_at: now
          },
          { id: '2', role: 'assistant', text: '', result, run_id: '2', created_at: now }
        ];
        body = { ...report, id: '2', kind: 'chat', conversation_id: 'c1' };
      } else body = messages;
    } else if (path === '/api/v1/agent/reports/1/members') body = [];
    else throw new Error(`Unexpected request: ${method} ${path}`);
    await route.fulfill({
      status: method === 'POST' ? (path.endsWith('/conversations') ? 201 : 202) : 200,
      contentType: 'application/json',
      body: JSON.stringify(body)
    });
  });
  await page.goto('/agent');
  await page.getByRole('button', { name: '模型配置', exact: true }).click();
  await page.getByLabel('Base URL', { exact: true }).fill('http://localhost:1234/v1');
  await page.getByLabel('模型', { exact: true }).fill('gpt-6-luna');
  await page.getByLabel('API Key', { exact: true }).fill('synthetic-key');
  await page.getByLabel('启用 Agent', { exact: true }).check();
  await page.getByRole('button', { name: '保存配置', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('配置已保存');
  expect(savedSecret).toBe(true);
  await expect(page.getByLabel('API Key', { exact: true })).toHaveValue('');
  await page.getByRole('button', { name: '晨报', exact: true }).click();
  await page.getByRole('button', { name: '生成未读晨报', exact: true }).click();
  await expect(page.getByText('合成索引项目改为增量更新。')).toBeVisible();
  await expect(page.getByRole('link', { name: '合成索引文章' })).toHaveAttribute(
    'href',
    '/?scope=all&article=1'
  );
  await page.getByRole('button', { name: '对话', exact: true }).click();
  await page.getByLabel('向阅读助理提问').fill('SQLite 更新了什么？');
  await page.getByLabel('向阅读助理提问').press('Enter');
  await expect(page.getByText('SQLite 更新了什么？', { exact: true })).toBeVisible();
  await expect(page.getByText('合成索引项目改为增量更新。')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true
  );
  await page.screenshot({ path: `test-results/${info.project.name}-agent.png`, fullPage: true });
});
