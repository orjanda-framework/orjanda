// ComponentRegistry implements the Admin UI's named component registry
// (PRD §18.2). Keys are namespaced:
//
//   - field:{type}                       → Default renderer for a field type
//   - field:{type}:{DocType}             → Application override for a DocType
//   - field:{type}:{DocType}.{fieldName} → Document-specific override
//
// Resolution order (documented in PRD §18.2):
// Document-specific → Application override → Default.
//
// A field resolver must search all three levels; a re-registered key replaces
// the previous value, so an Application overriding the default `field:Date`
// key shadows it globally.

import type { ComponentType } from 'react';

export type NamedComponent = ComponentType<any>;

const store = new Map<string, NamedComponent>();

export const ComponentRegistry = {
  register(key: string, component: NamedComponent): void {
    store.set(key, component);
  },

  resolve(key: string): NamedComponent | undefined {
    return store.get(key);
  },

  /** Resolve a field renderer with the documented three-level resolution order. */
  resolveField(
    fieldType: string,
    docType: string,
    fieldName: string,
  ): NamedComponent | undefined {
    return (
      store.get(`field:${fieldType}:${docType}.${fieldName}`) ??
      store.get(`field:${fieldType}:${docType}`) ??
      store.get(`field:${fieldType}`)
    );
  },

  keys(): string[] {
    return [...store.keys()];
  },
};
