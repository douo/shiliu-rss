import { test, expect } from '@playwright/test';
import { createServer, type Server } from 'node:http';
let server: Server, origin: string;
test.beforeAll(async () => {
  server = createServer((req, res) => {
    if (req.url === '/feed') {
      res.setHeader('Content-Type', 'application/rss+xml');
      res.end(
        `<?xml version="1.0"?><rss version="2.0"><channel><title>本机合成订阅</title><link>${origin}</link><description>测试</description><item><guid>synthetic-${origin}</guid><title>本机阅读闭环 ${new URL(origin).port}</title><link>${origin}/article</link><description><![CDATA[<p>这是一篇合成 RSS 正文，全部内容仅用于本机验证。</p>]]></description></item></channel></rss>`
      );
    } else {
      res.setHeader('Content-Type', 'text/html');
      res.end(
        `<html lang="zh"><head><title>本机阅读闭环 ${new URL(origin).port}</title></head><body><article><h1>本机阅读闭环 ${new URL(origin).port}</h1>${'<p>这是完全合成的长篇中文文章，用来检验显式全文获取。阅读器只在按钮点击或授权接口请求时下载这个页面，普通 RSS 轮询不会发起全文请求。后台任务和正文生命周期遵守共同的服务端规则。</p>'.repeat(15)}</article></body></html>`
      );
    }
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
});
test.afterAll(async () => {
  if (server) await new Promise<void>((resolve) => server.close(() => resolve()));
});
test('real API: subscription → background ingestion → reading → full text → notes', async ({
  page
}, info) => {
  test.skip(
    !process.env.SHILIU_E2E_PASSWORD,
    'Set SHILIU_E2E_PASSWORD and start an isolated local API.'
  );
  const cspErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error' && /Content Security Policy|script-src/.test(message.text()))
      cspErrors.push(message.text());
  });
  const startup = await page.goto('/');
  if (process.env.SHILIU_E2E_URL) {
    expect(startup?.headers()['content-security-policy']).toContain("script-src 'self'");
    expect(startup?.headers()['content-encoding']).toBe('gzip');
  }
  await page.getByLabel('登录密码').fill(process.env.SHILIU_E2E_PASSWORD!);
  await page.getByRole('button', { name: '进入拾流' }).click();
  await expect(page.getByRole('heading', { name: '近期文章', exact: true })).toBeVisible();
  if (info.project.name === 'mobile') await page.getByRole('button', { name: '打开导航' }).click();
  await page.getByRole('button', { name: '设置与授权', exact: true }).click();
  await page.getByRole('button', { name: '添加订阅', exact: true }).click();
  await page.getByLabel('订阅地址', { exact: true }).fill(`${origin}/feed`);
  await page.getByLabel('允许访问的内网主机（可选）').fill('127.0.0.1');
  await page.getByLabel('名称', { exact: true }).fill(`本机验证 ${info.project.name}`);
  await page.getByRole('button', { name: '保存订阅', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.getByRole('button', { name: '关闭设置' }).click();
  if (info.project.name === 'mobile') await page.getByRole('button', { name: '打开导航' }).click();
  await page.getByRole('button', { name: /近期文章/ }).click();
  for (let i = 0; i < 20; i++) {
    if (
      await page
        .getByRole('heading', { name: `本机阅读闭环 ${new URL(origin).port}`, exact: true })
        .count()
    )
      break;
    await page.getByRole('button', { name: '更新列表', exact: true }).click();
    await page.waitForTimeout(300);
  }
  await page
    .getByRole('heading', { name: `本机阅读闭环 ${new URL(origin).port}`, exact: true })
    .first()
    .click();
  await expect(page.getByText('这是一篇合成 RSS 正文，全部内容仅用于本机验证。')).toBeVisible();
  await page.getByRole('button', { name: '收藏文章 (Alt+Shift+S)', exact: true }).click();
  await expect(
    page.getByRole('button', { name: '取消收藏 (Alt+Shift+S)', exact: true })
  ).toBeVisible();
  await page.getByRole('textbox', { name: '我的笔记', exact: true }).fill('本机验证笔记');
  await page.getByLabel('标签', { exact: true }).fill('合成, 验证');
  await page.getByRole('button', { name: '保存笔记', exact: true }).click();
  await page.reload();
  await expect(page.getByRole('textbox', { name: '我的笔记', exact: true })).toHaveValue(
    '本机验证笔记'
  );
  await page.getByRole('button', { name: '获取全文', exact: true }).click();
  await expect(page.getByText('已获取全文', { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.locator('.article-body')).toContainText('这是完全合成的长篇中文文章');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true
  );
  expect(cspErrors).toEqual([]);
  await page.screenshot({
    path: `test-results/${info.project.name}-real-service.png`,
    fullPage: true
  });
});
