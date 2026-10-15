import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';
import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { readFileSync } from 'node:fs';
const env = Object.fromEntries(
  readFileSync(new URL('../config/dev.env', import.meta.url), 'utf8')
    .split('\n')
    .filter((x) => x && !x.startsWith('#'))
    .map((x) => x.split('='))
);
export default defineConfig({
  plugins: [
    tailwindcss(),
    sveltekit({ preprocess: vitePreprocess(), adapter: adapter({ fallback: 'index.html' }) })
  ],
  server: {
    host: '127.0.0.1',
    port: Number(env.SHILIU_WEB_PORT),
    strictPort: true,
    proxy: { '/api': { target: `http://${env.SHILIU_ADDR}` } }
  },
  preview: {
    host: '127.0.0.1',
    port: Number(env.SHILIU_WEB_PORT),
    strictPort: true,
    proxy: { '/api': { target: `http://${env.SHILIU_ADDR}` } }
  }
});
