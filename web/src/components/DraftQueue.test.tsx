import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DraftQueue } from './DraftQueue';
import type { Draft } from '../types';

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  document.cookie = 'pplale_cms_csrf=token-value';
});

function draft(overrides: Partial<Draft>): Draft {
  return {
    id: 1,
    kind: 'yojo',
    cardId: '',
    isEdit: false,
    name: 'かがり',
    fruit: 'strawberry',
    description: '',
    cost: 1,
    hp: 1,
    attack: 1,
    imageDisplayUrl: '/api/drafts/1/image',
    hasNewImage: true,
    createdAt: '2026-09-20T00:00:00Z',
    ...overrides,
  };
}

describe('DraftQueue', () => {
  it('shows a hint instead of a submit button when empty', () => {
    render(<DraftQueue drafts={[]} onChanged={vi.fn()} onSubmitted={vi.fn()} />);
    expect(screen.getByText(/下書きはまだありません/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /まとめて送信する/ })).toBeNull();
  });

  it('shows field-level and image before/after diffs for an edited card', () => {
    const edited = draft({
      cardId: 'y_1',
      isEdit: true,
      name: 'かがり改',
      effect: '相手は2枚引く',
      cost: 2,
      imageDisplayUrl: '/api/drafts/1/image',
      hasNewImage: true,
      original: {
        id: 'y_1', name: 'かがり', type: 'yojo', fruit: 'strawberry', description: '',
        imageUrl: '/images/yojo/kagari.webp', imageDisplayUrl: 'https://example/kagari.webp',
        cost: 1, hp: 1, attack: 1, effect: '相手は1枚引く', role: '',
      },
    });
    const view = render(<DraftQueue drafts={[edited]} onChanged={vi.fn()} onSubmitted={vi.fn()} />);

    expect(view.container.querySelector('.draft-name del')?.textContent).toBe('かがり');
    expect(view.container.querySelector('.draft-name ins')?.textContent).toBe('かがり改');
    expect(screen.getByText('コスト')).toBeTruthy();
    expect(screen.getByText('効果')).toBeTruthy();
    expect(view.container.querySelector('.draft-text-before del')?.textContent).toBe('1');
    expect(view.container.querySelector('.draft-text-after ins')?.textContent).toBe('2');
    expect(screen.getByAltText('かがりの現在の画像').getAttribute('src')).toBe('https://example/kagari.webp');
    expect(screen.getByAltText('かがり改の変更後の画像').getAttribute('src')).toBe('/api/drafts/1/image');
  });

  it('submits every queued draft in one request and reports the result', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 201,
      text: async () =>
        JSON.stringify({
          branch: 'cms/batch-abcd',
          files: ['src/data/yojo.json'],
          prUrl: 'https://example/pull/9',
          prNumber: 9,
          submission: { id: 1, cards: [{ kind: 'yojo', cardId: 'y_1', cardName: 'かがり', isEdit: false }] },
        }),
    });
    const onSubmitted = vi.fn();
    const drafts = [draft({ id: 1, name: 'かがり' }), draft({ id: 2, name: 'とここ' })];
    render(<DraftQueue drafts={drafts} onChanged={vi.fn()} onSubmitted={onSubmitted} />);

    await userEvent.click(screen.getByRole('button', { name: /まとめて送信する（2件）/ }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [path, init] = fetchMock.mock.calls[0];
    expect(path).toBe('/api/drafts/submit');
    expect(init.method).toBe('POST');
    expect(init.headers.get('X-CSRF-Token')).toBe('token-value');
    // No ids specified: submits everything currently queued.
    expect(JSON.parse(init.body as string)).toEqual({});

    await waitFor(() => expect(onSubmitted).toHaveBeenCalledWith(expect.objectContaining({ prNumber: 9 })));
  });

  it('removes a single draft without submitting the rest', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, text: async () => JSON.stringify({ status: 'ok' }) });
    const onChanged = vi.fn();
    const drafts = [draft({ id: 1, name: 'かがり' }), draft({ id: 2, name: 'とここ' })];
    render(<DraftQueue drafts={drafts} onChanged={onChanged} onSubmitted={vi.fn()} />);

    await userEvent.click(screen.getByRole('button', { name: 'かがりを下書きから削除' }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(fetchMock.mock.calls[0][0]).toBe('/api/drafts/1');
    expect(fetchMock.mock.calls[0][1].method).toBe('DELETE');
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  it('surfaces a submission error without clearing the queue', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 502,
      text: async () => JSON.stringify({ error: 'PR の作成に失敗しました' }),
    });
    const onSubmitted = vi.fn();
    render(<DraftQueue drafts={[draft({})]} onChanged={vi.fn()} onSubmitted={onSubmitted} />);

    await userEvent.click(screen.getByRole('button', { name: /まとめて送信する/ }));

    await waitFor(() => expect(screen.getByText('PR の作成に失敗しました')).toBeTruthy());
    expect(onSubmitted).not.toHaveBeenCalled();
  });
});
