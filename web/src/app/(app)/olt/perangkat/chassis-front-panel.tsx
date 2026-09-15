"use client";

import { useState } from "react";
import { Fan } from "lucide-react";
import type { ChassisCard, ChassisPort, ChassisView } from "@/lib/olts/types";
import styles from "./chassis-front-panel.module.css";

function stateLabel(status?: string) {
  switch (status?.toLowerCase()) {
    case "inservice": return "Running";
    case "standby": return "Standby";
    case "online": return "ONU online";
    case "offline": return "Offline";
    case "los": return "LOS";
    case "idle": return "SFP terdeteksi";
    default: return "Belum terbaca";
  }
}

function stateColor(status?: string) {
  switch (status?.toLowerCase()) {
    case "inservice":
    case "online": return "#16a46b";
    case "standby": return "#d59b16";
    case "offline":
    case "los": return "#dc4545";
    case "idle": return "#229bc2";
    default: return "#87938f";
  }
}

type Selection = { slot: number; port?: number };

function Blade({ slot, card, selection, onSelect }: {
  slot: number;
  card?: ChassisCard;
  selection: Selection | null;
  onSelect: (selection: Selection) => void;
}) {
  const control = card?.is_control;
  const power = /^(PRW|PRAM)/i.test(card?.type || "");
  const selected = selection?.slot === slot;
  return (
    <div className={`${styles.blade} ${control ? styles.control : ""} ${!card ? styles.unread : ""}`} data-slot={slot} data-selected={selected}>
      <button className={styles.boardLabel} type="button" onClick={() => onSelect({ slot })}
        aria-pressed={selected && !selection?.port}
        title={`Slot ${slot} - ${card?.type || "Belum terdeteksi"} - ${stateLabel(card?.status)}`}>
        <span className={styles.screw} aria-hidden="true" />
        <span className={styles.slotNumber}>{slot}</span>
        <strong>{card?.type || "---"}</strong>
        <span className={styles.led} style={{ background: stateColor(card?.status) }} />
      </button>
      {power ? (
        <div className={styles.powerFace}>
          <span>-48V</span><span className={styles.terminal} aria-hidden="true" /><span className={styles.terminal} aria-hidden="true" />
          <span>RTN</span><span className={styles.vents} aria-hidden="true" />
        </div>
      ) : control ? (
        <div className={styles.controlFace}>
          <span className={styles.socket} aria-hidden="true" />
          <span className={styles.socket} aria-hidden="true" />
          <span className={styles.role}>{card.role || stateLabel(card.status)}</span>
          <span className={styles.vents} aria-hidden="true" />
        </div>
      ) : card?.ports?.length ? (
        <div className={styles.ports}>
          {card.ports.map(port => (
            <button key={port.port} type="button" className={styles.port}
              aria-label={`Slot ${slot} port ${port.port}: ${stateLabel(port.status)}, ONU ${port.onu_online}/${port.onu_total}`}
              aria-pressed={selected && selection?.port === port.port}
              title={`${port.label || `Port ${port.port}`} - ${stateLabel(port.status)} - ONU ${port.onu_online}/${port.onu_total}`}
              onClick={() => onSelect({ slot, port: port.port })}>
              <span className={styles.portNumber}>{port.port}</span>
              <span className={styles.connector} style={{ borderBottomColor: stateColor(port.status) }}>
                <span className={styles.led} style={{ background: stateColor(port.status) }} />
              </span>
            </button>
          ))}
        </div>
      ) : <span className={styles.vents} aria-hidden="true" />}
      <span className={styles.handle} aria-hidden="true" />
    </div>
  );
}

