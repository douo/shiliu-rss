import { test, expect, type Page } from '@playwright/test';
import type { Category, Feed } from '../src/lib/models';

async function mockCategories(page: Page, stale = false) {
  const now = new Date().toISOString();
  const categories: Category[] = [
    { id: 'c1', title: '合成科技', version: 1 },
    { id: 'c2', title: '合成生活', version: 1 }
  ];
  const source = (id: string, title: string, category_id: string): Feed => ({
    id,
    title,
    category_id,
    url: `https://example.invalid/${id}/feed`,
    site_url: '',
    allow_host: '',
    paused: true,
    importance: 'normal',
    archive: false,
    interval_hours: 5,
    read_days: 7,
    unread_days: 30,
    unread_count: 0,
    next_check_at: now,
    last_attempt_at: null,
    last_success_at: null,
    error: '',
    version: 1
  });
  const feeds = [source('f1', '合成甲源', 'c1'), source('f2', '合成乙源', '')];
  const writes: { method: string; path: string; body: Record<string, unknown> | null }[] = [];
  let staleOnce = stale;
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname;
    const method = req.method();
    let result: unknown = {};
    if (!['GET', 'HEAD'].includes(method)) {
      expect(req.headers()['x-csrf-token']).toBe('synthetic-category-csrf');
      writes.push({ method, path, body: req.postData() ? req.postDataJSON() : null });
    }
    if (path.endsWith('/session'))
      result = { authenticated: true, csrf_token: 'synthetic-category-csrf' };
    else if (path.endsWith('/categories')) {
      if (method === 'POST') {
        if (categories.some((category) => category.title === req.postDataJSON().title)) {
          await route.fulfill({ status: 409, json: { title: '分类名重复' } });
          return;
        }
        const created = {
          id: `c${categories.length + 1}`,
          title: req.postDataJSON().title,
          version: 1
        };
        categories.push(created);
        result = created;
      } else result = categories;
    } else if (/\/categories\/[^/]+$/.test(path)) {
      const category = categories.find((c) => c.id === path.split('/').at(-1))!;
      const version =
        method === 'DELETE' ? Number(url.searchParams.get('version')) : req.postDataJSON().version;
      if (staleOnce) {
        staleOnce = false;
        category.title = '其他操作的新名称';
        category.version++;
      }
      if (version !== category.version) {
        await route.fulfill({ status: 412, json: { title: '版本冲突' } });
        return;
      }
      if (method === 'DELETE') {
        categories.splice(categories.indexOf(category), 1);
        for (const feed of feeds.filter((f) => f.category_id === category.id)) {
          feed.category_id = '';
          feed.version++;
        }
        await route.fulfill({ status: 204 });
        return;
      }
      category.title = req.postDataJSON().title;
      category.version++;
      result = category;
    } else if (path.endsWith('/feeds')) {
      if (method === 'POST') {
        const feed = { ...source('f3', '', ''), ...req.postDataJSON(), id: 'f3', version: 1 };
        feeds.push(feed);
        result = feed;
      } else result = feeds;
    } else if (/\/feeds\/[^/]+$/.test(path)) {
      expect(method).toBe('PATCH');
      const feed = feeds.find((f) => f.id === path.split('/').at(-1))!;
      expect(req.postDataJSON().version).toBe(feed.version);
      Object.assign(feed, req.postDataJSON(), { version: feed.version + 1 });
      result = feed;
    } else if (path.endsWith('/articles'))
      result = { items: [], next_cursor: '', high_watermark: '0' };
    await route.fulfill({ json: result });
  });
  return { categories, feeds, writes };
}

async function openCategories(page: Page, mobile: boolean) {
  await page.goto('/?scope=recent');
  if (mobile) await page.getByRole('button', { name: '打开导航', exact: true }).click();
  if (!mobile) {
    await page.getByRole('button', { name: '设置与授权', exact: true }).click();
    await expect(page.getByRole('heading', { name: '设置', exact: true })).toBeVisible();
  }
  await page.getByRole('button', { name: '管理分类', exact: true }).click();
  await expect(page.getByRole('region', { name: '分类管理' })).toBeVisible();
}

