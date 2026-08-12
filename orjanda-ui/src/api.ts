// Thin fetch layer over the Orjanda REST + metadata API (PRD §14.2, TAD §6.1).
// The access token is attached from the AuthProvider's token slot; errors are
// unwrapped from the {data, meta, error} envelope.

import type { Envelope } from './types';

export class ApiError extends Error {
  code: string;
  status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
  }
}

export function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  return withToken<T>(path, init);
}

async function fetchEnvelope<T>(path: string, init: RequestInit): Promise<Envelope<T> | null> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((init.headers as Record<string, string>) ?? {}),
  };
  const token = localStorage.getItem('orjanda.access_token');
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(path, { ...init, headers });
  let body: Envelope<T> | null = null;
  try {
    body = (await res.json()) as Envelope<T>;
  } catch {
    body = null;
  }

  if (!res.ok) {
    const code = body?.error?.code ?? 'HTTP_ERROR';
    const message = body?.error?.message ?? `HTTP ${res.status}`;
    throw new ApiError(res.status, code, message);
  }
  return body;
}

async function withToken<T>(path: string, init: RequestInit): Promise<T> {
  const body = await fetchEnvelope<T>(path, init);
  return (body?.data ?? body) as T;
}

export const api = {
  get<T>(path: string): Promise<T> {
    return apiFetch<T>(path);
  },
  post<T>(path: string, body: unknown): Promise<T> {
    return apiFetch<T>(path, { method: 'POST', body: JSON.stringify(body) });
  },
  patch<T>(path: string, body: unknown): Promise<T> {
    return apiFetch<T>(path, { method: 'PATCH', body: JSON.stringify(body) });
  },
  delete<T>(path: string): Promise<T> {
    return apiFetch<T>(path, { method: 'DELETE' });
  },
  /** Fetch the full {data, meta, error} envelope for endpoints whose metadata
   *  (e.g. list pagination) must be read alongside the payload. */
  getEnvelope<T>(path: string): Promise<Envelope<T> | null> {
    return fetchEnvelope<T>(path, {});
  },
};
