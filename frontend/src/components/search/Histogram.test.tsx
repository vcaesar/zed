import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Histogram, { axisTicks, bucketLabel } from './Histogram';

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });
const histogram = { buckets: [{ key: new Date(2026, 1, 15, 12, 30, 45).getTime(), doc_count: 7 }] };
describe('Histogram', () => {
  it.each([['1s', '12:30:45'], ['1m', '12:30'], ['1h', '02-15 12:30'], ['1d', '2026-02-15']] as const)('formats %s buckets', (interval, label) => {
    expect(bucketLabel(histogram.buckets[0].key, interval)).toBe(label);
  });
  it('zooms and resets while exposing bucket counts', () => {
    render(<Histogram histogram={histogram} interval="1s" />);
    expect(screen.getByText('12:30:45: 7')).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText('Zoom in histogram'));
    expect(screen.getByRole('img')).toHaveStyle({ width: '200%' });
    fireEvent.click(screen.getByRole('button', { name: 'Reset zoom' }));
    expect(screen.getByRole('img')).toHaveStyle({ width: '100%' });
  });
  it('keeps sparse bars slim and centered, and many bars inside their slots', () => {
    const { container, rerender } = render(<Histogram histogram={histogram} interval="1s" />);
    const bar = container.querySelector('rect')!;
    expect(bar).toHaveAttribute('width', '24');
    expect(bar).toHaveAttribute('x', '388');
    expect(container.querySelector('text[text-anchor=middle]')).toHaveAttribute('x', '400');
    const many = { buckets: Array.from({ length: 100 }, (_, i) => ({ key: histogram.buckets[0].key + i * 1000, doc_count: i + 1 })) };
    rerender(<Histogram histogram={many} interval="1s" />);
    const widths = Array.from(container.querySelectorAll('rect'), rect => Number(rect.getAttribute('width')));
    expect(widths).toHaveLength(100);
    expect(Math.max(...widths)).toBeCloseTo(740 / 110 * 0.9); // axis 12:30:40-12:32:30 in 10s ticks
  });
  it('extends the axis to whole days on both sides and places bars by time', () => {
    const start = new Date(2025, 11, 31, 19).getTime();
    const sparse = { interval: '3h', buckets: Array.from({ length: 65 }, (_, i) => ({ key: start + i * 3 * 3600_000, doc_count: [0, 32, 64].includes(i) ? 1 : 0 })) };
    const { container } = render(<Histogram histogram={sparse} interval="1h" />);
    const ticks = Array.from(container.querySelectorAll('text[y="155"]'));
    expect(ticks.map(tick => tick.textContent)).toEqual(['12-31', '01-01', '01-02', '01-03', '01-04', '01-05', '01-06', '01-07', '01-08', '01-09']);
    ticks.forEach((tick, i) => expect(Number(tick.getAttribute('x'))).toBeCloseTo(30 + i * 740 / 9));
    expect(container.querySelectorAll('line')).toHaveLength(11);
    const hour = 740 / 216, px = (hours: number) => 30 + hours * hour;
    const centers = Array.from(container.querySelectorAll('rect'), rect => Number(rect.getAttribute('x')) + Number(rect.getAttribute('width')) / 2);
    expect(centers[0]).toBeCloseTo(px(19 + 1.5)); // Dec 31 19:00-22:00
    expect(centers[32]).toBeCloseTo(px(4 * 24 + 19 + 1.5)); // Jan 4 19:00, between the 01-04 and 01-05 lines
    expect(centers[64]).toBeCloseTo(px(8 * 24 + 19 + 1.5)); // Jan 8 19:00, inside the last day
  });
  it.each([
    ['seconds', 0, 60_000, ['12:00:00', '12:00:10', '12:00:20', '12:00:30', '12:00:40', '12:00:50', '12:01:00']],
    ['minutes', 5_000, 3600_000, ['12:00', '12:10', '12:20', '12:30', '12:40', '12:50', '13:00', '13:10']],
    ['hours', 0, 36 * 3600_000, ['01-01 12:00', '01-01 18:00', '01-02 00:00', '01-02 06:00', '01-02 12:00', '01-02 18:00', '01-03 00:00']],
    ['days', 22 * 3600_000, 18 * 86400_000, ['01-01', '01-03', '01-05', '01-07', '01-09', '01-11', '01-13', '01-15', '01-17', '01-19', '01-21']],
    ['months', 0, 600 * 86400_000, ['2026-01', '2026-04', '2026-07', '2026-10', '2027-01', '2027-04', '2027-07', '2027-10']],
  ] as const)('aligns %s ticks to calendar boundaries around the range', (_, offset, length, labels) => {
    const from = new Date(2026, 0, 1, 12).getTime() + offset;
    expect(axisTicks(from, from + length, 10).map(tick => tick.text)).toEqual(labels);
  });
  it.each(['CSV', 'SVG', 'PNG'])('exports %s', async format => {
    const create = vi.fn(() => 'blob:histogram');
    vi.stubGlobal('URL', { createObjectURL: create, revokeObjectURL: vi.fn() });
    vi.stubGlobal('Image', class { src = ''; decode = vi.fn().mockResolvedValue(undefined); });
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({ fillStyle: '', fillRect: vi.fn(), drawImage: vi.fn() } as unknown as CanvasRenderingContext2D);
    vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(callback => callback(new Blob(['png'], { type: 'image/png' })));
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    render(<Histogram histogram={histogram} interval="1s" />);
    fireEvent.click(screen.getByRole('button', { name: `Download ${format}` }));
    await waitFor(() => expect(click).toHaveBeenCalledOnce());
    expect((click.mock.instances[0] as HTMLAnchorElement).download).toBe(`search-summary.${format.toLowerCase()}`);
    expect(create.mock.calls.length).toBeGreaterThan(0);
    await waitFor(() => expect(URL.revokeObjectURL).toHaveBeenCalledTimes(format === 'PNG' ? 2 : 1));
  });
  it('reports failed export without breaking the chart', async () => {
    vi.stubGlobal('URL', { createObjectURL: () => { throw new Error('unavailable'); } });
    render(<Histogram histogram={histogram} interval="1s" />);
    fireEvent.click(screen.getByRole('button', { name: 'Download SVG' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Unable to export histogram');
    expect(screen.getByRole('img')).toBeInTheDocument();
  });
});
