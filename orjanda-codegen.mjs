#!/usr/bin/env node
// @orjanda/codegen: the TypeScript SDK generator (PRD §22.2, TAD §6.3).
//
// Reads the TAD §6.3 step-1 payload (a JSON array of DocMetaJSON records, the
// same shape as GET /api/v1/meta/{doctype}) and emits:
//
//   <out>/types.ts     - one TS interface per Document, fields typed from the
//                        single field-type mapping table (TAD §10.2), which is
//                        the SAME table the agent tool JSON Schemas use, so the
//                        generated client and the agent tools agree
//                        field-for-field on every Document.
//   <out>/documents.ts - a typed documents.{DocType} client exposing the REST
//                        surface (PRD §14.2), gated by the identity-independent
//                        permissions summary.
//
// Usage: node orjanda-codegen.mjs --input schema.json --out src/generated

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';

const args = process.argv.slice(2);
function arg(name, fallback) {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback;
}

const inputPath = arg('--input', 'orjanda-ui/src/generated/schema.json');
const outDir = arg('--out', 'orjanda-ui/src/generated');

/** Field type -> TypeScript, mirroring fieldJSONType in agent/tools/tools.go
 *  (TAD §10.2). Dates keep their format so tool schemas and the SDK agree. */
function tsType(field) {
  switch (field.type) {
    case 'int':
    case 'int64':
    case 'float64':
    case 'currency':
      return 'number';
    case 'bool':
      return 'boolean';
    case 'json':
      return 'Record<string, unknown>';
    case 'options':
      return field.options?.length ? field.options.map((o) => JSON.stringify(o)).join(' | ') : 'string';
    default:
      return 'string';
  }
}

/** PascalCase for interface names: leave_requests -> LeaveRequest. */
function pascal(name) {
  return name
    .split('_')
    .filter(Boolean)
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join('');
}

/** Column -> TS property name (camelCase), matching REST write keys. */
function propName(field) {
  const cols = field.db_column.split('_');
  return cols[0] + cols.slice(1).map((c) => c.charAt(0).toUpperCase() + c.slice(1)).join('');
}

// Auto columns present on every row (TAD §1.4 BaseColumns).
const baseColumns = ['id', 'name', 'owner', 'created_at', 'updated_at', 'modified_by', 'doc_status', 'deleted'];

function indent(lines, pad = '  ') {
  return lines.map((l) => (l ? pad + l : l)).join('\n');
}

function renderInterface(name, fields) {
  const lines = fields.map((f) => {
    const type = tsType(f);
    const jsdoc = [];
    if (f.label) jsdoc.push(f.label);
    if (f.link) jsdoc.push(`Reference to a ${f.link} document (TAD §10.2).`);
    if (f.required) jsdoc.push('Required.');
    if (f.read_only) jsdoc.push('Read-only.');
    const doc = jsdoc.length ? `  /** ${jsdoc.join(' ')} */\n` : '';
    return `${doc}  ${f.prop}${f.required ? '' : '?'}: ${type};`;
  });
  return `export interface ${name} {\n${lines.join('\n')}\n}\n`;
}

const docs = JSON.parse(readFileSync(inputPath, 'utf8'));
docs.sort((a, b) => a.name.localeCompare(b.name));

const allInterfaces = [];
const clientEntries = [];
const typeImports = new Set();

