export type OverlayTab = 'history' | 'activity' | 'global' | 'context';

export type BootstrapPayload = {
  slug: string | null;
  deployId: string | null;
  title: string | null;
  url: string;
  createdBy: string | null;
  createdAt: string | null;
  tags: string[];
  apiBase: string;
};

export type ManifestFile = {
  sha256: string;
  size: number;
  content_type: string;
};

export type DeployManifest = {
  schema_version: number;
  id: string;
  slug: string;
  created_at: string;
  created_by: string;
  actor?: string;
  title?: string;
  summary?: string;
  tags?: string[];
  spa_fallback?: string;
  files: Record<string, ManifestFile>;
};

export type DeployListResponse = {
  deploys: DeployManifest[];
};

export type SectionKey = 'history' | 'activity' | 'global';

export type SectionState = {
  status: 'idle' | 'loading' | 'ready' | 'error';
  data: DeployManifest[];
  error: string | null;
};
