// DocListPage: auto-generated list for any Document from its metadata
// (PRD §17.3). Columns are the non-hidden fields; search maps to the REST
// ?q= parameter; row actions are gated by the DocMeta permissions.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { PermissionGuard } from '../core/PermissionGuard';
import { useDocMeta } from '../core/MetaProvider';
import { useDocument } from '../core/useDocument';
import type { RecordData } from '../types';

const PAGE_SIZE = 25;

export function DocListPage() {
  const { doctype = '' } = useParams();
  const meta = useDocMeta(doctype);
  const api = useDocument(doctype, meta);
  const [rows, setRows] = useState<RecordData[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [q, setQ] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const columns = useMemo(
    () => (meta ? meta.fields.filter((f) => !f.hidden) : []),
    [meta],
  );

  const load = useCallback(
    async (query: string, off: number) => {
      if (!api) return;
      setLoading(true);
      setError(null);
      try {
        const res = await api.list({ q: query || undefined, limit: PAGE_SIZE, offset: off });
        setRows(res.data);
        setTotal(res.meta.total_count);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load');
      } finally {
        setLoading(false);
      }
    },
    [api],
  );

  useEffect(() => {
    load(q, 0);
  }, [load, q]);

  if (!meta) {
    return <p className="text-sm text-slate-500">Unknown Document: {doctype}</p>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-slate-900">{doctype}</h1>
        <PermissionGuard doctype={doctype} action="can_create" fallback={null}>
          <Link
            to={`/doc/${doctype}/new`}
            className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            New {doctype}
          </Link>
        </PermissionGuard>
      </div>

      <input
        type="search"
        placeholder="Search…"
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setOffset(0);
        }}
        className="w-full max-w-sm rounded-md border border-slate-300 px-3 py-2 text-sm"
      />

      {error && <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>}
      {loading && <p className="text-sm text-slate-500">Loading…</p>}

      {!loading && (
        <>
          <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white shadow-sm">
            <table className="min-w-full divide-y divide-slate-200 text-sm">
              <thead className="bg-slate-50">
                <tr>
                  {columns.map((c) => (
                    <th
                      key={c.db_column}
                      className="px-4 py-2 text-left font-medium text-slate-600"
                    >
                      {c.label}
                    </th>
                  ))}
                  <th className="px-4 py-2" />
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {rows.map((row) => (
                  <tr key={String(row.id)} className="hover:bg-slate-50">
                    {columns.map((c) => (
                      <td key={c.db_column} className="px-4 py-2 text-slate-800">
                        {String(row[c.db_column] ?? '')}
                      </td>
                    ))}
                    <td className="px-4 py-2 text-right">
                      <Link
                        to={`/doc/${doctype}/${row.id}`}
                        className="text-indigo-600 hover:text-indigo-800"
                      >
                        View
                      </Link>
                    </td>
                  </tr>
                ))}
                {rows.length === 0 && (
                  <tr>
                    <td colSpan={columns.length + 1} className="px-4 py-6 text-center text-slate-400">
                      No records
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          <div className="flex items-center gap-4 text-sm text-slate-600">
            <button
              disabled={offset === 0}
              onClick={() => {
                setOffset(Math.max(0, offset - PAGE_SIZE));
                load(q, Math.max(0, offset - PAGE_SIZE));
              }}
              className="rounded border border-slate-300 px-3 py-1 disabled:opacity-40"
            >
              Prev
            </button>
            <span>
              {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
            </span>
            <button
              disabled={offset + PAGE_SIZE >= total}
              onClick={() => {
                setOffset(offset + PAGE_SIZE);
                load(q, offset + PAGE_SIZE);
              }}
              className="rounded border border-slate-300 px-3 py-1 disabled:opacity-40"
            >
              Next
            </button>
          </div>
        </>
      )}
    </div>
  );
}
