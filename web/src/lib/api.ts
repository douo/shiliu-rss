import createClient from 'openapi-fetch';
import type { paths, components } from './api.generated';
import type { Article, ArticlePatch, Category, Feed } from './models';
let csrf = '';
export class APIError extends Error {
  constructor(
    public status: number,
    message: string
  ) {
    super(message);
  }
}
const client = createClient<paths>({ credentials: 'same-origin', cache: 'no-store' });
client.use({
  onRequest({ request }) {
    if (!['GET', 'HEAD'].includes(request.method)) request.headers.set('X-CSRF-Token', csrf);
    return request;
  },
  async onResponse({ response }) {
    if (response.status === 401) window.dispatchEvent(new Event('shiliu:session-expired'));
    if (!response.ok) {
      const problem = await response
        .clone()
        .json()
        .catch(() => ({}));
      throw new APIError(
        response.status,
        (
          {
            agent_disabled: '请先配置并启用 Agent',
            agent_budget_exhausted: '已达到今日调用预算',
            invalid_request: '请检查输入内容',
            version_mismatch: '配置已更新，请刷新后重试'
          } as Record<string, string>
        )[problem.code] ||
          problem.detail ||
          problem.title ||
          `请求失败 (${response.status})`
      );
    }
  }
});
async function data<T>(response: Promise<{ data?: T }>): Promise<T> {
  return (await response).data as T;
}
const feedInput = (feed: Partial<Feed>): components['schemas']['FeedInput'] => ({
  url: feed.url || '',
  title: feed.title || '',
  site_url: feed.site_url,
  category_id: feed.category_id,
  allow_host: feed.allow_host,
  paused: feed.paused,
  archive: feed.archive,
  interval_hours: feed.interval_hours,
  read_days: feed.read_days,
  unread_days: feed.unread_days,
  importance: feed.importance,
  version: feed.version
});
export const api = {
  async session() {
    const r = await data(client.GET('/api/v1/session'));
    csrf = r.csrf_token;
    return r;
  },
  async login(password: string) {
    const r = await data(client.POST('/api/v1/session', { body: { password } }));
    csrf = r.csrf_token;
    return r;
  },
  logout: () => data(client.DELETE('/api/v1/session')),
  feeds: () => data(client.GET('/api/v1/feeds')),
  categories: () => data(client.GET('/api/v1/categories')),
  articles: (query: Record<string, string>) =>
    data(
      client.GET('/api/v1/articles', {
        params: {
          query: {
            scope: (query.scope || 'recent') as 'recent' | 'archive' | 'all',
            feed_id: query.feed_id,
            category_id: query.category_id,
            q: query.q,
            read: query.read ? query.read === 'true' : undefined,
            favorite: query.favorite ? query.favorite === 'true' : undefined,
            from: query.from,
            until: query.until,
            cursor: query.cursor,
            limit: Number(query.limit || 50)
          }
        }
      })
    ),
  article: (id: string) => data(client.GET('/api/v1/articles/{id}', { params: { path: { id } } })),
  patch: (a: Article, patch: ArticlePatch) =>
    data(
      client.PATCH('/api/v1/articles/{id}', {
        params: { path: { id: a.id } },
        body: { ...patch, version: a.version }
      })
    ),
  saveFeed: (feed: Partial<Feed>) =>
    feed.id
      ? data(
          client.PATCH('/api/v1/feeds/{id}', {
            params: { path: { id: feed.id } },
            body: feedInput(feed)
          })
        )
      : data(client.POST('/api/v1/feeds', { body: feedInput(feed) })),
  saveCategory: (title: string) => data(client.POST('/api/v1/categories', { body: { title } })),
  editCategory: (c: Category, title: string) =>
    data(
      client.PATCH('/api/v1/categories/{id}', {
        params: { path: { id: c.id } },
        body: { title, version: c.version }
      })
    ),
  deleteCategory: (c: Category) =>
    data(
      client.DELETE('/api/v1/categories/{id}', {
        params: { path: { id: c.id }, query: { version: c.version } }
      })
    ),
  refresh: (id: string) =>
    data(
      client.POST('/api/v1/feeds/{id}/refresh', {
        params: { path: { id }, header: { 'Idempotency-Key': crypto.randomUUID() } }
      })
    ),
  fulltext: (id: string) =>
    data(
      client.POST('/api/v1/articles/{id}/fulltext-jobs', {
        params: { path: { id }, header: { 'Idempotency-Key': crypto.randomUUID() } }
      })
    ),
  job: (id: string) => data(client.GET('/api/v1/jobs/{id}', { params: { path: { id } } })),
  preview: (feed: Partial<Feed>) =>
    data(
      client.POST('/api/v1/retention-preview', {
        body: { feed: feedInput(feed), feed_id: feed.id }
      })
    ),
  tokens: () => data(client.GET('/api/v1/tokens')),
  createToken: (name: string, scopes: string[]) =>
    data(client.POST('/api/v1/tokens', { body: { name, scopes } })),
  revoke: (id: string) => data(client.DELETE('/api/v1/tokens/{id}', { params: { path: { id } } })),
  importOPML: (xml: string) => data(client.POST('/api/v1/opml', { body: { xml } })),
  exportOPML: () => data(client.GET('/api/v1/opml', { parseAs: 'text' })),
  agentSettings: () => data(client.GET('/api/v1/agent/settings')),
  saveAgentSettings: (settings: components['schemas']['AgentSettings'], api_key?: string) =>
    data(client.PUT('/api/v1/agent/settings', { body: { settings, api_key } })),
  agentUsage: () => data(client.GET('/api/v1/agent/usage')),
  agentTest: () =>
    data(
      client.POST('/api/v1/agent/connection-tests', {
        params: { header: { 'Idempotency-Key': crypto.randomUUID() } }
      })
    ),
  agentReport: () =>
    data(
      client.POST('/api/v1/agent/report-runs', {
        params: { header: { 'Idempotency-Key': crypto.randomUUID() } }
      })
    ),
  agentRuns: (kind = 'report', before = '') =>
    data(client.GET('/api/v1/agent/runs', { params: { query: { kind, before } } })),
  agentRun: (id: string) =>
    data(client.GET('/api/v1/agent/runs/{id}', { params: { path: { id } } })),
  agentControl: (id: string, action: 'cancel' | 'retry') =>
    data(
      client.POST('/api/v1/agent/runs/{id}/control', { params: { path: { id } }, body: { action } })
    ),
  agentMembers: (id: string, after = '') =>
    data(
      client.GET('/api/v1/agent/reports/{id}/members', {
        params: { path: { id }, query: { after, limit: 50 } }
      })
    ),
  agentMarkRead: (id: string, groupId: string) =>
    data(
      client.POST('/api/v1/agent/reports/{id}/read', {
        params: {
          path: { id },
          query: { group_id: groupId },
          header: { 'Idempotency-Key': crypto.randomUUID() }
        }
      })
    ),
  agentConversations: (before = '') =>
    data(client.GET('/api/v1/agent/conversations', { params: { query: { before } } })),
  agentConversation: (report_id?: string) =>
    data(
      client.POST('/api/v1/agent/conversations', {
        params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
        body: { report_id }
      })
    ),
  agentMessages: (id: string, before = '') =>
    data(
      client.GET('/api/v1/agent/conversations/{id}/messages', {
        params: { path: { id }, query: { before } }
      })
    ),
  agentSend: (id: string, text: string) =>
    data(
      client.POST('/api/v1/agent/conversations/{id}/messages', {
        params: { path: { id }, header: { 'Idempotency-Key': crypto.randomUUID() } },
        body: { text }
      })
    ),
  health: () => data(client.GET('/api/v1/health'))
};
