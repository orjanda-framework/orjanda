// MetaProvider loads the Registry metadata the Admin UI renders from
// (TAD §6.1): doc summaries for the sidebar and the custom page registry
// (ui.Page). Per-doc field metadata is loaded lazily by useDocMeta and cached
// at module level. Everything stays metadata-driven (PRD §17.3).

import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { api } from '../api';
import type { DocMeta, DocTypeSummary, UiPage } from '../types';

export interface MetaContextValue {
  loading: boolean;
  error: string | null;
  summaries: DocTypeSummary[];
  pages: UiPage[];
  reload: () => void;
}

const MetaContext = createContext<MetaContextValue | null>(null);

export function MetaProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [summaries, setSummaries] = useState<DocTypeSummary[]>([]);
  const [pages, setPages] = useState<UiPage[]>([]);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    Promise.all([api.get<DocTypeSummary[]>('/api/v1/meta'), api.get<UiPage[]>('/api/v1/meta/pages')])
      .then(([sums, pgs]) => {
        if (cancelled) return;
        setSummaries(sums);
        setPages(pgs);
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [tick]);

  const value = useMemo<MetaContextValue>(
    () => ({
      loading,
      error,
      summaries,
      pages,
      reload: () => setTick((t) => t + 1),
    }),
    [loading, error, summaries, pages],
  );

  return <MetaContext.Provider value={value}>{children}</MetaContext.Provider>;
}

export function useMeta(): MetaContextValue {
  const ctx = useContext(MetaContext);
  if (!ctx) throw new Error('useMeta must be used within MetaProvider');
  return ctx;
}

/** Per-DocType metadata loader with a module-level cache. */
const metaCache = new Map<string, DocMeta>();

export async function loadDocMeta(doctype: string): Promise<DocMeta | null> {
  if (metaCache.has(doctype)) return metaCache.get(doctype) ?? null;
  try {
    const meta = await api.get<DocMeta>(`/api/v1/meta/${doctype}`);
    metaCache.set(doctype, meta);
    return meta;
  } catch {
    return null;
  }
}

export function useDocMeta(doctype: string): DocMeta | null {
  const [meta, setMeta] = useState<DocMeta | null>(() => metaCache.get(doctype) ?? null);

  useEffect(() => {
    if (!doctype) return;
    if (metaCache.has(doctype)) {
      setMeta(metaCache.get(doctype) ?? null);
      return;
    }
    let cancelled = false;
    loadDocMeta(doctype).then((m) => {
      if (!cancelled) setMeta(m);
    });
    return () => {
      cancelled = true;
    };
  }, [doctype]);

  return meta;
}
