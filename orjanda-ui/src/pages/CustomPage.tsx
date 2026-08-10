// Custom pages: Applications may register React components for the `component`
// field of a ui.Page (PRD §18). Named entries resolve through a simple
// registry; unregistered components render a placeholder so the page still
// routes.

import type { ComponentType } from 'react';
import { useParams } from 'react-router-dom';

const custom = new Map<string, ComponentType>();

export function registerCustomComponent(name: string, comp: ComponentType): void {
  custom.set(name, comp);
}

export function CustomPage() {
  const params = useParams();
  const componentName = (params as Record<string, string>).component ?? '';
  const Comp = custom.get(componentName);

  if (!Comp) {
    return (
      <div className="rounded-lg border border-dashed border-slate-300 bg-white p-8 text-center">
        <p className="text-sm text-slate-500">
          Custom component <code className="font-mono text-slate-700">{componentName}</code> is
          not registered. Register it via <code className="font-mono text-slate-700">registerCustomComponent</code>.
        </p>
      </div>
    );
  }
  return <Comp />;
}
