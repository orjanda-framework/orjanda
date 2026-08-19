import type { ReactNode } from 'react';
import { ComponentRegistry } from '@/registry';
import type { FieldMeta } from '@/types';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Switch } from '@/components/ui/switch';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';

export interface FieldRendererProps {
  meta: FieldMeta;
  docType: string;
  value: unknown;
  onChange: (value: unknown) => void;
  disabled?: boolean;
}

function TextField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <Input
      type="text"
      value={(value as string) ?? ''}
      disabled={disabled}
      data-field={meta.db_column}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function LongTextField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <Textarea
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
    <Input
      type="number"
      step={meta.type === 'currency' ? '0.01' : undefined}
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
    <Switch
      checked={Boolean(value)}
      disabled={disabled}
      data-field={meta.db_column}
      onCheckedChange={(checked) => onChange(checked)}
    />
  );
}

function DateField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <Input
      type={meta.type === 'datetime' ? 'datetime-local' : 'date'}
      value={value != null ? String(value).slice(0, meta.type === 'datetime' ? 16 : 10) : ''}
      disabled={disabled}
      data-field={meta.db_column}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function SelectField({ meta, value, onChange, disabled }: FieldRendererProps) {
  const options = meta.options ?? [];
  return (
    <Select
      value={(value as string) ?? ''}
      disabled={disabled}
      onValueChange={(val) => onChange(val)}
    >
      <SelectTrigger className="w-full">
        <SelectValue placeholder="Select..." />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {options.map((opt) => (
            <SelectItem key={opt} value={opt}>
              {opt}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function LinkField({ meta, value, onChange, disabled }: FieldRendererProps) {
  return (
    <Input
      type="text"
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
    <Textarea
      rows={6}
      value={text}
      disabled={disabled}
      data-field={meta.db_column}
      className="font-mono"
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

type ComponentTypeField = (props: FieldRendererProps) => ReactNode;

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

for (const [type, comp] of DEFAULTS) {
  ComponentRegistry.register(`field:${type}`, comp);
}

export function FieldRenderer(props: FieldRendererProps): ReactNode {
  const { meta, docType } = props;
  const specific =
    ComponentRegistry.resolve(`field:${meta.type}:${docType}.${meta.name}`) ??
    ComponentRegistry.resolve(`field:${meta.type}:${docType}`);
  if (specific) return (specific as ComponentTypeField)(props);

  const defaultComp =
    (meta.options && meta.options.length > 0 ? SelectField : undefined) ??
    ComponentRegistry.resolve(`field:${meta.type}`);
  const Comp = (defaultComp ?? TextField) as ComponentTypeField;
  return <Comp {...props} />;
}
