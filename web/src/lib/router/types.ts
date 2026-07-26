export type RouterConfig = {
  id?: string;
  name?: string;
  host?: string;
  api_port: number;
  use_tls?: boolean;
  api_username?: string;
  routeros_version?: string;
  identity?: string;
  last_connected_at?: string | null;
  last_error?: string;
  configured: boolean;
};

export type RouterTestResult = {
  identity: string;
  routeros_version: string;
  secret_count: number;
  profile_count: number;
};
