import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import pkg from '../web/node_modules/@playwright/test/index.js';
const { chromium } = pkg;

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const rootDir = path.resolve(__dirname, '..');
const buildDir = path.join(rootDir, 'web/build');
const outDir = path.join(rootDir, 'assets/screenshots');
fs.mkdirSync(outDir, { recursive: true });

const mimeTypes = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.json': 'application/json',
  '.png': 'image/png',
  '.ico': 'image/x-icon'
};

const server = http.createServer((req, res) => {
  const urlPath = req.url.split('?')[0];
  let filePath = path.join(buildDir, urlPath);
  
  if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
    const ext = path.extname(filePath);
    res.writeHead(200, { 'Content-Type': mimeTypes[ext] || 'application/octet-stream' });
    fs.createReadStream(filePath).pipe(res);
    return;
  }
  
  // SPA fallback
  const fallback = path.join(buildDir, '200.html');
  if (fs.existsSync(fallback)) {
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    fs.createReadStream(fallback).pipe(res);
    return;
  }
  
  res.writeHead(404);
  res.end('Not Found');
});

const now = new Date().toISOString();

const categories = [
  { id: 'cat-tech', title: '技术架构', position: 1 },
  { id: 'cat-frontend', title: '前端与设计', position: 2 },
  { id: 'cat-sys', title: '系统与自托管', position: 3 }
];

const feeds = [
  {
    id: 'f-go',
    url: 'https://go.dev/blog/feed.atom',
    title: 'The Go Blog',
    site_url: 'https://go.dev/blog',
    category_id: 'cat-tech',
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
  },
  {
    id: 'f-sqlite',
    url: 'https://sqlite.org/news.rss',
    title: 'SQLite News',
    site_url: 'https://sqlite.org',
    category_id: 'cat-tech',
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
  },
  {
    id: 'f-svelte',
    url: 'https://svelte.dev/blog/rss.xml',
    title: 'Svelte Blog',
    site_url: 'https://svelte.dev',
    category_id: 'cat-frontend',
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
  },
  {
    id: 'f-css',
    url: 'https://developer.mozilla.org/feed.xml',
    title: 'Web Standards Weekly',
    site_url: 'https://developer.mozilla.org',
    category_id: 'cat-frontend',
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
  },
  {
    id: 'f-sys',
    url: 'https://selfhosted.show/rss',
    title: 'Self-Hosted Architecture',
    site_url: 'https://selfhosted.show',
    category_id: 'cat-sys',
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
  }
];

