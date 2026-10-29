import { useRef, useState } from 'react';
import type { SearchResponse } from './model';

export function bucketLabel(key: string | number, interval: string) {
  const date = new Date(key);
  if (!Number.isFinite(+date)) return String(key);
  const pad = (n: number) => String(n).padStart(2, '0');
  const day = `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  const clock = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  if (interval.includes('d')) return `${date.getFullYear()}-${day}`;
  if (interval.includes('h') || interval === '5m') return `${day} ${clock}`;
  return interval.includes('s') ? `${clock}:${pad(date.getSeconds())}` : clock;
}
const SECOND = 1000, MINUTE = 60 * SECOND, HOUR = 60 * MINUTE, DAY = 24 * HOUR;
const STEPS = [SECOND, 5 * SECOND, 10 * SECOND, 30 * SECOND, MINUTE, 5 * MINUTE, 10 * MINUTE, 30 * MINUTE, HOUR, 3 * HOUR, 6 * HOUR, 12 * HOUR, DAY, 2 * DAY, 7 * DAY, 14 * DAY];
const MONTHS = [1, 3, 6, 12];

// axisTicks returns calendar-aligned ticks (local midnight, whole hours, month starts, ...), about count
// of them, from the last boundary at or before from to the first one at or after to, so the axis
// covers every bucket and never skips a date because of an odd bucket stride.
export function axisTicks(from: number, to: number, count: number): { time: number; text: string }[] {
  const pad = (n: number) => String(n).padStart(2, '0');
  const target = (to - from) / Math.max(1, count);
  const step = STEPS.find(value => value >= target);
  const cursor = new Date(from);
  cursor.setHours(0, 0, 0, 0);
  let advance: (date: Date) => void, text: (date: Date) => string, keep = (_: Date) => true;
  if (step === undefined) {
    const months = MONTHS.find(value => value * 30 * DAY >= target) ?? Math.ceil(target / (365 * DAY)) * 12;
    cursor.setDate(1);
    cursor.setMonth(Math.floor(cursor.getMonth() / months) * months);
    advance = date => date.setMonth(date.getMonth() + months);
    text = date => `${date.getFullYear()}-${pad(date.getMonth() + 1)}`;
  } else if (step >= DAY) {
    cursor.setDate(1);
    advance = date => date.setDate(date.getDate() + 1);
    keep = date => (date.getDate() - 1) % (step / DAY) === 0;
    text = date => `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  } else {
    advance = date => date.setTime(+date + step);
    text = date => {
      const clock = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
      return step < MINUTE ? `${clock}:${pad(date.getSeconds())}` : step < HOUR ? clock : `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${clock}`;
    };
  }
  const ticks: { time: number; text: string }[] = [];
  for (; ; advance(cursor)) {
    if (!keep(cursor)) continue;
    if (+cursor <= from) ticks.length = 0;
    ticks.push({ time: +cursor, text: text(cursor) });
    if (+cursor >= to) return ticks;
  }
}
export default function Histogram({ histogram, interval }: { histogram: NonNullable<SearchResponse['aggregations']>['histogram']; interval: string }) {
  const [zoom, setZoom] = useState(1);
  const chart = useRef<SVGSVGElement>(null);
  const [exportError, setExportError] = useState('');
  const buckets = histogram?.buckets || [];
  const maximum = Math.max(1, ...buckets.map(bucket => Number(bucket.doc_count)));
  const label = (key: string | number) => bucketLabel(key, histogram?.interval || interval);
  const width = 800 * zoom, margin = 30, plot = width - 2 * margin;
  const first = new Date(buckets[0]?.key ?? 0).getTime();
  const span = buckets.length > 1 ? new Date(buckets[1].key).getTime() - first : 0;
  const axis = span > 0 ? axisTicks(first, first + span * buckets.length, 10 * zoom) : [];
  const from = axis[0]?.time ?? first, range = (axis.at(-1)?.time ?? first) - from;
  const x = (time: number) => margin + (time - from) / range * plot;
  const slot = span > 0 ? span / range * plot : plot;
  const center = (index: number) => span > 0 ? x(first + (index + 0.5) * span) : width / 2;
  const ticks = span > 0 ? axis.map(tick => ({ x: x(tick.time), text: tick.text })) : buckets.map(bucket => ({ x: width / 2, text: label(bucket.key) }));
  const anchor = (x: number, text: string) => x - text.length * 3 < 0 ? 'start' : x + text.length * 3 > width ? 'end' : 'middle';
  async function download(format: 'csv' | 'svg' | 'png') {
    setExportError('');
    try {
      let blob: Blob;
      if (format === 'csv') {
        const csv = ['Timestamp,Count', ...buckets.map(bucket => `${new Date(bucket.key).toISOString()},${Number(bucket.doc_count)}`)].join('\n');
        blob = new Blob([csv], { type: 'text/csv' });
      } else {
        const svg = chart.current!.cloneNode(true) as SVGSVGElement;
        svg.setAttribute('width', String(800 * zoom));
        svg.setAttribute('height', '170');
        svg.removeAttribute('style');
        blob = new Blob([new XMLSerializer().serializeToString(svg)], { type: 'image/svg+xml' });
        if (format === 'png') {
          const url = URL.createObjectURL(blob);
          try {
            const image = new Image();
            image.src = url;
            await image.decode();
            const canvas = document.createElement('canvas');
            canvas.width = 800 * zoom;
            canvas.height = 170;
            const context = canvas.getContext('2d');
            if (!context) throw new Error('Canvas unavailable');
            context.fillStyle = 'white';
            context.fillRect(0, 0, canvas.width, canvas.height);
            context.drawImage(image, 0, 0);
            blob = await new Promise<Blob>((resolve, reject) => canvas.toBlob(value => value ? resolve(value) : reject(new Error('PNG export failed')), 'image/png'));
          } finally { URL.revokeObjectURL(url); }
        }
      }
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `search-summary.${format}`;
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch {
      setExportError('Unable to export histogram. Please try another format.');
    }
  }
  return <section aria-label="Search histogram">
    {exportError && <p className="error" role="alert">{exportError}</p>}
    {buckets.length ? <>
      <div className="toolbar"><button type="button" aria-label="Zoom in histogram" onClick={() => setZoom(Math.min(8, zoom * 2))}>+</button><button type="button" aria-label="Zoom out histogram" onClick={() => setZoom(Math.max(1, zoom / 2))}>−</button><button type="button" onClick={() => setZoom(1)}>Reset zoom</button>{(['csv', 'svg', 'png'] as const).map(format => <button type="button" key={format} onClick={() => { void download(format); }}>Download {format.toUpperCase()}</button>)}</div>
      <div className="overflow-x-auto"><svg ref={chart} role="img" aria-label="Document counts over time" viewBox={`0 0 ${width} 170`} style={{ width: `${zoom * 100}%`, minWidth: 300, height: 170 }}>
        <g stroke="currentColor" strokeOpacity="0.15">{ticks.map(tick => <line key={tick.x} x1={tick.x} x2={tick.x} y1="10" y2="140" />)}<line x1={margin} x2={width - margin} y1="135" y2="135" /></g>
        {buckets.map((bucket, index) => {
          const bar = Math.min(slot * 0.9, 24);
          const height = Number(bucket.doc_count) / maximum * 125;
          return <rect key={`${bucket.key}-${index}`} x={center(index) - bar / 2} y={135 - height} width={bar} height={height} rx="2" fill="#26A69A"><title>{label(bucket.key)}: {bucket.doc_count}</title></rect>;
        })}
        {ticks.map(tick => <text key={tick.x} x={tick.x} y="155" fontSize="10" textAnchor={anchor(tick.x, tick.text)} fill="currentColor">{tick.text}</text>)}<text x="0" y="12" fontSize="10" fill="currentColor">{maximum}</text>
      </svg></div>
    </> : <p>No data available</p>}
  </section>;
}
