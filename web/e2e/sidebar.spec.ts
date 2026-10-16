import { test, expect, type Page } from '@playwright/test';
import type { Feed } from '../src/lib/models';

async function sidebarData(page: Page) {
  const now = '2026-10-16T00:00:00Z';
  const source = (
    id: string,
    title: string,
    category_id: string,
    error = '',
    paused = false
  ): Feed => ({
    id,
    title,
    category_id,
    error,
    paused,
    url: `https://example.invalid/${id}/feed`,
    site_url: '',
    allow_host: '',
    archive: false,
    importance: 'normal',
    interval_hours: 5,
    read_days: 7,
    unread_days: 30,
    unread_count: 0,
    last_attempt_at: now,
    last_success_at: null,
    next_check_at: now,
    version: 1
  });
  const feeds = [
    source('1', '异常技术源', 'tech', 'http_404'),
    source('2', '正常技术源', 'tech'),
    source('3', '暂停生活源', 'life', 'dns_failed', true),
    source('4', '未分类源', '')
  ];
  feeds[0].unread_count = 52;
  feeds[2].unread_count = 1;
  const categories = [
    { id: 'tech', title: '技术', version: 1 },
    { id: 'life', title: '生活', version: 1 },
    { id: 'empty', title: '空分类', version: 1 }
  ];
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    let result: unknown;
    if (path.endsWith('/session')) result = { authenticated: true, csrf_token: 'synthetic' };
    else if (path.endsWith('/feeds')) result = feeds;
    else if (path.endsWith('/categories')) result = categories;
    else if (path.endsWith('/articles'))
      result = { items: [], high_watermark: '0', next_cursor: '' };
    else throw new Error(`Unexpected request ${path}`);
    await route.fulfill({ json: result });
  });
  return feeds;
}
async function openNavigation(page: Page, mobile: boolean) {
  if (mobile) await page.getByRole('button', { name: '打开导航', exact: true }).click();
}

test('source health dot reveals the actual error without navigating or closing the sidebar', async ({
  page
}, info) => {
  const feeds = await sidebarData(page);
  await page.goto('/?scope=recent');
  const mobile = info.project.name === 'mobile';
  await openNavigation(page, mobile);
  const sources = page.getByRole('navigation', { name: '订阅源', exact: true });
  const status = sources.getByRole('button', { name: '查看异常技术源的异常', exact: true });
  const before = page.url();
  expect(await status.evaluate((el) => !el.parentElement?.closest('button'))).toBe(true);
  await expect(sources.getByRole('button', { name: '查看正常技术源的异常' })).toHaveCount(0);
  await expect(
    sources.getByRole('button', { name: '异常技术源', exact: true }).locator('.unread-count')
  ).toHaveText('52');
  await expect(
    sources.getByRole('button', { name: '正常技术源', exact: true }).locator('.unread-count')
  ).toHaveCount(0);
  if (mobile) await status.click();
  else await status.hover();
  const detail = page.getByRole('dialog', { name: '订阅源异常', exact: true });
  await expect(detail).toContainText('HTTP 404');
  await expect(detail).toContainText('http_404');
  await expect(detail).toContainText('最近尝试');
  await expect(detail).toContainText('尚无记录');
  expect(page.url()).toBe(before);
  const bounds = await detail.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  await page.screenshot({ path: `test-results/${info.project.name}-feed-error.png` });
  await page.keyboard.press('Escape');
  await expect(detail).not.toBeVisible();
  if (mobile) await expect(page.locator('.sidebar')).toHaveClass(/open/);
  await sources.getByRole('button', { name: '正常技术源', exact: true }).focus();
  await status.focus();
  await expect(detail).toBeVisible();
  await detail.getByRole('button', { name: '关闭异常说明', exact: true }).click();
  await sources.getByRole('button', { name: '查看暂停生活源的异常', exact: true }).click();
  await expect(detail).toContainText('dns_failed');
  await expect(detail).toContainText('域名解析失败');
  await expect(detail).toContainText('已暂停');
  await expect(detail).not.toContainText('预计下次检查');
  await page.keyboard.press('Escape');
  await sources.getByRole('button', { name: '异常技术源', exact: true }).click();
  await expect(page).toHaveURL(/feed_id=1/);
  feeds[0].error = '';
  await page.reload();
  await openNavigation(page, mobile);
  await expect(page.getByRole('button', { name: '查看异常技术源的异常', exact: true })).toHaveCount(
    0
  );
});

test('categories independently expand their feeds while name selection and section collapse keep working', async ({
  page
}, info) => {
  await sidebarData(page);
  await page.goto('/?scope=recent');
  const mobile = info.project.name === 'mobile';
  await openNavigation(page, mobile);
  const section = page.getByRole('button', { name: '分类 3', exact: true });
  await section.click();
  const tech = page.getByRole('navigation', { name: '技术下的订阅源', exact: true });
  const life = page.getByRole('navigation', { name: '生活下的订阅源', exact: true });
  const before = page.url();
  await page.getByRole('button', { name: '展开分类：技术', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(tech.getByRole('button', { name: '正常技术源', exact: true })).toBeVisible();
  await expect(
    tech.getByRole('button', { name: '异常技术源', exact: true }).locator('.unread-count')
  ).toHaveText('52');
  await expect(
    tech.getByRole('button', { name: '正常技术源', exact: true }).locator('.unread-count')
  ).toHaveCount(0);
  await expect(tech.getByRole('button', { name: '暂停生活源', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: '展开分类：生活', exact: true }).click();
  await expect(life.getByRole('button', { name: '暂停生活源', exact: true })).toBeVisible();
  expect(page.url()).toBe(before);
  await page.getByRole('button', { name: '收起分类：技术', exact: true }).click();
  await expect(tech).not.toBeVisible();
  await expect(life).toBeVisible();
  await page.getByRole('button', { name: '展开分类：空分类', exact: true }).click();
  await expect(page.getByText('此分类暂无订阅源', { exact: true })).toBeVisible();
  await section.click();
  await section.click();
  await expect(life).toBeVisible();
  await page.getByRole('button', { name: '订阅源 4', exact: true }).click();
  await expect(life).toBeVisible();
  await expect(page.getByRole('navigation', { name: '订阅源', exact: true })).not.toBeVisible();
  await page
    .getByRole('navigation', { name: '分类', exact: true })
    .getByRole('button', { name: '技术', exact: true })
    .click();
  await expect(page).toHaveURL(/category_id=tech/);
  await openNavigation(page, mobile);
  await page.getByRole('button', { name: '展开分类：技术', exact: true }).click();
  await tech.getByRole('button', { name: '查看异常技术源的异常', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '订阅源异常', exact: true })).toContainText(
    'http_404'
  );
  await page.keyboard.press('Escape');
  await page.screenshot({ path: `test-results/${info.project.name}-category-feeds.png` });
  await tech.getByRole('button', { name: '正常技术源', exact: true }).click();
  await expect(page).toHaveURL(/feed_id=2/);
  await expect(page).not.toHaveURL(/category_id=/);
  await openNavigation(page, mobile);
  await expect(tech.getByRole('button', { name: '正常技术源', exact: true })).toHaveAttribute(
    'aria-current',
    'page'
  );
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
