import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { PermissionGuard } from '@/core/PermissionGuard';
import { useDocMeta } from '@/core/MetaProvider';
import { useDocument } from '@/core/useDocument';
import type { RecordData } from '@/types';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription, EmptyContent } from '@/components/ui/empty';
import { SearchIcon, PlusIcon, AlertCircleIcon, InboxIcon } from 'lucide-react';

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
    return <p className="text-sm text-muted-foreground">Unknown Document: {doctype}</p>;
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-foreground">{doctype}</h1>
        <PermissionGuard doctype={doctype} action="can_create" fallback={null}>
          <Button render={<Link to={`/doc/${doctype}/new`} />}>
            <PlusIcon data-icon="inline-start" />
            New {doctype}
          </Button>
        </PermissionGuard>
      </div>

      <div className="relative max-w-sm">
        <SearchIcon className="absolute start-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
        <Input
          type="search"
          placeholder="Search..."
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setOffset(0);
          }}
          className="ps-8"
        />
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertCircleIcon data-icon="inline-start" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {loading && (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                {columns.map((c) => (
                  <TableHead key={c.db_column}>{c.label}</TableHead>
                ))}
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {Array.from({ length: 5 }).map((_, i) => (
                <TableRow key={i}>
                  {columns.map((c) => (
                    <TableCell key={c.db_column}>
                      <Skeleton className="h-4 w-full" />
                    </TableCell>
                  ))}
                  <TableCell />
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {!loading && (
        <>
          {rows.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <InboxIcon />
                </EmptyMedia>
                <EmptyTitle>No records</EmptyTitle>
                <EmptyDescription>
                  {q ? 'No results match your search.' : 'No records found.'}
                </EmptyDescription>
              </EmptyHeader>
              {q && (
                <EmptyContent>
                  <Button variant="outline" onClick={() => setQ('')}>Clear search</Button>
                </EmptyContent>
              )}
            </Empty>
          ) : (
            <div className="rounded-lg border">
              <Table>
                <TableHeader>
                  <TableRow>
                    {columns.map((c) => (
                      <TableHead key={c.db_column}>{c.label}</TableHead>
                    ))}
                    <TableHead className="w-[1%]" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((row) => (
                    <TableRow key={String(row.id)}>
                      {columns.map((c) => (
                        <TableCell key={c.db_column}>
                          <span className="truncate">{String(row[c.db_column] ?? '')}</span>
                        </TableCell>
                      ))}
                      <TableCell>
                        <Button variant="ghost" size="sm" render={<Link to={`/doc/${doctype}/${row.id}`} />}>View</Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}

          {total > 0 && (
            <div className="flex items-center justify-between text-sm text-muted-foreground">
              <span>
                {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
              </span>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset === 0}
                  onClick={() => {
                    const newOffset = Math.max(0, offset - PAGE_SIZE);
                    setOffset(newOffset);
                    load(q, newOffset);
                  }}
                >
                  Previous
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset + PAGE_SIZE >= total}
                  onClick={() => {
                    const newOffset = offset + PAGE_SIZE;
                    setOffset(newOffset);
                    load(q, newOffset);
                  }}
                >
                  Next
                </Button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
