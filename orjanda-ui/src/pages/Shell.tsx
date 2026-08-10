// Shell: the admin layout with a metadata-driven sidebar (PRD §17.2). The
// sidebar groups Documents by module (from /api/v1/meta) and renders the
// custom ui.Page registrations under their menu group (from /api/v1/meta/pages).

import { useState } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuth } from '../core/AuthProvider';
import { useMeta } from '../core/MetaProvider';

export function Shell() {
  const { identity, logout } = useAuth();
  const { summaries, pages, loading } = useMeta();
  const navigate = useNavigate();
  const [open, setOpen] = useState(true);

  const groups = new Map<string, typeof summaries>();
  for (const s of summaries) {
    const key = s.module ?? 'Other';
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(s);
  }

  function signOut() {
    logout();
    navigate('/login');
  }

  return (
    <div className="flex min-h-screen bg-slate-100">
      {open && (
        <aside className="w-64 shrink-0 border-r border-slate-200 bg-white">
          <div className="border-b border-slate-200 px-4 py-4">
            <div className="text-lg font-semibold text-slate-900">Orjanda</div>
            {identity?.email && <div className="truncate text-xs text-slate-500">{identity.email}</div>}
          </div>
          <nav className="space-y-1 p-3">
            <NavLink to="/" className={sidebarClass}>
              Dashboard
            </NavLink>
            <NavLink to="/agent" className={sidebarClass}>
              Agent Chat
            </NavLink>

            {loading ? (
              <p className="px-3 py-2 text-xs text-slate-400">Loading…</p>
            ) : (
              <>
                {[...groups.entries()].map(([group, docs]) => (
                  <div key={group}>
                    <div className="px-3 pb-1 pt-3 text-xs font-semibold uppercase tracking-wide text-slate-400">
                      {group}
                    </div>
                    {docs.map((d) => (
                      <NavLink key={d.name} to={`/doc/${d.name}`} className={sidebarClass}>
                        {d.name}
                      </NavLink>
                    ))}
                  </div>
                ))}
                {pages.length > 0 && (
                  <div>
                    <div className="px-3 pb-1 pt-3 text-xs font-semibold uppercase tracking-wide text-slate-400">
                      Custom
                    </div>
                    {pages.map((p) => (
                      <NavLink key={p.path} to={p.path} className={sidebarClass}>
                        {p.title}
                      </NavLink>
                    ))}
                  </div>
                )}
              </>
            )}
          </nav>
        </aside>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center justify-between border-b border-slate-200 bg-white px-4 py-3">
          <button
            onClick={() => setOpen((o) => !o)}
            className="rounded border border-slate-300 px-2 py-1 text-sm text-slate-600"
          >
            {open ? '◀' : '▶'}
          </button>
          <div className="flex items-center gap-3">
            <span className="text-sm text-slate-600">{identity?.name ?? identity?.email ?? 'User'}</span>
            <button onClick={signOut} className="text-sm text-slate-400 hover:text-slate-600">
              Sign out
            </button>
          </div>
        </header>
        <main className="flex-1 p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

function sidebarClass({ isActive }: { isActive: boolean }): string {
  return [
    'block rounded-md px-3 py-1.5 text-sm',
    isActive ? 'bg-indigo-50 text-indigo-700' : 'text-slate-700 hover:bg-slate-50',
  ].join(' ');
}
