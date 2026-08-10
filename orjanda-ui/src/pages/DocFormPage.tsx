// DocFormPage: auto-generated create/edit form for any Document from its
// metadata (PRD §17.3). Field editors resolve through the ComponentRegistry;
// read_only/hidden fields are excluded from the payload (TAD §6.1).

import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { FieldRenderer } from '../components/fields';
import { useDocMeta } from '../core/MetaProvider';
import { buildPayload, useDocument } from '../core/useDocument';

export function DocFormPage() {
  const { doctype = '', id } = useParams();
  const isEdit = Boolean(id);
  const navigate = useNavigate();
  const meta = useDocMeta(doctype);
  const api = useDocument(doctype, meta);
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(isEdit);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!meta || !api) return;
    if (isEdit && id) {
      setLoading(true);
      api
        .read(id)
        .then((row) => setValues(row))
        .catch((err: Error) => setError(err.message))
        .finally(() => setLoading(false));
    } else if (meta) {
      const initial: Record<string, unknown> = {};
      for (const f of meta.fields) {
        if (f.type === 'bool') initial[f.db_column] = false;
        if (f.type === 'options' && f.options?.length) initial[f.db_column] = f.options[0];
      }
      setValues(initial);
    }
  }, [meta, api, isEdit, id]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!api || !meta) return;
    setSaving(true);
    setError(null);
    try {
      const payload = buildPayload(meta, values);
      if (isEdit && id) {
        await api.update(id, payload);
      } else {
        await api.create(payload);
      }
      navigate(`/doc/${doctype}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  const setValue = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  if (!meta) {
    return <p className="text-sm text-slate-500">Unknown Document: {doctype}</p>;
  }

  const fields = meta.fields.filter((f) => !f.hidden);

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <h1 className="text-2xl font-semibold text-slate-900">
        {isEdit ? `Edit ${doctype}` : `New ${doctype}`}
      </h1>
      {error && <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>}
      {loading && <p className="text-sm text-slate-500">Loading…</p>}
      {!loading && (
        <form onSubmit={onSubmit} className="space-y-4 rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
          {fields.map((f) => {
            const readOnly = f.read_only;
            return (
              <div key={f.db_column}>
                <label className="mb-1 block text-sm font-medium text-slate-700">
                  {f.label}
                  {f.required && <span className="ml-1 text-red-500">*</span>}
                </label>
                <FieldRenderer
                  meta={f}
                  docType={doctype}
                  value={values[f.db_column]}
                  onChange={(v) => setValue(f.db_column, v)}
                  disabled={readOnly}
                />
              </div>
            );
          })}
          <div className="flex gap-3 pt-2">
            <button
              type="submit"
              disabled={saving}
              className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
            >
              {saving ? 'Saving…' : 'Save'}
            </button>
            <button
              type="button"
              onClick={() => navigate(`/doc/${doctype}`)}
              className="rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            >
              Cancel
            </button>
          </div>
        </form>
      )}
    </div>
  );
}
