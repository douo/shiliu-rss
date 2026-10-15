import { test, expect, type Page } from '@playwright/test';
const now = new Date().toISOString();
const feed = {
  id: '1',
  url: 'https://example.test/feed',
  title: '合成阅读源',
  site_url: 'https://example.test',
  category_id: '',
  allow_host: '',
  paused: false,
  archive: false,
  interval_hours: 5,
  read_days: 7,
  unread_days: 30,
  last_attempt_at: now,
  last_success_at: now,
  next_check_at: now,
  error: '',
  version: 1
};
const article = (id: string) => ({
  id,
  feed_id: '1',
  feed_title: feed.title,
  title: `合成文章 ${id}`,
  url: 'https://example.test/article',
  author: '测试作者',
  published_at: now,
  first_seen_at: now,
  read_at: null as string | null,
  favorite: false,
  hidden: false,
  removed: false,
  note: '',
  tags: [] as string[],
  version: 1,
  body_state: 'available',
  content_hash: 'synthetic-hash',
  provenance: 'feed',
  read_at_unknown: false,
  imported_at: null,
  protection_until: null,
  content_expires_at: null as string | null,
  content: [
    {
      kind: 'feed',
      html: '<p>只用于浏览器测试的中文正文。</p>',
      text: '只用于浏览器测试的中文正文。',
      hash: 'synthetic-hash',
      state: 'available',
      obtained_at: now,
      expires_at: null as string | null
    }
  ],
  artifacts: [
    {
      id: '1',
      article_id: id,
      actor: '测试 Agent',
      external_id: 'test',
      kind: 'summary',
      content: '保留的独立摘要',
      basis_hash: 'synthetic-hash',
      created_at: now,
      version: 1
    }
  ]
});
async function mock(page: Page, { expires = false, conflict = false } = {}) {
  const items = Array.from({ length: 30 }, (_, i) => article(String(i + 1)));
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname;
    let result: unknown;
    if (path.endsWith('/session')) result = { authenticated: true, csrf_token: 'synthetic-csrf' };
    else if (path.endsWith('/feeds')) result = [feed];
    else if (path.endsWith('/categories')) result = [];
    else if (path.endsWith('/articles')) result = { items, next_cursor: '', high_watermark: '30' };
    else if (/\/articles\/\d+$/.test(path)) {
      const a = items.find((x) => x.id === path.split('/').at(-1))!;
      if (req.method() === 'PATCH') {
        const body = req.postDataJSON();
        if (conflict && body.note) {
          await route.fulfill({ status: 412, json: { title: '文章已更新，请重新读取' } });
          return;
        }
        if (body.read !== undefined) a.read_at = body.read ? now : null;
        if (body.favorite !== undefined) a.favorite = body.favorite;
        if (body.note !== undefined) a.note = body.note;
        if (body.tags !== undefined) a.tags = body.tags;
        a.version++;
      }
      if (expires) {
        a.content[0].expires_at = new Date(Date.now() + 500).toISOString();
        a.content_expires_at = a.content[0].expires_at;
      }
      result = a;
    } else if (path.endsWith('/tokens')) result = [];
    else result = {};
    await route.fulfill({ json: result });
  });
  return items;
}
test('reading preserves list position, keyboard state, and deep links', async ({ page }, info) => {
  const items = await mock(page);
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '近期文章', exact: true })).toBeVisible();
  const list = page.locator('.article-list');
  await list.evaluate((el) => (el.scrollTop = 550));
  const previous = await list.evaluate((el) => el.scrollTop);
  await page.screenshot({ path: `test-results/${info.project.name}-list.png` });
  await page.locator('[data-article-id="10"]').click();
  await expect(page.getByRole('heading', { name: '合成文章 10', exact: true })).toBeVisible();
  await expect.poll(() => items[9].read_at).toBeTruthy();
  if (info.project.name === 'desktop') {
    await page.keyboard.press('s');
    await expect.poll(() => items[9].favorite).toBe(true);
    await page.keyboard.press('j');
    await expect(page.getByRole('heading', { name: '合成文章 11', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
  } else await page.getByRole('button', { name: '返回列表 (Esc)', exact: true }).click();
  await expect(list).toBeVisible();
  expect(await list.evaluate((el) => el.scrollTop)).toBeGreaterThanOrEqual(previous - 100);
  await page.goto('/?scope=all&article=2');
  await expect(page.getByRole('heading', { name: '合成文章 2', exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole('heading', { name: '近期文章', exact: true })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole('heading', { name: '合成文章 2', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: '合成文章 2', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true
  );
  await page.screenshot({ path: `test-results/${info.project.name}-reader.png`, fullPage: true });
});
test('expiry removes visible body while preserving notes and artifacts', async ({ page }) => {
  await mock(page, { expires: true });
  await page.goto('/?article=1');
  await expect(page.getByText('只用于浏览器测试的中文正文。')).toBeVisible();
  await expect(page.getByText('这篇文章的正文已按保存策略清理')).toBeVisible();
  await expect(page.getByText('只用于浏览器测试的中文正文。')).toHaveCount(0);
  await expect(page.getByText('保留的独立摘要')).toBeVisible();
});
test('conflicting note save reports failure and preserves unsaved input', async ({ page }) => {
  await mock(page, { conflict: true });
  await page.goto('/?article=1');
  await page.getByRole('textbox', { name: '我的笔记', exact: true }).fill('未保存的想法');
  await page.getByRole('button', { name: '保存笔记', exact: true }).click();
  await expect(page.getByRole('alert').first()).toContainText('文章已更新');
  await expect(page.getByRole('textbox', { name: '我的笔记', exact: true })).toHaveValue(
    '未保存的想法'
  );
});

test('token scopes, one-time secret, and revocation stay in memory', async ({ page }) => {
  await mock(page);
  let record = {
    id: 'token-1',
    name: '合成整理助手',
    scopes: ['articles:read', 'artifacts:write'],
    created_at: now,
    revoked_at: null as string | null
  };
  let created = false;
  await page.route('**/api/v1/tokens**', async (route) => {
    const request = route.request();
    expect(request.headers()['x-csrf-token'] || request.method()).toBe(
      request.method() === 'GET' ? 'GET' : 'synthetic-csrf'
    );
    if (request.method() === 'POST') {
      expect(request.postDataJSON().scopes).toEqual(record.scopes);
      created = true;
      await route.fulfill({ status: 201, json: { token: 'synthetic-once-secret', record } });
    } else if (request.method() === 'DELETE') {
      record.revoked_at = now;
      await route.fulfill({ status: 204 });
    } else await route.fulfill({ json: created ? [record] : [] });
  });
  await page.goto('/?settings=true');
  await page.getByRole('button', { name: 'Agent 授权', exact: true }).click();
  await page.getByLabel('用途名称').fill(record.name);
  await page.getByLabel('写入摘要与分析').check();
  await page.getByRole('button', { name: '创建令牌', exact: true }).click();
  await expect(page.getByText('synthetic-once-secret', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => Object.values(localStorage).join(''))).not.toContain(
    'synthetic-once-secret'
  );
  await page.getByRole('button', { name: '我已保存，隐藏令牌', exact: true }).click();
  await expect(page.getByText('synthetic-once-secret', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: '撤销', exact: true }).click();
  await expect(page.getByText('已撤销', { exact: true })).toBeVisible();
});
