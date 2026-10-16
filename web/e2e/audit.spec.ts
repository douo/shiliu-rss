import { test, expect, type BrowserContext } from '@playwright/test';
async function auditedAPI(context: BrowserContext) {
  const now = new Date().toISOString();
  let authenticated = true;
  let article = {
    id: '1',
    feed_id: '1',
    feed_title: '审查合成源',
    title: '受保护的文章',
    url: 'javascript:alert(1)',
    author: '<img src=x onerror=alert(1)>',
    published_at: now,
    first_seen_at: now,
    read_at: now,
    favorite: true,
    hidden: false,
    removed: false,
    note: '保留的个人笔记',
    tags: [],
    version: 1,
    body_state: 'available',
    content_hash: 'synthetic',
    content_expires_at: null,
    protection_until: null,
    imported_at: null,
    provenance: 'feed',
    read_at_unknown: false,
    content: [
      {
        kind: 'feed',
        html: '<p>需要会话才能看到的正文</p>',
        text: '需要会话才能看到的正文',
        state: 'available',
        hash: 'synthetic',
        obtained_at: now,
        expires_at: null
      }
    ],
    artifacts: []
  };
  const feed = {
    id: '1',
    url: 'https://example.test/feed',
    title: '审查合成源',
    site_url: '',
    category_id: '',
    allow_host: '',
    paused: false,
    archive: true,
    interval_hours: 5,
    read_days: 7,
    unread_days: 30,
    last_attempt_at: now,
    last_success_at: now,
    next_check_at: now,
    error: '',
    version: 1
  };
  await context.route('**/api/v1/**', async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    let result: unknown;
    if (path.endsWith('/session')) {
      if (request.method() === 'DELETE') authenticated = false;
      result = { authenticated, csrf_token: authenticated ? 'synthetic' : '' };
    } else if (!authenticated) {
      await route.fulfill({ status: 401, json: { title: 'Unauthorized' } });
      return;
    } else if (path.endsWith('/feeds')) result = [feed];
    else if (path.endsWith('/categories')) result = [];
    else if (path.endsWith('/articles'))
      result = {
        items: [{ ...article, content: undefined }],
        next_cursor: '',
        high_watermark: '1'
      };
    else if (path.endsWith('/articles/1')) {
      if (request.method() === 'PATCH') article = { ...article, ...request.postDataJSON() };
      result = article;
    } else if (path.endsWith('/retention-preview'))
      result = { articles: 1, contents: 1, bytes: 200 };
    else result = [];
    await route.fulfill({ json: result });
  });
  return {
    expire: () => {
      authenticated = false;
    }
  };
}
test('audit: unsafe metadata links stay inert and favorite removal explains retention', async ({
  page,
  context
}) => {
  await auditedAPI(context);
  await page.goto('/?article=1');
  await expect(page.getByRole('heading', { name: '受保护的文章', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: '打开原文 (e)' })).toHaveCount(0);
  await expect(page.getByText('<img src=x onerror=alert(1)>', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '取消收藏 (s)', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('当前正文继续受源策略保护');
  await page.getByRole('button', { name: '保留收藏', exact: true }).last().click();
  await expect(page.getByRole('button', { name: '取消收藏 (s)', exact: true })).toBeVisible();
});
test('audit: back navigation revalidates session before restoring private content', async ({
  page,
  context
}) => {
  const api = await auditedAPI(context);
  await page.goto('/?article=1');
  await expect(page.getByText('需要会话才能看到的正文', { exact: true })).toBeVisible();
  await page.goto('about:blank');
  api.expire();
  await page.goBack();
  await expect(page.getByLabel('登录密码')).toBeVisible();
  await expect(page.getByText('需要会话才能看到的正文', { exact: true })).toHaveCount(0);
  await expect(page.getByText('保留的个人笔记', { exact: true })).toHaveCount(0);
});
test('audit: logout in another tab clears an open reader immediately', async ({
  page,
  context
}) => {
  await auditedAPI(context);
  await page.goto('/?article=1');
  await expect(page.getByText('需要会话才能看到的正文', { exact: true })).toBeVisible();
  const second = await context.newPage();
  await second.goto('/?scope=recent');
  if (test.info().project.name === 'mobile')
    await second.getByRole('button', { name: '打开导航' }).click();
  await second.getByRole('button', { name: '退出登录', exact: true }).click();
  await expect(page.getByLabel('登录密码')).toBeVisible();
  await expect(page.getByText('需要会话才能看到的正文', { exact: true })).toHaveCount(0);
});
test('audit: changing archive policy shows immediate visibility and body impact', async ({
  page,
  context
}) => {
  await auditedAPI(context);
  await page.goto('/?settings=true');
  await page.getByRole('main').getByRole('button', { name: '审查合成源', exact: true }).click();
  await page.getByLabel('长期存档已获取的正文').uncheck();
  await expect(page.getByRole('dialog')).toContainText('超过90天');
  await page.getByRole('button', { name: '预览保存影响', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('1 篇文章');
  await page.getByLabel('已读可见（天）').fill('10');
  await expect(page.getByText(/此策略下/)).toHaveCount(0);
});
