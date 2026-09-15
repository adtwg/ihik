export type TrafficPoint = { t: number; in_bps: number; out_bps: number };
export type TrafficCounter = { t: number; in_octets: number; out_octets: number; method?: string };

export function counterRate(previous: TrafficCounter | null, current: TrafficCounter): TrafficPoint | null {
  if (!previous || current.method !== previous.method) return null;
  const seconds = current.t - previous.t;
  if (seconds <= 0 || seconds > 60) return null;
  const counters = [previous.in_octets, previous.out_octets, current.in_octets, current.out_octets];
  if (counters.some(value => !Number.isSafeInteger(value) || value < 0)) return null;
  if (current.in_octets < previous.in_octets || current.out_octets < previous.out_octets) return null;
  return {
    t: current.t,
    in_bps: (current.in_octets - previous.in_octets) * 8 / seconds,
    out_bps: (current.out_octets - previous.out_octets) * 8 / seconds,
  };
}

export function mergeTraffic(history: TrafficPoint[], live: TrafficPoint[], cutoff: number): TrafficPoint[] {
  const points = new Map<number, TrafficPoint>();
  for (const point of [...history, ...live]) {
    if (!Number.isFinite(point.t) || point.t < cutoff ||
      !Number.isFinite(point.in_bps) || !Number.isFinite(point.out_bps) || point.in_bps < 0 || point.out_bps < 0) continue;
    points.set(point.t, point);
  }
  return [...points.values()].sort((first, second) => first.t - second.t);
}

export function formatTraffic(value?: number): string {
  if (value == null || !Number.isFinite(value)) return "-";
  if (value >= 1e9) return `${(value / 1e9).toFixed(2)} Gbps`;
  if (value >= 1e6) return `${(value / 1e6).toFixed(2)} Mbps`;
  if (value >= 1e3) return `${(value / 1e3).toFixed(1)} Kbps`;
  return `${Math.round(value)} bps`;
}