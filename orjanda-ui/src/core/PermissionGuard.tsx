// PermissionGuard renders children only when the current identity's DocMeta
// permissions grant the requested action. Permissions are checked on the
// server (PRD §25.1 — one permission path); this guard is a UI affordance,
// not a security boundary.

import type { ReactNode } from 'react';
import { useDocMeta } from './MetaProvider';

export type GuardAction = 'can_read' | 'can_write' | 'can_create' | 'can_delete';

interface Props {
  doctype: string;
  action?: GuardAction;
  children: ReactNode;
  fallback?: ReactNode;
}

export function PermissionGuard({ doctype, action = 'can_read', children, fallback = null }: Props) {
  const meta = useDocMeta(doctype);
  if (!meta) return fallback;
  if (action === 'can_write' && !meta.permissions.can_write) return fallback;
  if (action === 'can_create' && !meta.permissions.can_create) return fallback;
  if (action === 'can_delete' && !meta.permissions.can_delete) return fallback;
  if (!meta.permissions.can_read) return fallback;
  return children;
}
