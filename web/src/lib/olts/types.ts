export type OLT = {
  id: string;
  name: string;
  host: string;
  port: number;
  model: string;
  snmp_mode: "v2c" | "v3";
  v3_username?: string;
  v3_auth_protocol?: string;
  v3_priv_protocol?: string;
  has_secret: boolean;
  cli_protocol?: 'ssh' | 'telnet';
  cli_port?: number;
  cli_username?: string;
  last_connected_at?: string | null;
  last_error?: string;
};

export type SystemInfo = {
  sys_descr: string;
  sys_uptime: string;
  model_hint: string;
};

export type OltCLITestInfo = {
  protocol: 'ssh' | 'telnet' | string;
  host: string;
  port: number;
  username: string;
  command: string;
  sample_output?: string;
};

export type ONU = {
  index: string;
  onu_number: string;
  name: string;
  serial_number: string;
  model?: string;
  description?: string;
  status: string;
  rx_power_dbm: number;
  tx_power_dbm: number;
  distance_m?: number;
  ip_address?: string;
  in_bps?: number;
  out_bps?: number;
};

export type ONUPaged = {
  items: ONU[];
  total: number;
  page: number;
};

export type ONUActionResponse = {
  status: "ok";
  action: string;
  method?: "snmp" | "cli" | string;
  fallback?: boolean;
  message?: string;
};

export type ONUOneSyncResponse = {
  status: "ok";
  message?: string;
  result?: {
    pon: string;
    onu_id: number;
    status: string;
    rx_power_dbm: number;
    tx_power_dbm: number;
    distance_m: number;
    name?: string;
    method?: "cli" | string;
  };
};

export type ONUConfigDetail = {
  pon_port: string;
  onu_id: number;
  interface: string;
  status?: string;
  name?: string;
  description?: string;
  serial_number?: string;
  onu_type?: string;
  distance_m?: number;
  rx_onu_side_dbm?: number;
  tx_onu_side_dbm?: number;
  rx_olt_side_dbm?: number;
  upstream_bps?: number;
  downstream_bps?: number;
  vlans?: number[];
  dba_profiles?: string[];
  upstream_profiles?: string[];
  downstream_profiles?: string[];
  tconts?: Array<{ id: number; name?: string; profile?: string; dba_gap_mode?: string; config?: string }>;
  gemports?: Array<{ id: number; upstream_profile?: string; downstream_profile?: string; config?: string }>;
  service_ports?: Array<{
    id: number;
    vport?: number;
    description?: string;
    mode?: string;
    user_vlan?: number;
    user_svlan?: number;
    vlan?: number;
    c_tag_cos?: number;
    svlan?: number;
    s_tag_cos?: number;
    ether_type?: string;
    raw?: string;
  }>;
  wan_ips?: Array<{
    id: number;
    mode?: string;
    auth_mode?: string;
    vlan_profile?: string;
    ip_profile?: string;
    static_ip?: string;
    pppoe_username?: string;
    pppoe_password?: string;
    respond_ping?: boolean;
    respond_traceroute?: boolean;
    raw?: string;
  }>;
  config_state?: {
    status?: "configured" | "partial" | "unconfigured" | string;
    access?: "pppoe" | "ipoe" | "bridge" | "unknown" | string;
    missing?: string[];
    reason?: string;
  };
  warnings?: string[];
};

export type ONUConfigApplyInput = {
  pon: string;
  onu_id: number;
  operation: "set_name" | "set_description" | "set_tcont" | "set_gemport" | "set_service_port" | "set_service_port_description" | "set_wan_ip" | "delete_wan_ip" | "delete_service_port" | "delete_tcont" | "delete_gemport" | "auto_config_onu";
  name?: string;
  description?: string;
  tcont_id?: number;
  tcont_name?: string;
  tcont_profile?: string;
  gemport_id?: number;
  upstream_profile?: string;
  downstream_profile?: string;
  service_port_id?: number;
  vport?: number;
  user_vlan?: number;
  user_svlan?: number;
  vlan?: number;
  c_vid?: number;
  svlan?: number;
  s_vid?: number;
  c_tag_cos?: number;
  s_tag_cos?: number;
  service_port_mode?: "tagged" | "untagged" | "double_vlan" | "hybrid";
  service_description?: string;
  wan_ip_id?: number;
  wan_mode?: "pppoe" | "ipoe" | "static";
  wan_auth_mode?: "auto" | "pap" | "chap";
  wan_vlan_profile?: string;
  wan_ip_profile?: string;
  wan_static_ip?: string;
  wan_pppoe_username?: string;
  wan_pppoe_password?: string;
  wan_respond_ping?: boolean;
  wan_respond_traceroute?: boolean;
  ether_type?: "all" | "pppoe" | "ipoe";
  apply_via?: "auto" | "cli" | "snmp";
};

export type ONUConfigApplyResponse = {
  status: "ok";
  operation: string;
  method?: "cli" | string;
  executed?: string[];
  message?: string;
};

export const onuStatusLabels: Record<string, { label: string; className: string }> = {
  working: { label: "Online", className: "" },
  los: { label: "LOS", className: "danger" },
  logging: { label: "Logging", className: "warning" },
  sync_mib: { label: "Sync MIB", className: "warning" },
  dying_gasp: { label: "Dying Gasp", className: "warning" },
  auth_failed: { label: "Auth Gagal", className: "danger" },
  offlined: { label: "Offline", className: "danger" },
  unknown: { label: "Tidak diketahui", className: "warning" },
};

export type ChassisPort = {
  port: number;
  label?: string;
  has_sfp: boolean;
  rx_dbm?: number;
  tx_dbm?: number;
  onu_total: number;
  onu_online: number;
  status: "online" | "los" | "idle" | "empty" | string;
};

export type ChassisCard = {
  slot: number;
  type?: string;
  status?: string;
  role?: string;
  cpu_percent: number;
  mem_percent: number;
  temp_c: number;
  is_control: boolean;
  port_count: number;
  ports?: ChassisPort[];
};

export type ChassisView = {
  model: string;
  family: "C320" | "C300" | "C220" | "C600" | "unknown" | string;
  source: "snmp" | "cli" | "snmp+cli" | string;
  cards: ChassisCard[];
};