for (const doc of docs) {
  const iface = pascal(doc.name);
  const createIface = `Create${iface}`;
  typeImports.add(iface);
  typeImports.add(createIface);

  const docFields = (doc.fields ?? []).map((f) => ({ ...f, prop: propName(f) }));
  // A struct field may shadow a BaseDocument column (same db_column); the last
  // declaration wins, so the interface carries the struct's requiredness.
  const byColumn = new Map();
  for (const f of docFields) byColumn.set(f.db_column, f);
  const uniqueFields = [...byColumn.values()];

  const baseProps = baseColumns
    .filter((c) => !byColumn.has(c))
    .map((c) => {
      const prop = c === 'id' ? 'id' : propName({ db_column: c });
      return { prop, label: c, type: c === 'deleted' ? 'boolean' : 'string', read_only: true, required: false };
    });
  const fields = [...uniqueFields, ...baseProps];

  allInterfaces.push(renderInterface(iface, fields, doc));

  // Create payload: writable fields only (TAD §6.1 read_only is excluded);
  // auto columns (id, owner, timestamps, ...) are server-managed (TAD §1.4).
  const isAuto = new Set(baseColumns);
  const writable = uniqueFields
    .filter((f) => !f.read_only && !isAuto.has(f.db_column))
    .map((f) => {
      const type = tsType(f);
      return `${f.prop}${f.required ? '' : '?'}: ${type};`;
    });
  allInterfaces.push(`export interface ${createIface} {\n${writable.map((w) => `  ${w}`).join('\n')}\n}\n`);

  // Child table embeds (TAD §6.3 ChildTableJSON).
  const childDocs = [];
  for (const ct of doc.child_tables ?? []) {
    const childIface = ct.type_name ?? pascal(ct.doc_type);
    const ctFields = (ct.fields ?? []).map((f) => ({ ...f, prop: propName(f) }));
    if (!allInterfaces.some((s) => s.startsWith(`export interface ${childIface} `))) {
      allInterfaces.push(renderInterface(childIface, ctFields));
    }
  }

  const { can_read, can_write, can_create, can_delete } = doc.permissions ?? {};
  const docType = doc.name;
  const endpoint = `/api/v1/document/${docType}`;
  const methods = [];
  if (can_read) {
    methods.push(`list: async (opts?: { q?: string; limit?: number; offset?: number }) => { const qs = new URLSearchParams(); if (opts?.q) qs.set('q', opts.q); if (opts?.limit != null) qs.set('limit', String(opts.limit)); if (opts?.offset != null) qs.set('offset', String(opts.offset)); const s = qs.toString(); return api.get<{ data: ${iface}[]; meta: { total_count: number; limit: number; offset: number } }>('${endpoint}' + (s ? '?' + s : '')); },`);
    methods.push(`get: (id: string) => api.get<${iface}>('${endpoint}' + '/' + id),`);
  }
  if (can_create) methods.push(`create: (payload: ${createIface}) => api.post<${iface}>('${endpoint}', payload),`);
  if (can_write) methods.push(`update: (id: string, payload: Partial<${createIface}>) => api.patch<${iface}>('${endpoint}' + '/' + id, payload),`);
  if (can_delete) methods.push(`remove: (id: string) => api.delete<void>('${endpoint}' + '/' + id),`);

  clientEntries.push(
    `  ${doc.name}: {\n${methods.map((m) => `    ${m}`).join('\n')}\n  },`,
  );
}

mkdirSync(outDir, { recursive: true });

writeFileSync(
  join(outDir, 'types.ts'),
  `// Generated by @orjanda/codegen (TAD §6.3). Do not edit by hand — run the
// codegen pass (npm run codegen) after changing a Document's schema.
// Field types mirror the agent tool JSON Schema mapping one-for-one (TAD §10.2).

${allInterfaces.join('\n')}`,
);

writeFileSync(
  join(outDir, 'documents.ts'),
  `// Generated by @orjanda/codegen (TAD §6.3). Do not edit by hand.
// Typed documents client: the same REST surface the UI and agent tools use
// (PRD §14.2, §22.2). Methods are gated by the identity-independent
// permissions summary; the server enforces real per-request checks (PRD §25.1).

import { api } from '../api';
import type { ${[...typeImports].join(', ')} } from './types';

export const documents = {
${clientEntries.join('\n')}
};

export type Documents = typeof documents;
`,
);

console.log(`@orjanda/codegen: ${docs.length} Documents -> ${join(outDir, 'types.ts')}, ${join(outDir, 'documents.ts')}`);
