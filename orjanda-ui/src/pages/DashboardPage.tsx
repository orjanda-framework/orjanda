// DashboardPage: entry point listing the Documents the identity may read plus
// a link into the Agent Chat (PRD §17.2, §23).

import { Link } from 'react-router-dom';
import { useMeta } from '../core/MetaProvider';

export function DashboardPage() {
  const { summaries, loading, error } = useMeta();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-slate-900">Dashboard</h1>
        <Link
          to="/agent"
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        >
          Open Agent Chat
        </Link>
      </div>

      {error && <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>}
      {loading && <p className="text-sm text-slate-500">Loading…</p>}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {!loading &&
          summaries.map((s) => (
            <Link
              key={s.name}
              to={`/doc/${s.name}`}
              className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm hover:border-indigo-300 hover:shadow"
            >
              <div className="text-sm font-medium text-slate-900">{s.name}</div>
              <div className="text-sm text-slate-500">{s.description ?? s.title_field}</div>
            </Link>
          ))}
      </div>
    </div>
  );
}
