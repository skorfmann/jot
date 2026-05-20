import {
  Check,
  Clock3,
  Copy,
  ExternalLink,
  FileText,
  Globe2,
  History,
  ListRestart,
  Loader2,
  PanelRightOpen,
  RotateCw,
  UserRound,
  X
} from 'lucide-react';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { fetchGlobalDeploys, fetchMyDeploys, fetchSlugHistory } from './api';
import type { BootstrapPayload, DeployManifest, OverlayTab, SectionKey, SectionState } from './types';

export const LAST_TAB_KEY = 'jot-overlay:last-tab';

const tabs: Array<{ id: OverlayTab; label: string; icon: React.ElementType }> = [
  { id: 'history', label: 'History', icon: History },
  { id: 'activity', label: 'Mine', icon: UserRound },
  { id: 'global', label: 'Discover', icon: Globe2 },
  { id: 'context', label: 'Context', icon: FileText }
];

const emptySection: SectionState = {
  status: 'idle',
  data: [],
  error: null
};

type AppProps = {
  bootstrap: BootstrapPayload;
};

export function App({ bootstrap }: AppProps) {
  const [open, setOpen] = useState(false);
  const launcherRef = useRef<HTMLButtonElement | null>(null);
  const closeRef = useRef<HTMLButtonElement | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const toastTimer = useRef<number | null>(null);
  const [activeTab, setActiveTab] = useState<OverlayTab>(() => initialTab(bootstrap.slug));
  const [sections, setSections] = useState<Record<SectionKey, SectionState>>({
    history: emptySection,
    activity: emptySection,
    global: emptySection
  });

  const closePanel = useCallback(() => {
    setOpen(false);
    window.setTimeout(() => launcherRef.current?.focus(), 0);
  }, []);

  const togglePanel = useCallback(() => {
    setOpen(value => {
      if (value) {
        window.setTimeout(() => launcherRef.current?.focus(), 0);
      }
      return !value;
    });
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== 'j' || (!event.metaKey && !event.ctrlKey) || event.altKey) {
        return;
      }
      if (isTypingTarget(event.target)) {
        return;
      }
      event.preventDefault();
      togglePanel();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [togglePanel]);

  useEffect(() => {
    if (!open) {
      abortRef.current?.abort();
      abortRef.current = null;
      return;
    }

    const controller = new AbortController();
    abortRef.current = controller;
    if (bootstrap.slug) {
      void loadSection('history', controller.signal, () =>
        fetchSlugHistory(bootstrap.apiBase, bootstrap.slug as string, controller.signal)
      );
    }
    void loadSection('activity', controller.signal, () => fetchMyDeploys(bootstrap.apiBase, controller.signal));
    void loadSection('global', controller.signal, () => fetchGlobalDeploys(bootstrap.apiBase, controller.signal));

    window.setTimeout(() => closeRef.current?.focus(), 0);
    return () => controller.abort();
  }, [bootstrap.apiBase, bootstrap.slug, open]);

  useEffect(() => {
    return () => {
      if (toastTimer.current) {
        window.clearTimeout(toastTimer.current);
      }
      abortRef.current?.abort();
    };
  }, []);

  const selectTab = useCallback((tab: OverlayTab) => {
    setActiveTab(tab);
    try {
      window.localStorage.setItem(LAST_TAB_KEY, tab);
    } catch {
      // localStorage can be unavailable in restricted browser contexts.
    }
  }, []);

  const loadSection = useCallback(
    async (key: SectionKey, signal: AbortSignal, loader: () => Promise<DeployManifest[]>) => {
      setSections(current => ({
        ...current,
        [key]: { ...current[key], status: 'loading', error: null }
      }));
      try {
        const data = await loader();
        if (signal.aborted) {
          return;
        }
        setSections(current => ({
          ...current,
          [key]: { status: 'ready', data, error: null }
        }));
      } catch (error) {
        if (signal.aborted || isAbortError(error)) {
          return;
        }
        setSections(current => ({
          ...current,
          [key]: {
            status: 'error',
            data: current[key].data,
            error: error instanceof Error ? error.message : 'Request failed'
          }
        }));
      }
    },
    []
  );

  const retrySection = useCallback(
    (key: SectionKey) => {
      const controller = new AbortController();
      abortRef.current = controller;
      if (key === 'history' && bootstrap.slug) {
        const slug = bootstrap.slug;
        void loadSection(key, controller.signal, () => fetchSlugHistory(bootstrap.apiBase, slug, controller.signal));
      }
      if (key === 'activity') {
        void loadSection(key, controller.signal, () => fetchMyDeploys(bootstrap.apiBase, controller.signal));
      }
      if (key === 'global') {
        void loadSection(key, controller.signal, () => fetchGlobalDeploys(bootstrap.apiBase, controller.signal));
      }
    },
    [bootstrap.apiBase, bootstrap.slug, loadSection]
  );

  const showToast = useCallback((message: string) => {
    setToast(message);
    if (toastTimer.current) {
      window.clearTimeout(toastTimer.current);
    }
    toastTimer.current = window.setTimeout(() => setToast(null), 1500);
  }, []);

  const copyValue = useCallback(
    async (label: string, value: string | null) => {
      if (!value) {
        return;
      }
      try {
        await navigator.clipboard.writeText(value);
        showToast(`Copied ${label}`);
      } catch {
        showToast('Copy failed');
      }
    },
    [showToast]
  );

  const contextRows = useMemo(
    () => [
      { label: 'Slug', value: bootstrap.slug },
      { label: 'Deploy ID', value: bootstrap.deployId },
      { label: 'Title', value: bootstrap.title },
      { label: 'Created by', value: bootstrap.createdBy },
      { label: 'Created at', value: formatDateTime(bootstrap.createdAt) },
      { label: 'URL', value: bootstrap.url },
      { label: 'Tags', value: bootstrap.tags.length > 0 ? bootstrap.tags.join(', ') : null }
    ],
    [bootstrap]
  );

  const handlePanelKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closePanel();
      return;
    }
    if (event.key !== 'Tab') {
      return;
    }
    trapFocus(event, panelRef.current);
  };

  return (
    <>
      <button
        ref={launcherRef}
        type="button"
        className="jot-launcher"
        aria-label={open ? 'Close Jot overlay' : 'Open Jot overlay'}
        aria-expanded={open}
        onClick={togglePanel}
      >
        <PanelRightOpen aria-hidden="true" size={22} />
      </button>

      {open ? (
        <div className="jot-layer" role="presentation">
          <div
            ref={panelRef}
            className="jot-panel"
            role="dialog"
            aria-modal="true"
            aria-label="Jot deploy overlay"
            onKeyDown={handlePanelKeyDown}
          >
            <header className="jot-panel-header">
              <div>
                <p className="jot-kicker">Jot</p>
                <h2>{bootstrap.title || bootstrap.slug || 'Deploy context'}</h2>
              </div>
              <button ref={closeRef} type="button" className="jot-icon-button" aria-label="Close overlay" onClick={closePanel}>
                <X aria-hidden="true" size={20} />
              </button>
            </header>

            <nav className="jot-tabs" role="tablist" aria-label="Jot overlay sections">
              {tabs.map(tab => {
                const Icon = tab.icon;
                const selected = activeTab === tab.id;
                const disabled = tab.id === 'history' && !bootstrap.slug;
                return (
                  <button
                    key={tab.id}
                    type="button"
                    role="tab"
                    aria-selected={selected}
                    aria-controls={`jot-tab-${tab.id}`}
                    id={`jot-tab-button-${tab.id}`}
                    className="jot-tab"
                    disabled={disabled}
                    onClick={() => selectTab(disabled ? 'context' : tab.id)}
                  >
                    <Icon aria-hidden="true" size={16} />
                    <span>{tab.label}</span>
                  </button>
                );
              })}
            </nav>

            <main className="jot-panel-body">
              <section
                id="jot-tab-history"
                role="tabpanel"
                aria-labelledby="jot-tab-button-history"
                hidden={activeTab !== 'history'}
              >
                <DeploySection
                  title="Current slug history"
                  state={bootstrap.slug ? sections.history : { status: 'ready', data: [], error: null }}
                  emptyCopy={bootstrap.slug ? 'No deploys have been recorded for this slug.' : 'This page is not tied to a slug.'}
                  onRetry={() => retrySection('history')}
                />
              </section>

              <section
                id="jot-tab-activity"
                role="tabpanel"
                aria-labelledby="jot-tab-button-activity"
                hidden={activeTab !== 'activity'}
              >
                <DeploySection
                  title="My activity"
                  state={sections.activity}
                  emptyCopy="You have not published anything yet."
                  onRetry={() => retrySection('activity')}
                />
              </section>

              <section
                id="jot-tab-global"
                role="tabpanel"
                aria-labelledby="jot-tab-button-global"
                hidden={activeTab !== 'global'}
              >
                <DeploySection
                  title="Discoverable deploys"
                  state={sections.global}
                  emptyCopy="No discoverable deploys are available."
                  onRetry={() => retrySection('global')}
                />
              </section>

              <section
                id="jot-tab-context"
                role="tabpanel"
                aria-labelledby="jot-tab-button-context"
                hidden={activeTab !== 'context'}
              >
                <div className="jot-section-heading">
                  <h3>Context</h3>
                </div>
                <dl className="jot-context-list">
                  {contextRows.map(row => (
                    <div key={row.label} className="jot-context-row">
                      <dt>{row.label}</dt>
                      <dd>{row.value || 'Not available'}</dd>
                    </div>
                  ))}
                </dl>
                <div className="jot-actions" aria-label="Context quick actions">
                  <button type="button" onClick={() => copyValue('slug', bootstrap.slug)} disabled={!bootstrap.slug}>
                    <Copy aria-hidden="true" size={16} />
                    Copy slug
                  </button>
                  <button type="button" onClick={() => copyValue('deploy ID', bootstrap.deployId)} disabled={!bootstrap.deployId}>
                    <Copy aria-hidden="true" size={16} />
                    Copy deploy ID
                  </button>
                  <button type="button" onClick={() => copyValue('URL', bootstrap.url)}>
                    <Copy aria-hidden="true" size={16} />
                    Copy URL
                  </button>
                  <button type="button" onClick={() => selectTab('history')} disabled={!bootstrap.slug}>
                    <ListRestart aria-hidden="true" size={16} />
                    Slug history
                  </button>
                </div>
              </section>
            </main>
          </div>
          {toast ? (
            <div className="jot-toast" role="status" aria-live="polite">
              <Check aria-hidden="true" size={16} />
              {toast}
            </div>
          ) : null}
        </div>
      ) : null}
    </>
  );
}

