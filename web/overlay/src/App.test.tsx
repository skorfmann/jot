import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { clearOverlayCache } from './api';
import { App, LAST_TAB_KEY } from './App';
import type { BootstrapPayload } from './types';

const bootstrap: BootstrapPayload = {
  slug: 'report',
  deployId: '01HX0000000000000000000001',
  title: 'Quarterly report',
  url: 'https://jot.example.com/report/',
  createdBy: 'dev@local',
  createdAt: '2026-05-20T10:00:00Z',
  tags: ['finance'],
  apiBase: '/_api'
};

beforeEach(() => {
  clearOverlayCache();
  window.localStorage.clear();
  vi.restoreAllMocks();
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ deploys: [] }), { status: 200 })));
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: {
      writeText: vi.fn().mockResolvedValue(undefined)
    }
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('App', () => {
  it('opens and closes from the launcher', async () => {
    const user = userEvent.setup();
    render(<App bootstrap={bootstrap} />);

    await user.click(screen.getByRole('button', { name: /open jot overlay/i }));
    expect(screen.getByRole('dialog', { name: /jot deploy overlay/i })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /close overlay/i }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('toggles with Cmd/Ctrl+J and ignores typing contexts', async () => {
    render(<App bootstrap={bootstrap} />);

    const input = document.createElement('input');
    document.body.append(input);
    input.focus();
    await act(async () => {
      fireEvent.keyDown(input, { key: 'j', metaKey: true });
    });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    await act(async () => {
      fireEvent.keyDown(document, { key: 'j', metaKey: true });
    });
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    input.remove();
  });

  it('traps focus, closes on Esc, and returns focus to the launcher', async () => {
    const user = userEvent.setup();
    render(<App bootstrap={bootstrap} />);

    const launcher = screen.getByRole('button', { name: /open jot overlay/i });
    await user.click(launcher);
    const close = await screen.findByRole('button', { name: /close overlay/i });
    await waitFor(() => expect(close).toHaveFocus());

    const contextTab = screen.getByRole('tab', { name: /context/i });
    contextTab.focus();
    fireEvent.keyDown(contextTab, { key: 'Tab' });
    expect(close).toHaveFocus();

    fireEvent.keyDown(close, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(launcher).toHaveFocus());
  });

  it('restores and persists the last open tab', async () => {
    const user = userEvent.setup();
    window.localStorage.setItem(LAST_TAB_KEY, 'context');
    render(<App bootstrap={bootstrap} />);

    await user.click(screen.getByRole('button', { name: /open jot overlay/i }));
    expect(screen.getByRole('tab', { name: /context/i })).toHaveAttribute('aria-selected', 'true');

    await user.click(screen.getByRole('tab', { name: /mine/i }));
    expect(window.localStorage.getItem(LAST_TAB_KEY)).toBe('activity');
  });

  it('aborts in-flight requests when the panel closes', async () => {
    const signals: AbortSignal[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: string | URL | Request, init?: RequestInit) => {
        if (init?.signal) {
          signals.push(init.signal);
        }
        return new Promise<Response>(() => undefined);
      })
    );
    const user = userEvent.setup();
    render(<App bootstrap={bootstrap} />);

    await user.click(screen.getByRole('button', { name: /open jot overlay/i }));
    await waitFor(() => expect(signals).toHaveLength(3));

    await user.click(screen.getByRole('button', { name: /close overlay/i }));
    expect(signals.every(signal => signal.aborted)).toBe(true);
  });

  it('copies context values and shows an ephemeral toast', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText }
    });
    render(<App bootstrap={bootstrap} />);

    await user.click(screen.getByRole('button', { name: /open jot overlay/i }));
    await user.click(screen.getByRole('tab', { name: /context/i }));
    await user.click(screen.getByRole('button', { name: /copy slug/i }));

    expect(writeText).toHaveBeenCalledWith('report');
    expect(await screen.findByRole('status')).toHaveTextContent('Copied slug');
  });

  it('links deploy cards to immutable deploy IDs', async () => {
    const user = userEvent.setup();
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: string | URL | Request) => {
        const url = String(input);
        const deploys = url.includes('/_api/slugs/report/history')
          ? [
              {
                schema_version: 1,
                id: '01HX0000000000000000000009',
                slug: 'report',
                created_at: '2026-05-20T10:00:00Z',
                created_by: 'dev@local',
                title: 'Historical report',
                files: {}
              }
            ]
          : [];
        return new Response(JSON.stringify({ deploys }), { status: 200 });
      })
    );
    render(<App bootstrap={bootstrap} />);

    await user.click(screen.getByRole('button', { name: /open jot overlay/i }));

    const link = await screen.findByRole('link', { name: /open/i });
    expect(link.getAttribute('href')).toBe('/01HX0000000000000000000009/');
  });
});
