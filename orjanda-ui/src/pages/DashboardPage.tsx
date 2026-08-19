import { Link } from 'react-router-dom';
import { useMeta } from '@/core/MetaProvider';
import { Card, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty';
import { MessageSquareIcon, AlertCircleIcon, DatabaseIcon } from 'lucide-react';

export function DashboardPage() {
  const { summaries, loading, error } = useMeta();

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-foreground">Dashboard</h1>
        <Button render={<Link to="/agent" />}>
          <MessageSquareIcon data-icon="inline-start" />
          Agent Chat
        </Button>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertCircleIcon data-icon="inline-start" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {loading && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i}>
              <CardHeader>
                <Skeleton className="h-5 w-24" />
                <Skeleton className="h-4 w-32" />
              </CardHeader>
            </Card>
          ))}
        </div>
      )}

      {!loading && summaries.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <DatabaseIcon />
            </EmptyMedia>
            <EmptyTitle>No documents</EmptyTitle>
            <EmptyDescription>
              Create a Document to get started.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {!loading && summaries.length > 0 && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {summaries.map((s) => (
            <Link key={s.name} to={`/doc/${s.name}`} className="group block">
              <Card className="transition-colors group-hover:border-primary/50 group-hover:shadow-md">
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <CardTitle className="text-sm">{s.name}</CardTitle>
                    {s.module && <Badge variant="secondary">{s.module}</Badge>}
                  </div>
                  <CardDescription>
                    {s.description ?? `Field: ${s.title_field}`}
                  </CardDescription>
                </CardHeader>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