const articles = [
  {
    id: 'art-1',
    feed_id: 'f-sqlite',
    feed_title: 'SQLite News',
    title: '现代 SQLite WAL 模式性能优化与事务隔离实践',
    url: 'https://sqlite.org/wal-performance.html',
    author: 'SQLite Architecture Team',
    published_at: new Date(Date.now() - 3600000 * 2).toISOString(),
    first_seen_at: new Date(Date.now() - 3600000 * 2).toISOString(),
    read_at: null,
    favorite: true,
    hidden: false,
    removed: false,
    note: '重点关注 WAL2 机制与长读事务对检查点执行的影响。',
    tags: ['数据库', '存储优化', '架构'],
    version: 1,
    body_state: 'available',
    content_hash: 'hash-art-1',
    provenance: 'feed',
    read_at_unknown: false,
    imported_at: null,
    protection_until: null,
    content_expires_at: null,
    content: [
      {
        kind: 'feed',
        html: `
          <p>在嵌入式数据库与个人自托管系统中，<strong>SQLite</strong> 凭借其零运维、极低内存占用和单文件便携性，成为了海量工作负载的基石。</p>
          <h3>WAL 模式的核心架构</h3>
          <p>传统回滚日志模式（Rollback Journal）在每次写事务提交时都需要独占整个数据库文件，导致读写互相阻塞。而引入 <code>PRAGMA journal_mode=WAL</code> 之后，写操作将所有新事务页追加至专用的预写日志文件中：</p>
          <ul>
            <li><strong>读写完全解耦：</strong> 活跃的读取事务只读取一致性历史快照，写入操作可在后台并行追加日志，互不阻塞。</li>
            <li><strong>顺序磁盘 IO：</strong> 写入转化为高效的顺序写，极大降低机械硬盘与低功耗 SSD 的写延迟。</li>
            <li><strong>原子检查点（Checkpoint）：</strong> 定期将日志页平滑合并回主库，保证低峰期的磁盘开销最小化。</li>
          </ul>
          <h3>在个人 NAS 与家庭服务器中的配置建议</h3>
          <p>对于在低功耗 NAS 上常驻运行的 RSS 服务，推荐开启 <code>synchronous = NORMAL</code>，既能抵抗操作系统崩溃导致的数据丢失，又可显著缓解瞬时磁盘冲刷瓶颈。</p>
        `,
        text: '在嵌入式数据库与个人自托管系统中，SQLite 凭借其零运维、极低内存占用和单文件便携性成为海量工作负载的基石...',
        hash: 'hash-art-1',
        state: 'available',
        obtained_at: now,
        expires_at: null
      }
    ],
    artifacts: []
  },
  {
    id: 'art-2',
    feed_id: 'f-go',
    feed_title: 'The Go Blog',
    title: 'Go 1.25 编译器在内联优化与逃逸分析方面的突破',
    url: 'https://go.dev/blog/compiler-inlining',
    author: 'Austin Clements',
    published_at: new Date(Date.now() - 3600000 * 4).toISOString(),
    first_seen_at: new Date(Date.now() - 3600000 * 4).toISOString(),
    read_at: null,
    favorite: false,
    hidden: false,
    removed: false,
    note: '',
    tags: ['Go', '编译器', '后端'],
    version: 1,
    body_state: 'available',
    content_hash: 'hash-art-2',
    provenance: 'feed',
    read_at_unknown: false,
    imported_at: null,
    protection_until: null,
    content_expires_at: null,
    content: [
      {
        kind: 'feed',
        html: '<p>Go 编译器在逃逸分析上引入了跨包局部推断，大幅降低微服务与长连接网关的堆内存分配...</p>',
        text: 'Go 编译器在逃逸分析上引入了跨包局部推断...',
        hash: 'hash-art-2',
        state: 'available',
        obtained_at: now,
        expires_at: null
      }
    ],
    artifacts: []
  },
  {
    id: 'art-3',
    feed_id: 'f-svelte',
    feed_title: 'Svelte Blog',
    title: '深入浅出 Svelte 5 响应式系统 Runes 设计哲学',
    url: 'https://svelte.dev/blog/runes-deep-dive',
    author: 'Rich Harris',
    published_at: new Date(Date.now() - 3600000 * 8).toISOString(),
    first_seen_at: new Date(Date.now() - 3600000 * 8).toISOString(),
    read_at: null,
    favorite: false,
    hidden: false,
    removed: false,
    note: '',
    tags: ['前端', 'Svelte', '架构'],
    version: 1,
    body_state: 'available',
    content_hash: 'hash-art-3',
    provenance: 'feed',
    read_at_unknown: false,
    imported_at: null,
    protection_until: null,
    content_expires_at: null,
    content: [
      {
        kind: 'feed',
        html: '<p>Runes 将响应式从编译器魔法推向了明确的语言级信号原语，兼顾静态类型推导与运行时极简开销...</p>',
        text: 'Runes 将响应式从编译器魔法推向了明确的语言级信号原语...',
        hash: 'hash-art-3',
        state: 'available',
        obtained_at: now,
        expires_at: null
      }
    ],
    artifacts: []
  },
  {
    id: 'art-4',
    feed_id: 'f-sys',
    feed_title: 'Self-Hosted Architecture',
    title: '家庭服务器存储演进：从单点存储到异地一致性热备份',
    url: 'https://selfhosted.show/backup-topology',
    author: 'Alex Kretzschmar',
    published_at: new Date(Date.now() - 3600000 * 12).toISOString(),
    first_seen_at: new Date(Date.now() - 3600000 * 12).toISOString(),
    read_at: null,
    favorite: true,
    hidden: false,
    removed: false,
    note: '',
    tags: ['自托管', 'NAS', '备份'],
    version: 1,
    body_state: 'available',
    content_hash: 'hash-art-4',
    provenance: 'feed',
    read_at_unknown: false,
    imported_at: null,
    protection_until: null,
    content_expires_at: null,
    content: [
      {
        kind: 'feed',
        html: '<p>构建高韧性个人数据中枢的关键，在于将持久状态与临时计算进程彻底解耦...</p>',
        text: '构建高韧性个人数据中枢的关键...',
        hash: 'hash-art-4',
        state: 'available',
        obtained_at: now,
        expires_at: null
      }
    ],
    artifacts: []
  },
  {
    id: 'art-5',
    feed_id: 'f-css',
    feed_title: 'Web Standards Weekly',
    title: '现代 CSS 容器查询（Container Queries）实战指南',
    url: 'https://developer.mozilla.org/css-container-queries',
    author: 'Rachel Andrew',
    published_at: new Date(Date.now() - 3600000 * 16).toISOString(),
    first_seen_at: new Date(Date.now() - 3600000 * 16).toISOString(),
    read_at: null,
    favorite: false,
    hidden: false,
    removed: false,
    note: '',
    tags: ['CSS', 'Web标准'],
    version: 1,
    body_state: 'available',
    content_hash: 'hash-art-5',
    provenance: 'feed',
    read_at_unknown: false,
    imported_at: null,
    protection_until: null,
    content_expires_at: null,
    content: [
      {
        kind: 'feed',
        html: '<p>容器查询彻底颠覆了基于整个视口的响应式布局设计，使组件能够真正根据自身容器宽度自适应...</p>',
        text: '容器查询彻底颠覆了基于整个视口的响应式布局设计...',
        hash: 'hash-art-5',
        state: 'available',
        obtained_at: now,
        expires_at: null
      }
    ],
    artifacts: []
  }
];

