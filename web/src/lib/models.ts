import type { components } from './api.generated';
export type Feed = components['schemas']['Feed'];
export type Category = components['schemas']['Category'];
export type Article = components['schemas']['Article'];
export type ArticlePage = components['schemas']['ArticlePage'];
export type ArticlePatch = Omit<components['schemas']['ArticlePatch'], 'version'>;
export type Artifact = components['schemas']['Artifact'];
export type Token = components['schemas']['Token'];
export type Job = components['schemas']['Job'];

export type AgentSettings = components['schemas']['AgentSettings'];
export type AgentRun = components['schemas']['AgentRun'];
export type AgentResult = components['schemas']['AgentResult'];
export type AgentMember = components['schemas']['AgentMember'];
export type AgentBlock = components['schemas']['AgentBlock'];
export type AgentMessage = components['schemas']['AgentMessage'];
export type AgentConversation = components['schemas']['AgentConversation'];
export type AgentUsage = components['schemas']['AgentUsage'];
