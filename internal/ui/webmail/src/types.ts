// Shapes returned by the webmail API (internal/webmail).

export interface Addr {
  name?: string;
  email: string;
}

export type Special = "inbox" | "sent" | "drafts" | "trash" | "junk" | "archive";

export interface Folder {
  name: string;
  special?: Special;
  total: number;
  unread: number;
  size: number;
}

export interface Summary {
  folder: string;
  uid: number;
  from: Addr;
  to: Addr[];
  subject: string;
  date: string;
  snippet: string;
  size: number;
  seen: boolean;
  flagged: boolean;
  answered: boolean;
  forwarded: boolean;
  draft: boolean;
  categories: string[];
  attachments: boolean;
  risky: boolean;
  marketing: boolean;
}

export interface Attachment {
  index: number;
  name: string;
  type: string;
  size: number;
  inline?: boolean;
}

export interface Detail extends Summary {
  cc: Addr[] | null;
  bcc: Addr[] | null;
  reply_to: Addr[] | null;
  message_id: string;
  in_reply_to: string;
  references: string;
  sent: string;
  text: string;
  html: boolean;
  remote: boolean;
  files: Attachment[];
  auth: { check: string; result: string }[];
  junk?: string;
  source?: string;
}

export interface Page {
  total: number;
  messages: Summary[];
}

export interface Signature {
  id: string;
  name: string;
  html: string;
}

export interface Category {
  id: string;
  name: string;
  color: string;
}

export type Field = "from" | "to" | "subject" | "body" | "anywhere" | "attachment" | "marketing";
export type Op = "contains" | "not_contains" | "is" | "starts" | "ends";
export type Kind = "move" | "copy" | "read" | "flag" | "category" | "delete";

export interface Condition {
  field: Field;
  op: Op;
  value: string;
}

export interface Action {
  kind: Kind;
  value: string;
}

export interface Rule {
  id: string;
  name: string;
  on: boolean;
  any: boolean;
  conditions: Condition[];
  actions: Action[];
  stop: boolean;
}

export interface Prefs {
  name: string;
  signatures: Signature[];
  new_signature: string;
  reply_signature: string;
  categories: Category[];
  rules: Rule[];
  keep_unread: boolean;
  images: boolean;
}

export interface Me {
  email: string;
  addresses: string[];
  prefs: Prefs;
  can_send: boolean;
  max_bytes: number;
  mail_apps: boolean;
}

export interface Ref {
  folder: string;
  uid: number;
}

export interface NewFile {
  name: string;
  type: string;
  data: string;
}

// Draft is what's sent to send or save a message.
export interface Draft {
  from: string;
  to: string[];
  cc: string[];
  bcc: string[];
  subject: string;
  html: string;
  text: string;
  in_reply_to: string;
  references: string;
  files: NewFile[];
  keep: (Ref & { parts: number[] }) | null;
  reply: Ref | null;
  forward: boolean;
  replaces: Ref | null;
}