const reportResult = {
  blocks: [
    {
      kind: 'fact',
      text: '今日重点聚焦在现代存储系统优化与语言运行时演进。SQLite 团队发布了关于 WAL（预写日志）模式在长读事务并发下的性能实测报告，阐述了检查点阻塞防范策略。',
      evidence_ids: ['e1'],
      citations: [
        {
          article_id: 'art-1',
          title: '现代 SQLite WAL 模式性能优化与事务隔离实践',
          url: '/?scope=all&article=art-1&report=rep-1&anchor=overview-b0-c0',
          basis_hash: 'hash-art-1',
          kind: 'feed_excerpt'
        }
      ]
    },
    {
      kind: 'fact',
      text: '在后端与语言生态方面，Go 1.25 编译器对局部逃逸分析的推断增强显著减少了短命闭包的堆分配压力，提升了长效常驻服务的 GC 吞吐表现。',
      evidence_ids: ['e2'],
      citations: [
        {
          article_id: 'art-2',
          title: 'Go 1.25 编译器在内联优化与逃逸分析方面的突破',
          url: '/?scope=all&article=art-2&report=rep-1&anchor=overview-b1-c0',
          basis_hash: 'hash-art-2',
          kind: 'feed_excerpt'
        }
      ]
    },
    {
      kind: 'fact',
      text: '现代 Web 工程体系中，Svelte 5 提出的 Runes 响应式原语消除了传统编译期魔法的不透明性，为无虚拟 DOM 架构带来了更清晰的代码组织范式与更优的运行时内存效率。',
      evidence_ids: ['e3'],
      citations: [
        {
          article_id: 'art-3',
          title: '深入浅出 Svelte 5 响应式系统 Runes 设计哲学',
          url: '/?scope=all&article=art-3&report=rep-1&anchor=overview-b2-c0',
          basis_hash: 'hash-art-3',
          kind: 'feed_excerpt'
        }
      ]
    }
  ],
  groups: [
    {
      id: 'grp-tech',
      title: '技术架构与后端',
      category_id: 'cat-tech',
      category_title: '技术架构',
      importance: 'high',
      total: 2,
      blocks: [
        {
          kind: 'fact',
          text: 'SQLite WAL 模式与 Go 编译器优化构成本次总结核心。建议自建服务优先启用 WAL 并配置常规自动快照备份。',
          evidence_ids: ['e1'],
          citations: [
            {
              article_id: 'art-1',
              title: '现代 SQLite WAL 模式性能优化与事务隔离实践',
              url: '/?scope=all&article=art-1&report=rep-1&anchor=grp-tech-b0-c0',
              basis_hash: 'hash-art-1',
              kind: 'feed_excerpt'
            }
          ]
        }
      ]
    }
  ],
  counts: {
    total: 16,
    summarized: 16,
    reused: 0,
    fulltext: 5,
    pending: 0,
    fallback: 0,
    summarized_articles: 16,
    unread_summarized: 16
  },
  trace: ['今日未读聚合快照 · 2026-10-16 08:00'],
  degraded: false
};

const reportRun = {
  id: 'rep-1',
  kind: 'report',
  state: 'complete',
  error: '',
  conversation_id: 'c-1',
  high_watermark: 'art-1',
  config_version: 1,
  total: 16,
  completed: 16,
  cutoff: now,
  created_at: now,
  updated_at: now,
  due_at: now,
  result: reportResult
};

const conversations = [
  {
    id: 'c-1',
    title: '关于 SQLite WAL 性能提问',
    report_id: 'rep-1',
    created_at: now,
    updated_at: now
  }
];

