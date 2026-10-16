import { test, expect, type Page } from '@playwright/test';
import type { Article, Feed } from '../src/lib/models';

async function contextData(page: Page, { unsafe = false, failBulk = false } = {}) {
  const now = '2026-10-16T00:00:00Z';
  const feeds: Feed[] = ['目标', '其他'].map((name, i) => ({
    id: String(i + 1),
    title: `合成${name}源`,
    category_id: i === 0 ? 'tech' : '',
    url: unsafe && i === 0 ? 'javascript:alert(1)' : `https://example.invalid/${i}/feed`,
    site_url: unsafe ? 'data:text/html,synthetic' : 'https://example.invalid/',
    allow_host: '',
    paused: false,
    archive: false,
    importance: 'high',
    interval_hours: 5,
    read_days: 7,
    unread_days: 30,
    unread_count: 0,
    last_attempt_at: now,
    last_success_at: null,
    next_check_at: now,
    error: i === 0 ? 'http_404' : '',
    version: 1
  }));
  const categories = [
    { id: 'tech', title: '合成技术', version: 1 },
    { id: 'empty', title: '合成空分类', version: 1 }
  ];
  const items: Article[] = Array.from({ length: 62 }, (_, i) => ({
    id: String(i + 1),
    feed_id: i < 60 ? '1' : '2',
    feed_title: feeds[i < 60 ? 0 : 1].title,
    title: `合成文章 ${i + 1}`,
    url: 'https://example.invalid/article',
    author: '',
    first_seen_at: now,
    published_at: now,
    read_at: i < 55 ? now : null,
    favorite: false,
    hidden: false,
    removed: false,
    note: '',
    tags: [],
    version: 1,
    body_state: 'available',
    content_hash: 'synthetic',
    provenance: 'feed',
    read_at_unknown: false,
    imported_at: null,
    protection_until: null,
    content_expires_at: null,
    content: [
      {
        kind: 'feed',
        html: '<p>合成正文</p>',
        text: '合成正文',
        hash: 'synthetic',
        state: 'available',
        obtained_at: now,
        expires_at: null
      }
    ],
    artifacts: []
  }));
  let hold = false,
    release: (() => void) | undefined,
    waiting = false;
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname;
    let result: unknown;
    if (path.endsWith('/session')) result = { authenticated: true, csrf_token: 'synthetic' };
    else if (path.endsWith('/feeds')) {
      for (const f of feeds)
        f.unread_count = items.filter((a) => a.feed_id === f.id && !a.read_at && !a.removed).length;
      result = feeds;
    } else if (path.endsWith('/categories')) result = categories;
    else if (/\/feeds\/[^/]+$/.test(path)) {
      const feed = feeds.find((f) => f.id === path.split('/').at(-1))!;
      expect(req.method()).toBe('PATCH');
      expect(req.postDataJSON().version).toBe(feed.version);
      Object.assign(feed, req.postDataJSON(), { version: feed.version + 1 });
      result = feed;
    } else if (/\/categories\/[^/]+$/.test(path)) {
      const category = categories.find((c) => c.id === path.split('/').at(-1))!;
      expect(req.method()).toBe('PATCH');
      expect(req.postDataJSON().version).toBe(category.version);
      Object.assign(category, req.postDataJSON(), { version: category.version + 1 });
      result = category;
    } else if (path.endsWith('/articles')) {
      let matching = items.filter(
        (a) =>
          (!url.searchParams.has('feed_id') || a.feed_id === url.searchParams.get('feed_id')) &&
          (!url.searchParams.has('category_id') ||
            feeds.find((f) => f.id === a.feed_id)?.category_id ===
              url.searchParams.get('category_id')) &&
          (!url.searchParams.has('favorite') ||
            a.favorite === (url.searchParams.get('favorite') === 'true')) &&
          (!url.searchParams.has('q') || a.title.includes(url.searchParams.get('q')!))
      );
      const unread_count = matching.filter((a) => !a.read_at && !a.removed).length;
      const read = url.searchParams.get('read');
      if (read === 'true') matching = matching.filter((a) => a.read_at);
      if (read === 'false') matching = matching.filter((a) => !a.read_at && !a.removed);
      const offset = Number(url.searchParams.get('cursor') || 0),
        limit = Number(url.searchParams.get('limit') || 50);
      result = {
        items: matching.slice(offset, offset + limit),
        unread_count,
        high_watermark: '62',
        next_cursor: matching.length > offset + limit ? String(offset + limit) : ''
      };
      if (hold && limit === 1) {
        hold = false;
        waiting = true;
        await new Promise<void>((resolve) => {
          release = resolve;
        });
        waiting = false;
      }
    } else if (/\/articles\/\d+$/.test(path)) {
      const a = items.find((a) => a.id === path.split('/').at(-1))!;
      if (req.method() === 'PATCH') {
        if (failBulk && a.id === '57') {
          await route.fulfill({ status: 500, json: { title: '合成操作失败' } });
          return;
        }
        const patch = req.postDataJSON();
        if (patch.read !== undefined) a.read_at = patch.read ? now : null;
        if (patch.favorite !== undefined) a.favorite = patch.favorite;
        a.version++;
      }
      result = a;
    } else if (path.endsWith('/tokens')) result = [];
    else throw new Error(`Unexpected request ${path}`);
    await route.fulfill({ json: result });
  });
  return {
    feeds,
    items,
    categories,
    holdCount: () => {
      hold = true;
    },
    isWaiting: () => waiting,
    releaseCount: () => release?.()
  };
}

