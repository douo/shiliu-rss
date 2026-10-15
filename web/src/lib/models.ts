import type { components } from './api.generated';
export type Feed = components['schemas']['Feed'];
export type Category = components['schemas']['Category'];
export type Article = components['schemas']['Article'];
export type ArticlePage = components['schemas']['ArticlePage'];
export type ArticlePatch = Omit<components['schemas']['ArticlePatch'], 'version'>;
export type Artifact = components['schemas']['Artifact'];
export type Token = components['schemas']['Token'];
export type Job = components['schemas']['Job'];