const messages = [
  {
    id: 'm-1',
    role: 'user',
    text: 'SQLite WAL 模式的核心优势是什么？在家庭服务器中如何配置？',
    result: { blocks: [], groups: [], counts: reportResult.counts, trace: [], degraded: false },
    run_id: 'run-chat-1',
    created_at: new Date(Date.now() - 60000).toISOString()
  },
  {
    id: 'm-2',
    role: 'assistant',
    text: '根据今日晨报引用的技术文章，SQLite WAL（预写日志）模式的核心优势在于将读取与写入操作彻底解耦：\n\n1. 读取操作仅访问历史快照，无需等待写入事务完成，彻底告别锁库等待；\n2. 写入操作转化为顺序日志追加，显著提升磁盘写入吞吐；\n3. 在家庭服务器与 NAS 场景中，推荐配合 `synchronous = NORMAL` 使用，既可防止系统崩溃损毁数据，又能大幅降低小文件写放大。',
    result: {
      blocks: [
        {
          kind: 'fact',
          text: 'WAL 模式解耦读写事务，配合 synchronous=NORMAL 可在低功耗 NAS 达到最佳吞吐与安全性平衡。',
          evidence_ids: ['art-1'],
          citations: [
            {
              article_id: 'art-1',
              title: '现代 SQLite WAL 模式性能优化与事务隔离实践',
              url: '/?scope=all&article=art-1',
              basis_hash: 'hash-art-1',
              kind: 'feed_excerpt'
            }
          ]
        }
      ],
      groups: [],
      counts: reportResult.counts,
      trace: [],
      degraded: false
    },
    run_id: 'run-chat-1',
    created_at: now
  }
];

server.listen(41099, '127.0.0.1', async () => {
  console.log('Static server listening on http://127.0.0.1:41099');
  
  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 1360, height: 880 },
    deviceScaleFactor: 2,
    colorScheme: 'light'
  });
  
  const page = await context.newPage();
  
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname;
    const method = req.method();
    
    let body = {};
    let status = 200;
    
    if (path === '/api/v1/session') {
      body = { authenticated: true, csrf_token: 'demo-csrf-token' };
    } else if (path === '/api/v1/feeds') {
      body = feeds;
    } else if (path === '/api/v1/categories') {
      body = categories;
    } else if (path === '/api/v1/articles') {
      body = {
        items: articles,
        total: articles.length,
        has_more: false,
        unread_count: 16
      };
    } else if (path.startsWith('/api/v1/articles/')) {
      const id = path.split('/').pop();
      body = articles.find((a) => a.id === id) || articles[0];
    } else if (path === '/api/v1/agent/settings') {
      body = {
        version: 1,
        enabled: true,
        configured: true,
        key_configured: true,
        name: '拾流阅读助理',
        base_url: 'https://api.openai.com/v1',
        model: 'gpt-4o-mini',
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
    } else if (path === '/api/v1/agent/usage') {
      body = { tokens: 14280, calls: 3, unknown_calls: 0 };
    } else if (path === '/api/v1/agent/report-runs') {
      body = reportRun;
    } else if (path.startsWith('/api/v1/agent/runs/')) {
      body = reportRun;
    } else if (path === '/api/v1/agent/runs') {
      body = [reportRun];
    } else if (path === '/api/v1/agent/conversations') {
      body = conversations;
    } else if (path.includes('/messages')) {
      body = messages;
    } else if (path.includes('/members')) {
      body = [];
    }
    
    await route.fulfill({
      status,
      contentType: 'application/json',
      body: JSON.stringify(body)
    });
  });

  // 1. Capture Morning Briefing with Chat Drawer
  console.log('Capturing morning brief...');
  await page.goto('http://127.0.0.1:41099/');
  await page.waitForSelector('text=未读晨报', { timeout: 10000 });
  // Expand chat
  const chatBtn = page.getByRole('button', { name: '围绕报告提问' });
  if (await chatBtn.isVisible()) {
    await chatBtn.click();
  }
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(outDir, 'morning-brief.png') });
  console.log('Saved morning-brief.png');

  // 2. Capture Clean Reader View
  console.log('Capturing reading view...');
  await page.goto('http://127.0.0.1:41099/?scope=all&article=art-1');
  await page.waitForSelector('text=现代 SQLite WAL 模式性能优化与事务隔离实践', { timeout: 10000 });
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(outDir, 'reading-view.png') });
  console.log('Saved reading-view.png');

  // 3. Capture Article List View
  console.log('Capturing article list...');
  await page.goto('http://127.0.0.1:41099/?scope=all');
  await page.waitForSelector('text=近期文章', { timeout: 10000 });
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(outDir, 'article-list.png') });
  console.log('Saved article-list.png');

  await browser.close();
  server.close();
  console.log('All screenshots captured successfully!');
  process.exit(0);
});
