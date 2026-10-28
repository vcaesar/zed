import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Histogram, { bucketLabel } from './Histogram';

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
    expect(bar).toHaveAttribute('x', '398');
    expect(container.querySelector('text[text-anchor=middle]')).toHaveAttribute('x', '410');
    const many = { buckets: Array.from({ length: 100 }, (_, i) => ({ key: histogram.buckets[0].key + i * 1000, doc_count: i + 1 })) };
    rerender(<Histogram histogram={many} interval="1s" />);
    const widths = Array.from(container.querySelectorAll('rect'), rect => Number(rect.getAttribute('width')));
    expect(widths).toHaveLength(100);
    expect(Math.max(...widths)).toBeCloseTo(7.6 * 0.9);
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