function DeploySection({
  title,
  state,
  emptyCopy,
  onRetry
}: {
  title: string;
  state: SectionState;
  emptyCopy: string;
  onRetry: () => void;
}) {
  return (
    <div>
      <div className="jot-section-heading">
        <h3>{title}</h3>
        {state.status === 'loading' ? (
          <span className="jot-loading">
            <Loader2 aria-hidden="true" size={15} />
            Loading
          </span>
        ) : null}
      </div>

      {state.status === 'error' ? (
        <div className="jot-inline-error" role="alert">
          <p>{state.error || 'This section could not be loaded.'}</p>
          <button type="button" onClick={onRetry}>
            <RotateCw aria-hidden="true" size={15} />
            Retry
          </button>
        </div>
      ) : null}

      {state.status !== 'loading' && state.data.length === 0 ? <p className="jot-empty">{emptyCopy}</p> : null}

      {state.data.length > 0 ? (
        <ol className="jot-deploy-list">
          {state.data.map(item => (
            <li key={item.id}>
              <DeployItem item={item} />
            </li>
          ))}
        </ol>
      ) : null}
    </div>
  );
}

function DeployItem({ item }: { item: DeployManifest }) {
  const title = item.title?.trim() || item.slug || item.id;
  const href = deployHref(item);
  return (
    <article className="jot-deploy-item">
      <div>
        <h4>{title}</h4>
        <p>{item.summary || item.id}</p>
      </div>
      <div className="jot-deploy-meta">
        <span>
          <Clock3 aria-hidden="true" size={14} />
          {formatDateTime(item.created_at)}
        </span>
        <span>
          <UserRound aria-hidden="true" size={14} />
          {item.created_by || 'Unknown'}
        </span>
      </div>
      {item.tags && item.tags.length > 0 ? (
        <ul className="jot-tag-list" aria-label={`${title} tags`}>
          {item.tags.map(tag => (
            <li key={tag}>{tag}</li>
          ))}
        </ul>
      ) : null}
      <a className="jot-open-link" href={href}>
        <ExternalLink aria-hidden="true" size={15} />
        Open
      </a>
    </article>
  );
}

