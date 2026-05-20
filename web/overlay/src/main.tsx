import React from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { readBootstrap, overlayStylesheetHref } from './bootstrap';
import './styles.css';

const isolatedEvents = [
  'click',
  'dblclick',
  'keydown',
  'keyup',
  'mousedown',
  'mouseup',
  'pointerdown',
  'pointerup',
  'touchstart',
  'touchend',
  'wheel'
];

function mountOverlay() {
  const host = document.getElementById('jot-overlay-root');
  if (!host || host.shadowRoot) {
    return;
  }

  const shadow = host.attachShadow({ mode: 'open' });
  const stylesheet = overlayStylesheetHref();
  if (stylesheet) {
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = stylesheet;
    shadow.append(link);
  }

  const appRoot = document.createElement('div');
  appRoot.className = 'jot-overlay-host';
  shadow.append(appRoot);

  for (const type of isolatedEvents) {
    shadow.addEventListener(type, event => event.stopPropagation());
  }

  createRoot(appRoot).render(
    <React.StrictMode>
      <App bootstrap={readBootstrap()} />
    </React.StrictMode>
  );
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', mountOverlay, { once: true });
} else {
  mountOverlay();
}
