import { beforeEach, describe, expect, it, vi } from 'vitest';
import { clearOverlayCache, fetchJSONCached } from './api';
import type { DeployListResponse } from './types';

beforeEach(() => {
  clearOverlayCache();
  vi.restoreAllMocks();
});

describe('fetchJSONCached', () => {
  it('serves cached data within the TTL and refreshes after expiry', async () => {
    const responses: DeployListResponse[] = [
      { deploys: [{ schema_version: 1, id: 'a', slug: 'one', created_at: '2026-05-20T10:00:00Z', created_by: 'dev@local', files: {} }] },
      { deploys: [{ schema_version: 1, id: 'b', slug: 'two', created_at: '2026-05-20T10:01:00Z', created_by: 'dev@local', files: {} }] }
    ];
    const fetchMock = vi.fn(async () => new Response(JSON.stringify(responses.shift()), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const signal = new AbortController().signal;

    const first = await fetchJSONCached<DeployListResponse>('key', '/_api/deploys', 45_000, signal, 1_000);
    const second = await fetchJSONCached<DeployListResponse>('key', '/_api/deploys', 45_000, signal, 2_000);
    const third = await fetchJSONCached<DeployListResponse>('key', '/_api/deploys', 45_000, signal, 50_001);

    expect(first.deploys[0].slug).toBe('one');
    expect(second.deploys[0].slug).toBe('one');
    expect(third.deploys[0].slug).toBe('two');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