function PortReadout({ port }: { port: ChassisPort }) {
  return <>
    <div><dt>PON</dt><dd>{port.label || port.port}</dd></div>
    <div><dt>Status</dt><dd>{stateLabel(port.status)}</dd></div>
    <div><dt>ONU online / total</dt><dd>{port.onu_online} / {port.onu_total}</dd></div>
    <div><dt>Rx / Tx</dt><dd>{port.rx_dbm ? `${port.rx_dbm.toFixed(2)} dBm` : "-"} / {port.tx_dbm ? `${port.tx_dbm.toFixed(2)} dBm` : "-"}</dd></div>
  </>;
}

export function ChassisFrontPanel({ data }: { data: ChassisView }) {
  const [selection, setSelection] = useState<Selection | null>(null);
  if (data.family !== "C320" && data.family !== "C300") return null;
  const compact = data.family === "C320";
  const cards = new Map(data.cards.map(card => [card.slot, card]));
  const selected = selection ? cards.get(selection.slot) : undefined;
  const selectedPort = selected?.ports?.find(port => port.port === selection?.port);
  const running = data.cards.filter(card => card.status?.toLowerCase() === "inservice").length;
  const ports = data.cards.flatMap(card => card.ports || []);
  const renderBlade = (slot: number) => <Blade key={slot} slot={slot} card={cards.get(slot)} selection={selection} onSelect={setSelection} />;

  return (
    <section className={styles.section} aria-label={`Tampak depan ${data.family}`}>
      <div className={styles.heading}>
        <div><h2>ZXA10 {data.family}</h2><span>Tampak depan · {compact ? "2U / 2 slot layanan" : "16 slot layanan / kontrol 9-10"}</span></div>
        <dl className={styles.metrics}>
          <div><dt>Card running</dt><dd>{running}<small> / {data.cards.length}</small></dd></div>
          <div><dt>PON dengan ONU online</dt><dd>{ports.filter(port => port.onu_online > 0).length}<small> / {ports.length}</small></dd></div>
        </dl>
      </div>
      <div className={styles.viewport} tabIndex={0} role="region" aria-label="Chassis fisik OLT">
        <div className={`${styles.chassis} ${compact ? styles.c320 : styles.c300}`}>
          <div className={styles.ear} aria-hidden="true"><span /><span /><span /></div>
          <div className={styles.enclosure}>
            <div className={styles.brand}><strong>ZTE</strong><span>ZXA10 {data.family}</span><span>OPTICAL ACCESS</span></div>
            {compact ? (
              <div className={styles.compactBody}>
                <div className={styles.fanSide} title="Panel fan - status belum tersedia"><Fan size={20} /><span>FAN</span></div>
                <div className={styles.compactCards}>
                  {renderBlade(1)}{renderBlade(2)}
                  <div className={styles.controllers}>{renderBlade(3)}{renderBlade(4)}</div>
                </div>
              </div>
            ) : (
              <>
                <div className={styles.fanTray} title="Fan tray - status belum tersedia"><Fan size={28} /><span className={styles.vents} /><Fan size={28} /><span className={styles.vents} /><Fan size={28} /></div>
                <div className={styles.verticalCards}>{Array.from({ length: 20 }, (_, index) => renderBlade(index + 1))}</div>
              </>
            )}
            <div className={styles.rail} aria-hidden="true" />
          </div>
          <div className={styles.ear} aria-hidden="true"><span /><span /><span /></div>
        </div>
      </div>
      {selection && <div className={styles.inspector} aria-live="polite">
        <strong>Slot {selection.slot} · {selected?.type || "Belum terdeteksi"}</strong>
        <dl>{selectedPort ? <PortReadout port={selectedPort} /> : <>
          <div><dt>Status</dt><dd>{stateLabel(selected?.status)}</dd></div>
          <div><dt>Peran</dt><dd>{selected?.role || "-"}</dd></div>
          <div><dt>Port teridentifikasi</dt><dd>{selected?.port_count ?? "-"}</dd></div>
          <div><dt>Suhu</dt><dd>{selected?.temp_c ? `${selected.temp_c.toFixed(1)} °C` : "-"}</dd></div>
        </>}</dl>
      </div>}
    </section>
  );
}