import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { CardForm, emptyValues, fromCard } from './CardForm';
import type { Card, Metadata } from '../types';

const metadata: Metadata = {
  datasets: [],
  fruits: ['all', 'strawberry'],
  roles: ['', 'manager'],
  sweetTypes: ['', 'cake'],
  versions: ['normal', 'beta'],
};

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  document.cookie = 'pplale_cms_csrf=token-value';
});

function renderForm(kind: Parameters<typeof emptyValues>[0] = 'yojo', onSubmitted = vi.fn()) {
  const values = emptyValues(kind);
  render(
    <CardForm
      metadata={metadata}
      kind={kind}
      nextId="y_42"
      values={{ ...values, name: 'テストカード', imageSlug: 'test_card' }}
      onChange={vi.fn()}
      onSubmitted={onSubmitted}
    />,
  );
  return onSubmitted;
}

describe('CardForm', () => {
  it('shows which ID a new card will take', () => {
    renderForm();
    expect(screen.getByText(/y_42 として登録されます/)).toBeTruthy();
  });

  it('shows dataset specific fields only', () => {
    renderForm('yojo');
    expect(screen.getByLabelText(/役職/)).toBeTruthy();
    expect(screen.queryByLabelText(/お菓子タイプ/)).toBeNull();
  });

  it('offers the sweet type selector for sweet cards', () => {
    renderForm('sweet');
    expect(screen.getByLabelText(/お菓子タイプ/)).toBeTruthy();
    expect(screen.queryByLabelText(/役職/)).toBeNull();
  });

  it('submits with the CSRF header and reports the created pull request', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 201,
      text: async () => JSON.stringify({ cardId: 'y_42', prNumber: 7, prUrl: 'https://example/pull/7', files: [] }),
    });
    const onSubmitted = renderForm();

    await userEvent.click(screen.getByRole('button', { name: /PR を作成する/ }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [path, init] = fetchMock.mock.calls[0];
    expect(path).toBe('/api/submissions');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    expect(init.headers.get('X-CSRF-Token')).toBe('token-value');

    const payload = JSON.parse((init.body as FormData).get('payload') as string);
    expect(payload).toMatchObject({ kind: 'yojo', name: 'テストカード', imageSlug: 'test_card' });

    await waitFor(() => expect(onSubmitted).toHaveBeenCalledWith(expect.objectContaining({ cardId: 'y_42' })));
  });

  it('surfaces per-field validation errors from the server', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 422,
      text: async () =>
        JSON.stringify({ error: '入力内容を確認してください', fields: { name: 'カード名は必須です' } }),
    });
    renderForm();

    await userEvent.click(screen.getByRole('button', { name: /PR を作成する/ }));

    await waitFor(() => expect(screen.getByText('カード名は必須です')).toBeTruthy());
    expect(screen.getByText('入力内容を確認してください')).toBeTruthy();
  });
});

describe('fromCard', () => {
  it('maps a card into editable values without carrying an image slug', () => {
    const card: Card = {
      id: 'y_1',
      name: 'とここ',
      type: 'yojo',
      fruit: 'strawberry',
      description: '',
      imageUrl: '/images/yojo/tokoko.webp',
      cost: 1,
      hp: 2,
      attack: 3,
      effect: '挑発',
      role: '',
      sweetType: undefined,
      version: undefined,
    };
    expect(fromCard('yojo', card)).toMatchObject({
      kind: 'yojo',
      id: 'y_1',
      name: 'とここ',
      effect: '挑発',
      role: '',
      version: 'normal',
      imageSlug: '',
    });
  });
});
