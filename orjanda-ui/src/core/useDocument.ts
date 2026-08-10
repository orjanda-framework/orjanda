// useDocument: CRUD client for a Document, mirroring the REST API surface
// (PRD §14.2): list, read, create, update, delete. Records are keyed by
// db_column; the payload builder translates form state into wire keys.

import { useCallback } from 'react';
import { api } from '../api';
import type { DocMeta, ListResponse, RecordData } from '../types';

export interface ListOptions {
  q?: string;
  limit?: number;
  offset?: number;
}

export interface DocumentApi {
  list: (opts?: ListOptions) => Promise<ListResponse>;
  read: (id: string) => Promise<RecordData>;
  create: (payload: RecordData) => Promise<RecordData>;
  update: (id: string, payload: RecordData) => Promise<RecordData>;
  remove: (id: string) => Promise<void>;
}

export function useDocument(doctype: string, meta: DocMeta | null): DocumentApi | null {
  const list = useCallback(
    async (opts: ListOptions = {}) => {
      const params = new URLSearchParams();
      if (opts.q) params.set('q', opts.q);
      if (opts.limit != null) params.set('limit', String(opts.limit));
      if (opts.offset != null) params.set('offset', String(opts.offset));
      const qs = params.toString();
      return api.get<ListResponse>(`/api/v1/document/${doctype}${qs ? `?${qs}` : ''}`);
    },
    [doctype],
  );

  const read = useCallback(
    async (id: string) => api.get<RecordData>(`/api/v1/document/${doctype}/${id}`),
    [doctype],
  );

  const create = useCallback(
    async (payload: RecordData) =>
      api.post<RecordData>(`/api/v1/document/${doctype}`, payload),
    [doctype],
  );

  const update = useCallback(
    async (id: string, payload: RecordData) =>
      api.patch<RecordData>(`/api/v1/document/${doctype}/${id}`, payload),
    [doctype],
  );

  const remove = useCallback(
    async (id: string) => {
      await api.delete<void>(`/api/v1/document/${doctype}/${id}`);
    },
    [doctype],
  );

  if (!meta) return null;
  return { list, read, create, update, remove };
}

/**
 * Build the wire payload for create/update from form state, mapping form keys
 * to db_column and omitting read_only/system fields (TAD §6.1).
 */
export function buildPayload(meta: DocMeta, values: Record<string, unknown>): RecordData {
  const payload: RecordData = {};
  for (const field of meta.fields) {
    if (field.hidden || field.read_only) continue;
    const key = field.db_column;
    if (values[key] !== undefined && values[key] !== null && values[key] !== '') {
      payload[key] = values[key];
    }
  }
  return payload;
}