const unread = (page: Page) => page.getByRole('button', { name: /^未读 / });
async function selectSource(page: Page, mobile: boolean, name: string) {
  if (mobile) await page.getByRole('button', { name: '打开导航', exact: true }).click();
  await page
    .getByRole('navigation', { name: '订阅源', exact: true })
    .getByRole('button', { name, exact: true })
    .click();
}

test('all/read/unread tabs keep the full count across pages and reading mutations', async ({
  page
}) => {
  await contextData(page);
  await page.goto('/?scope=all&feed_id=1');
  await expect(unread(page)).toHaveText('未读 5');
  await expect(page.locator('.article-count')).toHaveText('已加载 50+ 篇');
  await expect(page.getByRole('button', { name: '本页标为已读', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: '加载更多', exact: true }).click();
  await expect(page.locator('.article-count')).toHaveText('已加载 60 篇');
  await expect(unread(page)).toHaveText('未读 5');
  await page.getByRole('button', { name: '已读', exact: true }).click();
  await expect(unread(page)).toHaveText('未读 5');
  await unread(page).click();
  await expect(page.locator('.article-row')).toHaveCount(5);
  await page.locator('[data-article-id="56"]').click();
  await expect(unread(page)).toHaveCount(0);
  await page.getByRole('button', { name: '标为未读 (a)', exact: true }).click();
  await page.getByRole('button', { name: '返回列表 (Esc)', exact: true }).click();
  await expect(unread(page)).toHaveText('未读 5');
  await page.getByRole('button', { name: '本页标为已读', exact: true }).click();
  await expect(unread(page)).toHaveText('未读 0');
  await page.getByRole('button', { name: '全部', exact: true }).click();
  await expect(unread(page)).toHaveText('未读 0');
});

test('feed details open the exact editor and return to the original filtered list', async ({
  page
}, info) => {
  await contextData(page);
  await page.goto('/?scope=all&feed_id=1&read=true');
  const entry = page.getByRole('button', { name: '订阅源详情', exact: true });
  await entry.click();
  const detail = page.getByRole('dialog', { name: '订阅源详情', exact: true });
  await expect(detail).toContainText('合成目标源');
  await expect(detail).toContainText('5 篇（含归档）');
  await expect(detail).toContainText('http_404');
  await expect(detail).toContainText('尚无记录');
  await page.screenshot({ path: `test-results/${info.project.name}-list-feed-details.png` });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.keyboard.press('Escape');
  await expect(detail).not.toBeVisible();
  await expect(entry).toBeFocused();
  await entry.click();
  await detail.getByRole('button', { name: '编辑订阅', exact: true }).click();
  const editor = page.getByRole('dialog', { name: '编辑订阅', exact: true });
  await expect(editor.getByLabel('名称', { exact: true })).toHaveValue('合成目标源');
  await expect(editor.getByLabel('分类', { exact: true })).toHaveValue('tech');
  await editor.getByLabel('名称', { exact: true }).fill('合成目标源已修改');
  await editor.getByRole('button', { name: '保存订阅', exact: true }).click();
  await expect(editor).not.toBeVisible();
  await page.getByRole('button', { name: '关闭设置', exact: true }).click();
  await expect(page).toHaveURL(/scope=all&feed_id=1&read=true/);
  await expect(page.getByRole('heading', { name: '合成目标源已修改', exact: true })).toBeVisible();
  await expect(unread(page)).toHaveText('未读 5');
});

test('category details support exact category/member editing and empty categories', async ({
  page
}, info) => {
  const data = await contextData(page);
  await page.goto('/?scope=all&category_id=tech');
  await page.getByRole('button', { name: '分类详情', exact: true }).click();
  let detail = page.getByRole('dialog', { name: '分类详情', exact: true });
  await expect(detail).toContainText('1 个订阅源 · 5 篇未读（含归档）');
  await detail.getByRole('button', { name: '编辑 合成目标源', exact: true }).click();
  await expect(
    page.getByRole('dialog', { name: '编辑订阅', exact: true }).getByLabel('名称', { exact: true })
  ).toHaveValue('合成目标源');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '关闭设置', exact: true }).click();
  await page.getByRole('button', { name: '分类详情', exact: true }).click();
  await detail.getByRole('button', { name: '编辑分类', exact: true }).click();
  const editor = page.getByRole('dialog', { name: '重命名分类', exact: true });
  await expect(editor.getByLabel('分类名称', { exact: true })).toHaveValue('合成技术');
  await editor.getByLabel('分类名称', { exact: true }).fill('合成技术已修改');
  await editor.getByRole('button', { name: '保存分类', exact: true }).click();
  await expect.poll(() => data.categories[0].title).toBe('合成技术已修改');
  await page.getByRole('button', { name: '关闭设置', exact: true }).click();
  await expect(page).toHaveURL(/category_id=tech/);
  await expect(page.getByRole('heading', { name: '合成技术已修改', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '分类详情', exact: true }).click();
  await page.screenshot({ path: `test-results/${info.project.name}-list-category-details.png` });
  await detail.getByRole('button', { name: '合成目标源 5', exact: true }).click();
  await expect(page).toHaveURL(/feed_id=1/);
  await expect(page).not.toHaveURL(/category_id=/);
  await page.goto('/?scope=all&category_id=empty');
  await expect(unread(page)).toHaveText('未读 0');
  await page.getByRole('button', { name: '分类详情', exact: true }).click();
  detail = page.getByRole('dialog', { name: '分类详情', exact: true });
  await expect(detail).toContainText('此分类暂无订阅源');
  await detail.getByRole('button', { name: '编辑分类', exact: true }).click();
  await expect(
    page.getByRole('dialog', { name: '重命名分类', exact: true }).getByLabel('分类名称')
  ).toHaveValue('合成空分类');
});