function deployHref(item: DeployManifest) {
  const ref = item.id || item.slug;
  return ref ? `/${encodeURIComponent(ref)}/` : '#';
}

function initialTab(slug: string | null): OverlayTab {
  try {
    const stored = window.localStorage.getItem(LAST_TAB_KEY);
    if (stored === 'history' || stored === 'activity' || stored === 'global' || stored === 'context') {
      if (stored !== 'history' || slug) {
        return stored;
      }
    }
  } catch {
    // localStorage can be unavailable in restricted browser contexts.
  }
  return slug ? 'history' : 'context';
}

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError';
}

function isTypingTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tagName = target.tagName.toLowerCase();
  return tagName === 'input' || tagName === 'textarea' || tagName === 'select' || target.isContentEditable;
}

function focusableElements(root: HTMLElement) {
  return Array.from(
    root.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
    )
  ).filter(el => !hasHiddenAncestor(el, root));
}

function hasHiddenAncestor(el: HTMLElement, root: HTMLElement) {
  let current: HTMLElement | null = el;
  while (current && current !== root) {
    if (current.hasAttribute('hidden') || current.getAttribute('aria-hidden') === 'true') {
      return true;
    }
    current = current.parentElement;
  }
  return false;
}

function trapFocus(event: React.KeyboardEvent<HTMLDivElement>, panel: HTMLDivElement | null) {
  if (!panel) {
    return;
  }
  const focusable = focusableElements(panel);
  if (focusable.length === 0) {
    event.preventDefault();
    panel.focus();
    return;
  }
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  const current = event.target instanceof HTMLElement ? event.target : null;
  if (event.shiftKey && current === first) {
    event.preventDefault();
    last.focus();
    return;
  }
  if (!event.shiftKey && current === last) {
    event.preventDefault();
    first.focus();
  }
}

function formatDateTime(value: string | null | undefined) {
  if (!value) {
    return null;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short'
  }).format(date);
}
