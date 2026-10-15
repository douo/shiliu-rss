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
async function mock(
  page: Page,
  { expires = false, conflict = false, longMetadata = false, manySources = false } = {}
) {
  await page.route('**/favicon.ico', (route) => route.abort());
  const source = longMetadata
    ? { ...feed, title: '合成超长订阅源名称'.repeat(8) + 'UnbrokenSource'.repeat(12) }
    : feed;
  const items = Array.from({ length: 30 }, (_, i) => ({
    ...article(String(i + 1)),
    feed_title: source.title
  }));
  if (longMetadata) {
    items[0].title = '合成文章长标题'.repeat(12) + 'UnbrokenTitle'.repeat(12);
    items[0].author = 'SyntheticAuthor'.repeat(24);
  }
  const sources = manySources
    ? Array.from({ length: 200 }, (_, i) => ({
        ...source,
        id: String(i + 1),
        title: `合成订阅 ${String(i + 1).padStart(3, '0')}`,
        category_id: String((i % 20) + 1)
      }))
    : [source];
  const groups = manySources
    ? Array.from({ length: 20 }, (_, i) => ({
        id: String(i + 1),
        title: `合成分类 ${i + 1}`,
        version: 1
      }))
    : [];
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname;
    let result: unknown;
    if (path.endsWith('/session')) result = { authenticated: true, csrf_token: 'synthetic-csrf' };
    else if (path.endsWith('/feeds')) result = sources;
    else if (path.endsWith('/categories')) result = groups;
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
    await page.keyboard.press('ArrowDown');
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

test('long source titles and reader metadata fit the viewport without hiding navigation', async ({
  page
}, info) => {
  await mock(page, { longMetadata: true });
  await page.goto('/');
  if (info.project.name === 'mobile')
    await page.getByRole('button', { name: '打开导航', exact: true }).click();
  await page
    .getByRole('navigation', { name: '订阅源', exact: true })
    .getByRole('button')
    .first()
    .click();
  await page.locator('.article-row').first().waitFor();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(
    await page.locator('.article-list').evaluate((el) => el.scrollWidth <= el.clientWidth + 1)
  ).toBe(true);
  const count = await page.locator('.article-count').boundingBox();
  expect(count!.x + count!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  if (info.project.name === 'mobile')
    await expect(page.getByRole('button', { name: '打开导航', exact: true })).toBeVisible();
  await page.locator('.article-open').first().click();
  await expect(page.getByRole('article', { name: '文章阅读' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.locator('.reader').evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(
    true
  );
  await page.getByRole('button', { name: '返回列表 (Esc)', exact: true }).click();
  await expect(page.locator('.article-row').first()).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('plain arrows follow list focus and single keys operate the reader', async ({ page }) => {
  const items = await mock(page);
  await page.goto('/');
  await page.locator('[data-article-id="5"]').focus();
  await page.keyboard.press('ArrowDown');
  await expect(page.locator('[data-article-id="6"]')).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(page.locator('[data-article-id="5"]')).toBeFocused();
  await page.locator('[data-article-id="1"]').focus();
  await page.keyboard.press('ArrowUp');
  await expect(page.locator('[data-article-id="1"]')).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: '合成文章 1', exact: true })).toBeVisible();
  await expect.poll(() => items[0].read_at).toBeTruthy();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByRole('heading', { name: '合成文章 2', exact: true })).toBeVisible();
  await page.keyboard.press('ArrowUp');
  await expect(page.getByRole('heading', { name: '合成文章 1', exact: true })).toBeVisible();
  await page.keyboard.press('a');
  await expect.poll(() => items[0].read_at).toBeNull();
  await expect(page.getByRole('button', { name: '标为已读 (a)', exact: true })).toBeVisible();
  await page.keyboard.press('s');
  await expect.poll(() => items[0].favorite).toBe(true);
  await expect(page.getByRole('button', { name: '取消收藏 (s)', exact: true })).toBeVisible();
  await page
    .context()
    .route('https://example.test/article', (route) => route.fulfill({ body: '原文' }));
  const popupPromise = page.waitForEvent('popup');
  await page.keyboard.press('e');
  const popup = await popupPromise;
  await expect(popup).toHaveURL('https://example.test/article');
  await popup.close();
  await page.bringToFront();
  await page.keyboard.press('s');
  await expect(page.getByRole('dialog', { name: '取消收藏前' })).toBeVisible();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('a');
  await page.keyboard.press('q');
  expect(items[0].read_at).toBeNull();
  expect(items[0].favorite).toBe(true);
  await expect(page.getByRole('heading', { name: '合成文章 1', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-article-id="1"]')).toBeFocused();
  await page.keyboard.press('q');
  await expect(page.getByRole('textbox', { name: '搜索文章', exact: true })).toBeFocused();
  await expect(page.locator('.search-box kbd')).toHaveText('q');
});

test('Vimium default keys, modifiers, and composition stay available; held action keys do not toggle', async ({
  page
}) => {
  const items = await mock(page);
  await page.goto('/?article=1');
  await expect(page.getByRole('heading', { name: '合成文章 1', exact: true })).toBeVisible();
  const untouched = await page.evaluate(() => {
    const keys = [
      'j',
      'k',
      'h',
      'l',
      'g',
      'G',
      'z',
      'd',
      'u',
      'r',
      'R',
      'y',
      'p',
      'P',
      'i',
      'v',
      'V',
      'f',
      'F',
      '/',
      'n',
      'N',
      '*',
      '#',
      'o',
      'O',
      'T',
      'b',
      'B',
      'H',
      'L',
      'K',
      'J',
      '^',
      '<',
      '>',
      'W',
      't',
      'x',
      'X',
      'm',
      '`',
      '?',
      'Enter',
      'Tab'
    ];
    return keys.filter((key) => {
      const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
      document.activeElement!.dispatchEvent(event);
      return event.defaultPrevented;
    });
  });
  expect(untouched).toEqual([]);
  const handled = await page.evaluate(() => {
    const results: Record<string, boolean> = {};
    for (const [name, key, extra] of [
      ['composing', 'a', { isComposing: true }],
      ['alt', 's', { altKey: true }],
      ['control', 's', { ctrlKey: true }],
      ['meta', 's', { metaKey: true }],
      ['shift', 's', { shiftKey: true }],
      ['shiftArrow', 'ArrowDown', { shiftKey: true }],
      ['oldShortcut', 'ArrowDown', { altKey: true, shiftKey: true }],
      ['otherCharacter', 'ß', { code: 'KeyS' }],
      ['repeatFavorite', 's', { repeat: true }],
      ['repeatSidebar', 'c', { repeat: true }],
      ['repeatRead', 'a', { repeat: true }],
      ['repeatOriginal', 'e', { repeat: true }]
    ] as const) {
      const event = new KeyboardEvent('keydown', {
        key,
        bubbles: true,
        cancelable: true,
        ...extra
      });
      document.activeElement!.dispatchEvent(event);
      results[name] = event.defaultPrevented;
    }
    return results;
  });
  expect(handled).toEqual({
    composing: false,
    alt: false,
    control: false,
    meta: false,
    shift: false,
    shiftArrow: false,
    oldShortcut: false,
    otherCharacter: false,
    repeatFavorite: true,
    repeatSidebar: true,
    repeatRead: true,
    repeatOriginal: true
  });
  expect(items[0].favorite).toBe(false);
  expect(items[0].read_at).toBeTruthy();
  await expect(page.getByRole('heading', { name: '合成文章 1', exact: true })).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('typing and native form navigation never invoke reading shortcuts', async ({ page }) => {
  const items = await mock(page);
  await page.goto('/?article=1');
  const note = page.getByRole('textbox', { name: '我的笔记', exact: true });
  await note.fill('第一行\n第二行');
  await page.keyboard.press('ArrowUp');
  await page.keyboard.type('c a s e q');
  await page.keyboard.press('s');
  await page.keyboard.press('Escape');
  await expect(note).toBeFocused();
  expect(items[0].favorite).toBe(false);
  expect(items[0].read_at).toBeTruthy();
  await expect(page.getByRole('heading', { name: '合成文章 1', exact: true })).toBeVisible();
  await page.evaluate(() => {
    const editable = document.createElement('div');
    editable.contentEditable = 'true';
    editable.setAttribute('aria-label', '合成可编辑区域');
    editable.textContent = '正文';
    document.querySelector('.notes')!.append(editable);
    editable.focus();
  });
  await page.keyboard.type('c a s e q');
  await page.keyboard.press('ArrowDown');
  await expect(page.getByLabel('合成可编辑区域')).toBeFocused();
  expect(items[0].favorite).toBe(false);
  await page.getByRole('heading', { name: '合成文章 1', exact: true }).focus();
  await page.keyboard.press('q');
  const search = page.getByRole('textbox', { name: '搜索文章', exact: true });
  await page.keyboard.type('c a s e q');
  await page.keyboard.press('ArrowDown');
  await expect(search).toBeFocused();
  await expect(search).toHaveValue('c a s e q');
  await expect(page.getByRole('article', { name: '文章阅读' })).toHaveCount(0);
});

test('settings keep arrow keys and form controls; shortcut help matches the new bindings', async ({
  page
}, info) => {
  await mock(page);
  await page.goto('/?settings=true');
  await expect(page.getByRole('heading', { name: '设置', exact: true })).toBeVisible();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('a');
  await page.keyboard.press('s');
  await page.keyboard.press('e');
  await page.keyboard.press('q');
  await expect(page.getByRole('heading', { name: '设置', exact: true })).toBeVisible();
  await expect(page.getByRole('article', { name: '文章阅读' })).toHaveCount(0);
  const categoryFilter = page.getByRole('combobox', { name: '按分类筛选订阅', exact: true });
  await categoryFilter.focus();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('c');
  await expect(categoryFilter).toBeFocused();
  await expect(page.getByRole('heading', { name: '设置', exact: true })).toBeVisible();
  await categoryFilter.blur();
  await page.keyboard.press('c');
  if (info.project.name === 'desktop') {
    await expect(page.locator('.app-shell')).toHaveClass(/sidebar-collapsed/);
    await page.keyboard.press('c');
  } else await expect(page.locator('.sidebar')).toHaveClass(/open/);
  await page.getByRole('button', { name: '快捷键', exact: true }).focus();
  await page.keyboard.press('Enter');
  const help = page.getByRole('dialog', { name: '键盘阅读' });
  await expect(help).toBeVisible();
  await expect(help.locator('kbd')).toHaveText([
    'c',
    '↓ / ↑',
    'Tab / Enter',
    'a',
    's',
    'e',
    'q',
    'Esc'
  ]);
  await page.keyboard.press('c');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('q');
  await expect(help).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(help).toHaveCount(0);
});

test('sidebar collapse expands reading space and persists; mobile navigation stays available', async ({
  page
}, info) => {
  const items = await mock(page);
  items[0].content[0].html =
    '<h2>给阅读留出空间</h2>' +
    '<p>这是用于界面验证的合成中文段落。订阅内容应当有清楚的层次、舒适的行宽和足够的留白。收起侧栏之后，正文获得更多空间；重新展开时，分类和来源仍然容易找到。</p>'.repeat(
      6
    );
  await page.goto('/?article=1');
  await expect(page.getByRole('article', { name: '文章阅读' })).toBeVisible();
  const before = await page.locator('.reader').boundingBox();
  await page.keyboard.press('c');
  if (info.project.name === 'desktop') {
    await expect(page.locator('.app-shell')).toHaveClass(/sidebar-collapsed/);
    const after = await page.locator('.reader').boundingBox();
    expect(after!.width - before!.width).toBeGreaterThan(200);
    const content = await page.locator('.reading-content').boundingBox();
    expect(content!.width).toBeGreaterThan(850);
    expect(after!.height).toBeGreaterThan(800);
    await page.screenshot({ path: 'test-results/redesign-desktop-reading.png' });
    await page.reload();
    await expect(page.locator('.app-shell')).toHaveClass(/sidebar-collapsed/);
    await page.getByRole('button', { name: '切换侧栏 (c)', exact: true }).click();
    await expect(page.getByRole('complementary', { name: '阅读侧栏' })).toBeVisible();
  } else {
    await expect(page.locator('.sidebar')).toHaveClass(/open/);
    await page.keyboard.press('Escape');
    await expect(page.locator('.sidebar')).not.toHaveClass(/open/);
    await expect(page.getByRole('article', { name: '文章阅读' })).toBeVisible();
    await page.screenshot({ path: 'test-results/redesign-mobile-reading.png' });
    await page.keyboard.press('c');
    await expect(page.locator('.sidebar')).toHaveClass(/open/);
    await page.keyboard.press('ArrowDown');
    await expect(page.getByRole('heading', { name: '合成文章 1', exact: true })).toBeVisible();
    await page.keyboard.press('q');
    await expect(page.locator('.sidebar')).not.toHaveClass(/open/);
    await expect(page.getByRole('textbox', { name: '搜索文章', exact: true })).toBeFocused();
  }
});

test('one scroll region keeps sources discoverable with 200 feeds and 20 categories at short heights', async ({
  page
}, info) => {
  await page.setViewportSize({ width: info.project.name === 'desktop' ? 1280 : 390, height: 480 });
  await mock(page, { manySources: true });
  await page.goto('/');
  if (info.project.name === 'mobile')
    await page.getByRole('button', { name: '打开导航', exact: true }).click();
  await expect(page.getByRole('button', { name: '分类 20', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false'
  );
  const visible = await page.locator('.source-nav button').evaluateAll(
    (nodes) =>
      nodes.filter((node) => {
        const box = node.getBoundingClientRect(),
          parent = node.closest('.sidebar-library')!.getBoundingClientRect();
        return box.top >= parent.top && box.bottom <= parent.bottom;
      }).length
  );
  expect(visible).toBeGreaterThanOrEqual(3);
  await page.screenshot({ path: `test-results/redesign-${info.project.name}-short-sidebar.png` });
  if (info.project.name === 'desktop') {
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.screenshot({ path: 'test-results/redesign-desktop-sidebar.png' });
    await page.setViewportSize({ width: 1280, height: 480 });
  }
  const footer = await page.locator('.sidebar-bottom').boundingBox();
  expect(footer!.y + footer!.height).toBeLessThanOrEqual(480);
  await page.getByLabel('筛选订阅', { exact: true }).fill('200');
  await expect(
    page.getByRole('navigation', { name: '订阅源', exact: true }).getByRole('button')
  ).toHaveCount(1);
  await page.getByRole('button', { name: '合成订阅 200', exact: true }).click();
  await expect(page).toHaveURL(/scope=all&feed_id=200/);
  await expect(page.getByRole('heading', { name: '合成订阅 200', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('source links are separate from reading buttons and favicon failures fall back safely', async ({
  page
}) => {
  await mock(page);
  // Playwright aborts all exact /favicon.ico requests before page.route handlers.
  // Fulfill them through a separate CDP session to exercise the actual image path.
  const session = await page.context().newCDPSession(page);
  await session.send('Fetch.enable', {
    patterns: [{ urlPattern: 'https://example.test/favicon.ico', requestStage: 'Request' }]
  });
  let iconRequests = 0,
    failIcon = false;
  session.on('Fetch.requestPaused', async (event) => {
    iconRequests++;
    expect(Object.keys(event.request.headers).some((key) => key.toLowerCase() === 'referer')).toBe(
      false
    );
    await session.send('Fetch.fulfillRequest', {
      requestId: event.requestId,
      responseCode: failIcon ? 404 : 200,
      responseHeaders: [{ name: 'Content-Type', value: 'image/svg+xml' }],
      body: Buffer.from(
        failIcon
          ? ''
          : '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><rect width="16" height="16" fill="green"/></svg>'
      ).toString('base64')
    });
  });
  await page.goto('/');
  const link = page
    .locator('.article-row')
    .first()
    .getByRole('link', { name: feed.title, exact: true });
  await expect(link).toHaveAttribute('href', '/?scope=all&feed_id=1');
  await expect(link.locator('img')).toBeVisible();
  await expect
    .poll(() => link.locator('img').evaluate((img: HTMLImageElement) => img.naturalWidth))
    .toBeGreaterThan(0);
  expect(iconRequests).toBeGreaterThan(0);
  expect(await link.evaluate((el) => el.closest('button') === null)).toBe(true);
  const modified = await link.evaluate((el) => {
    let prevented: boolean | undefined;
    const observe = (e: MouseEvent) => {
      prevented = e.defaultPrevented;
      e.preventDefault();
    };
    document.addEventListener('click', observe, { once: true });
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, ctrlKey: true }));
    return prevented;
  });
  expect(modified).toBe(false);
  await link.click();
  await expect(page).toHaveURL('/?scope=all&feed_id=1');
  await expect(page.getByRole('article', { name: '文章阅读' })).toHaveCount(0);
  await page.locator('.article-open').first().click();
  await expect(page.getByRole('article', { name: '文章阅读' })).toBeVisible();
  await expect(page.locator('.reading-content .feed-link')).toHaveAttribute(
    'href',
    '/?scope=all&feed_id=1'
  );
  failIcon = true;
  await page.reload();
  const icon = page.locator('.reading-content .feed-icon');
  await expect(icon.locator('svg')).toBeVisible();
  await expect(icon.locator('img')).toHaveCount(0);
});
