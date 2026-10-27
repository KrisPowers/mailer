// Shapes returned by the Go admin API (internal/admin/admin.go).

export interface Step {
  name: string;
  ok: boolean;
  detail?: string;
}

export interface Check {
  name: string;
  ok: boolean;
  detail: string;
}

export interface DomainStatus {
  name: string;
  primary: boolean;
  account_id: string;
  checks: Step[];
  error?: string;
}

export interface CloudflareStatus {
  connected: boolean;
  domain: string;
  tunnel_status: string;
  tunnel_connections: number;
  public_healthy: boolean;
  domains: DomainStatus[];
  error?: string;
}

export interface TailscaleStatus {
  installed: boolean;
  running: boolean;
  state: string;
  dns_name: string;
  login_url?: string;
  error?: string;
  funnel_imap: boolean;
  funnel_submission: boolean;
}

export interface TunnelStatus {
  running: boolean;
  binary: string;
  since: string;
  last_error?: string;
  last_line?: string;
}

export interface Checks {
  at: string;
  cloudflare: CloudflareStatus;
  tailscale: TailscaleStatus;
  tunnel: TunnelStatus;
  items: Check[];
}

export interface ClientSettings {
  server: string;
  imap_port: number;
  smtp_port: number;
}

export interface Overview {
  domain: string;
  domains: string[] | null;
  hostname: string;
  cloudflare_setup: boolean;
  tailscale_setup: boolean;
  send_via_cloudflare: boolean;
  accounts: number;
  queue: number;
  checks: Checks | null;
  client: ClientSettings | null;
  last_inbound: string;
  connected_at: string;
  funnel_at: string;
}

export interface Mailbox {
  name: string;
  messages: number;
  unseen: number;
  size: number;
}

export interface Account {
  email: string;
  mailboxes: Mailbox[];
  messages: number;
  unseen: number;
  size: number;
  aliases: string[];
}

export type Aliases = Record<string, string>;

export interface QueueRcpt {
  addr: string;
  status: "pending" | "sent" | "failed";
  last_error?: string;
}

export interface QueueItem {
  id: string;
  from: string;
  rcpts: QueueRcpt[];
  created: string;
  next_try: string;
  attempts: number;
}

export interface DomainInfo {
  name: string;
  primary: boolean;
  account_id: string;
  connected_at: string;
  accounts: number;
  status: DomainStatus | null;
}

export interface Zone {
  name: string;
  connected: boolean;
}

export interface Settings {
  reject_dmarc_failures: boolean;
  save_sent_copy: boolean;
  send_via_cloudflare: boolean;
  max_message_mb: number;
}

export interface ConnectResult {
  steps?: Step[];
  error?: string;
  choose_zone?: string[];
}

export interface TailscaleResult {
  ok?: boolean;
  dns_name?: string;
  error?: string;
  action_url?: string;
  install?: boolean;
}

export interface Credentials {
  email: string;
  password: string;
}
