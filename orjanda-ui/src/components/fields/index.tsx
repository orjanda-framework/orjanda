// Field renderer components and the default registrations. The Admin UI is
// metadata-driven (PRD §17.3): every renderer is resolved through the
// ComponentRegistry so Applications can override defaults per type, per
// DocType, or per field (PRD §18.2).

import type { ReactNode } from 'react';
import { ComponentRegistry } from '../../registry';
import type { FieldMeta } from '../../types';

export interface FieldRendererProps {
  meta: FieldMeta;
  docType: string;
  value: unknown;
  onChange: (value: unknown) => void;
  disabled?: boolean;
}

function inputClass(disabled?: boolean): string {
  return [
    'w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500',
    disabled ? 'bg-slate-100 text-slate-500' : 'bg-white',
  ].join(' ');
}

function TextField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <input
      type="text"
      className={inputClass(disabled)}
      value={(value as string) ?? ''}
      disabled={disabled}
      data-field={meta.db_column}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function LongTextField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <textarea
      className={inputClass(disabled)}
      rows={4}
      value={(value as string) ?? ''}
      disabled={disabled}
      data-field={meta.db_column}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function NumberField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <input
      type="number"
      step={meta.type === 'currency' ? '0.01' : undefined}
      className={inputClass(disabled)}
      value={value == null ? '' : String(value)}
      disabled={disabled}
      data-field={meta.db_column}
      onChange={(e) => {
        const raw = e.target.value;
        if (raw === '') {
          onChange(null);
          return;
        }
        onChange(meta.type === 'int' || meta.type === 'int64' ? parseInt(raw, 10) : parseFloat(raw));
      }}
    />
  );
}

function BoolField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <input
        type="checkbox"
        className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
        checked={Boolean(value)}
        disabled={disabled}
        data-field={meta.db_column}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className={disabled ? 'text-slate-500' : 'text-slate-700'}>{meta.label}</span>
    </label>
  );
}

function DateField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <input
      type={meta.type === 'datetime' ? 'datetime-local' : 'date'}
      className={inputClass(disabled)}
      value={value != null ? String(value).slice(0, meta.type === 'datetime' ? 16 : 10) : ''}
      disabled={disabled}
      data-field={meta.db_column}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function SelectField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <select
      className={inputClass(disabled)}
      value={(value as string) ?? ''}
      disabled={disabled}
      data-field={meta.db_column}
      onChange={(e) => onChange(e.target.value)}
    >
      <option value="">—</option>
      {(meta.options ?? []).map((opt) => (
        <option key={opt} value={opt}>
          {opt}
        </option>
      ))}
    </select>
  );
}

function LinkField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <input
      type="text"
      className={inputClass(disabled)}
      value={(value as string) ?? ''}
      disabled={disabled}
      placeholder={`${meta.link ?? 'target'} reference (ID)`}
      data-field={meta.db_column}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function JsonField({ meta, value, onChange, disabled }: FieldRendererProps) {
  const text =
    value == null ? '' : typeof value === 'string' ? value : JSON.stringify(value, null, 2);
  return (
    <textarea
      className={inputClass(disabled) + ' font-mono'}
      rows={6}
      value={text}
      disabled={disabled}
      data-field={meta.db_column}
      onChange={(e) => {
        const raw = e.target.value.trim();
        if (raw === '') {
          onChange(null);
          return;
        }
        try {
          onChange(JSON.parse(raw));
        } catch {
          // Keep raw text in the textarea; validation happens on submit.
          onChange(null);
        }
      }}
    />
  );
}

// Built-in default registration (PRD §18.2 "Default" level).
const DEFAULTS: Array<[string, ComponentTypeField]> = [
  ['string', TextField],
  ['int', NumberField],
  ['int64', NumberField],
  ['float64', NumberField],
  ['currency', NumberField],
  ['bool', BoolField],
  ['date', DateField],
  ['datetime', DateField],
  ['text', LongTextField],
  ['richtext', LongTextField],
  ['link', LinkField],
  ['dynamiclink', LinkField],
  ['attachment', TextField],
  ['options', SelectField],
  ['json', JsonField],
];

type ComponentTypeField = (props: FieldRendererProps) => ReactNode;

for (const [type, comp] of DEFAULTS) {
  ComponentRegistry.register(`field:${type}`, comp);
}

/**
 * FieldRenderer renders one field of a Document using the three-level
 * ComponentRegistry resolution (PRD §18.2). Defaults above are the fallback.
 */
export function FieldRenderer(props: FieldRendererProps): ReactNode {
  const resolved = ComponentRegistry.resolveField(props.meta.type, props.docType, props.meta.name);
  const Comp = (resolved ?? TextField) as ComponentTypeField;
  return <Comp {...props} />;
}
