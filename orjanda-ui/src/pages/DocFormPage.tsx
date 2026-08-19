import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { FieldRenderer } from '@/components/fields';
import { useDocMeta } from '@/core/MetaProvider';
import { buildPayload, useDocument } from '@/core/useDocument';
import { Card, CardContent } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Skeleton } from '@/components/ui/skeleton';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Spinner } from '@/components/ui/spinner';
import { AlertCircleIcon } from 'lucide-react';

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
    return <p className="text-sm text-muted-foreground">Unknown Document: {doctype}</p>;
  }

  const fields = meta.fields.filter((f) => !f.hidden);

  return (
    <div className="mx-auto max-w-2xl flex flex-col gap-4">
      <h1 className="text-2xl font-semibold text-foreground">
        {isEdit ? `Edit ${doctype}` : `New ${doctype}`}
      </h1>

      {error && (
        <Alert variant="destructive">
          <AlertCircleIcon data-icon="inline-start" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {loading && (
        <Card>
          <CardContent>
            <FieldGroup>
              {Array.from({ length: 4 }).map((_, i) => (
                <Field key={i}>
                  <Skeleton className="h-4 w-20" />
                  <Skeleton className="h-9 w-full" />
                </Field>
              ))}
            </FieldGroup>
          </CardContent>
        </Card>
      )}

      {!loading && (
        <Card>
          <CardContent>
            <form onSubmit={onSubmit}>
              <FieldGroup>
                {fields.map((f) => {
                  const readOnly = f.read_only;
                  return (
                    <Field key={f.db_column}>
                      <FieldLabel>
                        {f.label}
                        {f.required && <span className="text-destructive ms-1">*</span>}
                      </FieldLabel>
                      <FieldRenderer
                        meta={f}
                        docType={doctype}
                        value={values[f.db_column]}
                        onChange={(v) => setValue(f.db_column, v)}
                        disabled={readOnly}
                      />
                    </Field>
                  );
                })}

                <div className="flex gap-3 pt-2">
                  <Button type="submit" disabled={saving}>
                    {saving && <Spinner data-icon="inline-start" />}
                    {saving ? 'Saving...' : 'Save'}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => navigate(`/doc/${doctype}`)}
                  >
                    Cancel
                  </Button>
                </div>
              </FieldGroup>
            </form>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
