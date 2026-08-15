// Wire types shared across the Admin UI (TAD §6.1, §6.2, PRD §14.2).

export interface FieldMeta {
  name: string;
  db_column: string;
  type: string;
  label: string;
  required: boolean;
  options?: string[];
  link?: string;
  hidden?: boolean;
  read_only?: boolean;
}

export interface PermissionsMeta {
  can_read: boolean;
  can_write: boolean;
  can_create: boolean;
  can_delete: boolean;
}

export interface DocMeta {
  name: string;
  title_field: string;
  searchable: boolean;
  submittable: boolean;
  icon?: string;
  description?: string;
  fields: FieldMeta[];
  permissions: PermissionsMeta;
}

export interface DocTypeSummary {
  name: string;
  module?: string;
  title_field: string;
  searchable: boolean;
  submittable: boolean;
  icon?: string;
  description?: string;
}

export interface UiPage {
  path: string;
  title: string;
  component?: string;
  icon?: string;
  menu?: string;
}

export type RecordData = Record<string, unknown>;

export interface MetaDetails {
  total_count: number;
  limit: number;
  offset: number;
}

export interface ListResponse {
  data: RecordData[];
  meta: MetaDetails;
}

export interface Envelope<T> {
  data: T;
  meta?: MetaDetails;
  error?: { code: string; message: string } | null;
}

export interface LoginResponse {
  access_token: string;
  token_type: string;
  refresh_token: string;
  expires_in: number;
}

// Agent Chat WebSocket contract (TAD §6.2, §12.3).
export type AgentServerEvent =
  | { type: 'token'; content?: string; sender?: 'user' | 'assistant' }
  | { type: 'tool_start'; tool?: string }
  | { type: 'tool_end'; tool?: string; success?: boolean; content?: string }
  | {
      type: 'approval_required';
      action_id: string;
      details: {
        doctype: string;
        action: string;
        payload?: Record<string, unknown>;
        policy_reason: string;
      };
    };

export interface ApprovalResponseMessage {
  type: 'approval_response';
  action_id: string;
  approved: boolean;
  payload?: Record<string, unknown>;
}
