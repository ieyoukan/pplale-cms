import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { CardList } from './CardList';
import type { Card } from '../types';

function card(overrides: Partial<Card>): Card {
  return {
    id: 'y_0',
    name: 'かがり',
    type: 'yojo',
    fruit: 'strawberry',
    description: '',
    imageUrl: '/images/yojo/kagari.webp',
    imageDisplayUrl: 'https://raw.githubusercontent.com/ieyoukan/PPLALE-web/main/public/images/yojo/kagari.webp',
    cost: 1,
    hp: 1,
    attack: 1,
    effect: '',
    role: '',
    sweetType: undefined,
    version: undefined,
    ...overrides,
  };
}

describe('CardList', () => {
  it('defaults to the image-first grid view', () => {
    const cards = [card({ id: 'y_0', name: 'かがり' })];
    const { container } = render(<CardList cards={cards} onEdit={vi.fn()} />);

    const img = container.querySelector('img.grid-thumb');
    expect(img?.getAttribute('src')).toBe(cards[0].imageDisplayUrl);
    // imageUrl ("/images/...") is site-relative and not browser-fetchable on
    // its own; the thumbnail must never fall back to it.
    expect(img?.getAttribute('src')).not.toBe(cards[0].imageUrl);
    expect(container.querySelector('.grid-overlay')?.textContent).toContain('かがり');
  });

  it('switches to the list view on request', async () => {
    const cards = [card({ id: 'y_0', name: 'かがり' })];
    const { container } = render(<CardList cards={cards} onEdit={vi.fn()} />);

    await userEvent.click(screen.getByRole('button', { name: 'リスト' }));
    expect(container.querySelector('img.card-thumb')).not.toBeNull();
    expect(container.querySelector('img.grid-thumb')).toBeNull();
  });

  it('filters by name or id and still calls onEdit with the full card', async () => {
    const onEdit = vi.fn();
    const cards = [card({ id: 'y_0', name: 'かがり' }), card({ id: 'y_1', name: 'とここ' })];
    const { container } = render(<CardList cards={cards} onEdit={onEdit} />);

    await userEvent.type(screen.getByPlaceholderText(/検索/), 'とここ');
    expect(container.querySelectorAll('img.grid-thumb')).toHaveLength(1);

    await userEvent.click(screen.getByRole('button', { name: /とここ/ }));
    expect(onEdit).toHaveBeenCalledWith(cards[1]);
  });
});