test('category CRUD, search and membership preserve subscriptions', async ({ page }, info) => {
  const state = await mockCategories(page);
  await openCategories(page, info.project.name === 'mobile');
  const panel = page.getByRole('region', { name: '分类管理' });
  const nav = panel.getByRole('navigation', { name: '管理分类列表' });
  await page.getByRole('button', { name: '新建分类', exact: true }).click();
  await page.getByLabel('分类名称', { exact: true }).fill('合成科技');
  await page.getByRole('button', { name: '保存分类', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByRole('dialog').getByRole('alert')).toContainText('分类名称重复');
  await expect(page.getByLabel('分类名称', { exact: true })).toHaveValue('合成科技');
  await page.getByLabel('分类名称', { exact: true }).fill('字'.repeat(101));
  await expect(page.getByRole('button', { name: '保存分类', exact: true })).toBeDisabled();
  await page.getByLabel('分类名称', { exact: true }).fill('合成阅读');
  await page.getByRole('button', { name: '保存分类', exact: true }).click();
  await expect(panel.getByRole('heading', { name: '合成阅读', exact: true })).toBeVisible();
  await panel.getByRole('button', { name: '重命名分类', exact: true }).click();
  await page.getByLabel('分类名称', { exact: true }).fill('合成研究');
  await page.getByRole('button', { name: '保存分类', exact: true }).click();
  await page.getByLabel('搜索分类', { exact: true }).fill('研究');
  await expect(nav.getByRole('button', { name: /合成科技/ })).toHaveCount(0);
  await expect(nav.getByRole('button', { name: /合成研究/ })).toBeVisible();
  await page.getByLabel('搜索分类', { exact: true }).fill('');
  await nav.getByRole('button', { name: /合成科技/ }).click();
  await expect(panel.getByText('合成甲源', { exact: true })).toBeVisible();
  await page.getByLabel('选择已有订阅', { exact: true }).selectOption('f2');
  await page.getByRole('button', { name: '加入分类', exact: true }).click();
  await expect.poll(() => state.feeds[1].category_id).toBe('c1');
  await expect(panel.locator('[data-feed-id="f2"]')).toBeVisible();
  await page.screenshot({
    path: `test-results/${info.project.name}-categories.png`,
    fullPage: true
  });
  await page.getByLabel('搜索分类内订阅', { exact: true }).fill('乙源');
  await expect(panel.locator('[data-feed-id="f1"]')).toHaveCount(0);
  await expect(panel.locator('[data-feed-id="f2"]')).toBeVisible();
  await page.getByRole('button', { name: '将 合成乙源 移至未分类', exact: true }).click();
  await expect.poll(() => state.feeds[1].category_id).toBe('');
  await page.getByLabel('搜索分类内订阅', { exact: true }).fill('');
  await page.getByRole('button', { name: '删除分类', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('订阅移至未分类，文章保留');
  await page.getByRole('button', { name: '确认删除分类', exact: true }).click();
  await expect.poll(() => state.categories.some((c) => c.id === 'c1')).toBe(false);
  expect(state.feeds).toHaveLength(2);
  expect(state.feeds[0].category_id).toBe('');
  expect(state.writes.filter((w) => w.method === 'DELETE').map((w) => w.path)).toEqual([
    '/api/v1/categories/c1'
  ]);
  await expect(panel.getByText('合成甲源', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('stale category version refreshes before explicit retry', async ({ page }, info) => {
  const state = await mockCategories(page, true);
  await openCategories(page, info.project.name === 'mobile');
  const panel = page.getByRole('region', { name: '分类管理' });
  await panel
    .getByRole('navigation')
    .getByRole('button', { name: /合成科技/ })
    .click();
  await page.getByRole('button', { name: '重命名分类', exact: true }).click();
  await page.getByLabel('分类名称', { exact: true }).fill('过期修改');
  await page.getByRole('button', { name: '保存分类', exact: true }).click();
  await expect(panel.getByRole('alert')).toContainText('请检查当前内容后重试');
  await expect(panel.getByRole('heading', { name: '其他操作的新名称', exact: true })).toBeVisible();
  expect(state.categories[0].title).toBe('其他操作的新名称');
  expect(state.writes.filter((w) => w.method === 'PATCH')).toHaveLength(1);
  await page.getByRole('button', { name: '重命名分类', exact: true }).click();
  await expect(page.getByLabel('分类名称', { exact: true })).toHaveValue('其他操作的新名称');
  await page.getByLabel('分类名称', { exact: true }).fill('确认后的修改');
  await page.getByRole('button', { name: '保存分类', exact: true }).click();
  await expect.poll(() => state.categories[0].title).toBe('确认后的修改');
  expect(state.writes.filter((w) => w.method === 'PATCH').map((w) => w.body?.version)).toEqual([
    1, 2
  ]);
});

test('category member creates and edits feeds with preselected category', async ({
  page
}, info) => {
  const state = await mockCategories(page);
  await openCategories(page, info.project.name === 'mobile');
  const panel = page.getByRole('region', { name: '分类管理' });
  await panel
    .getByRole('navigation')
    .getByRole('button', { name: /合成科技/ })
    .click();
  await page.getByRole('button', { name: '新增订阅', exact: true }).click();
  await expect(page.getByLabel('分类', { exact: true })).toHaveValue('c1');
  await page.getByLabel('订阅地址', { exact: true }).fill('https://example.invalid/new/feed');
  await page.getByLabel('名称', { exact: true }).fill('合成新源');
  await page.getByRole('button', { name: '保存订阅', exact: true }).click();
  await expect(panel.getByText('合成新源', { exact: true })).toBeVisible();
  await panel.getByRole('button', { name: '编辑 合成新源', exact: true }).click();
  await page.getByLabel('名称', { exact: true }).fill('合成新源改名');
  await page.getByRole('button', { name: '保存订阅', exact: true }).click();
  await expect.poll(() => state.feeds[2].title).toBe('合成新源改名');
  await page.getByRole('button', { name: '订阅与保存', exact: true }).click();
  await page.getByLabel('搜索订阅', { exact: true }).fill('新源改名');
  await page.getByLabel('按分类筛选订阅', { exact: true }).selectOption('c1');
  await expect(page.locator('.feed-setting')).toHaveCount(1);
  await page.getByRole('button', { name: '暂停 合成新源改名', exact: true }).click();
  await expect.poll(() => state.feeds[2].paused).toBe(true);
});
