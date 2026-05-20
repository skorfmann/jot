import type { DeployListResponse, DeployManifest } from './types';

type CacheEntry<T> = {
  expiresAt: number;
  data: T;
};

const cache = new Map<string, CacheEntry<unknown>>();

export const TTL = {
  history: 20_000,
  deploys: 45_000
} as const;

export function clearOverlayCache() {
  cache.clear();
}

export async function fetchJSONCached<T>(
  key: string,
  url: string,
  ttlMs: number,
  signal: AbortSignal,
  now = Date.now()
): Promise<T> {
  const cached = cache.get(key) as CacheEntry<T> | undefined;
  if (cached && cached.expiresAt > now) {
    return cached.data;
  }

  const response = await fetch(url, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    signal
  });
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText || 'Request failed'}`.trim());
  }
  const data = (await response.json()) as T;
  cache.set(key, { expiresAt: now + ttlMs, data });
  return data;
}

export async function fetchSlugHistory(apiBase: string, slug: string, signal: AbortSignal): Promise<DeployManifest[]> {
  const safeSlug = encodeURIComponent(slug);
  const data = await fetchJSONCached<DeployListResponse>(
    `history:${slug}`,
    `${apiBase}/slugs/${safeSlug}/history`,
    TTL.history,
    signal
  );
  return data.deploys ?? [];
}

export async function fetchMyDeploys(apiBase: string, signal: AbortSignal): Promise<DeployManifest[]> {
  const data = await fetchJSONCached<DeployListResponse>(
    'deploys:mine',
    `${apiBase}/deploys?mine=1&limit=50`,
    TTL.deploys,
    signal
  );
  return latestDeployPerSlug(data.deploys ?? []);
}

export async function fetchGlobalDeploys(apiBase: string, signal: AbortSignal): Promise<DeployManifest[]> {
  const data = await fetchJSONCached<DeployListResponse>(
    'deploys:global',
    `${apiBase}/deploys?limit=50`,
    TTL.deploys,
    signal
  );
  return latestDeployPerSlug(data.deploys ?? []);
}

export function latestDeployPerSlug(deploys: DeployManifest[]): DeployManifest[] {
  const seen = new Set<string>();
  const latest: DeployManifest[] = [];
  for (const deploy of deploys) {
    const key = deploy.slug || deploy.id;
    if (!key || seen.has(key)) {
      continue;
    }
    seen.add(key);
    latest.push(deploy);
  }
  return latest;
}