test('details keep unsafe links inert and long metadata within the viewport', async ({ page }) => {
  const data = await contextData(page, { unsafe: true });
  data.feeds[0].title = '合成长名称'.repeat(30) + 'LongName'.repeat(20);
  await page.goto('/?scope=all&feed_id=1');
  await page.getByRole('button', { name: '订阅源详情', exact: true }).click();
  const detail = page.getByRole('dialog', { name: '订阅源详情', exact: true });
  await expect(detail).toContainText('javascript:alert(1)');
  await expect(detail.getByRole('link')).toHaveCount(0);
  const bounds = await detail.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await detail.getByRole('button', { name: '编辑订阅', exact: true }).focus();
  await page.keyboard.press('Tab');
  await expect(detail.getByRole('button', { name: '关闭详情', exact: true })).toBeFocused();
});

test('partial bulk failures still refresh the full count', async ({ page }) => {
  await contextData(page, { failBulk: true });
  await page.goto('/?scope=all&feed_id=1&read=false');
  await expect(unread(page)).toHaveText('未读 5');
  await page.getByRole('button', { name: '本页标为已读', exact: true }).click();
  await expect(unread(page)).toHaveText('未读 4');
  await expect(page.getByRole('alert')).toContainText('合成操作失败');
});

test('a delayed count from the old context cannot replace the new source count', async ({
  page
}, info) => {
  const data = await contextData(page);
  await page.goto('/?scope=all&feed_id=1&read=false');
  await expect(unread(page)).toHaveText('未读 5');
  data.holdCount();
  await page.locator('[data-article-id="56"]').click();
  await expect.poll(data.isWaiting).toBe(true);
  await page.getByRole('button', { name: '返回列表 (Esc)', exact: true }).click();
  await selectSource(page, info.project.name === 'mobile', '合成其他源');
  await expect(unread(page)).toHaveText('未读 2');
  data.releaseCount();
  await expect.poll(data.isWaiting).toBe(false);
  await expect(unread(page)).toHaveText('未读 2');
});
