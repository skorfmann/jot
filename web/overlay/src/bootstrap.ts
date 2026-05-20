import type { BootstrapPayload } from './types';

export const defaultBootstrap: BootstrapPayload = {
  slug: null,
  deployId: null,
  title: null,
  url: window.location.href,
  createdBy: null,
  createdAt: null,
  tags: [],
  apiBase: '/_api'
};

export function readBootstrap(): BootstrapPayload {
  const el = document.getElementById('jot-overlay-bootstrap');
  if (!el?.textContent) {
    return defaultBootstrap;
  }
  try {
    const parsed = JSON.parse(el.textContent) as Partial<BootstrapPayload>;
    return {
      ...defaultBootstrap,
      ...parsed,
      tags: Array.isArray(parsed.tags) ? parsed.tags.filter((tag): tag is string => typeof tag === 'string') : []
    };
  } catch {
    return defaultBootstrap;
  }
}

export function overlayStylesheetHref(): string | null {
  return document.querySelector<HTMLLinkElement>('link[data-jot-overlay-style]')?.href ?? null;
}
