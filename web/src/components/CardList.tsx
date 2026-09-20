import { useMemo, useState } from 'react';
import type { Card } from '../types';

interface Props {
  cards: Card[];
  onEdit: (card: Card) => void;
}

export function CardList({ cards, onEdit }: Props) {
  const [query, setQuery] = useState('');

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return cards;
    return cards.filter(
      (c) => c.name.toLowerCase().includes(needle) || c.id.toLowerCase().includes(needle),
    );
  }, [cards, query]);

  return (
    <div className="card-list">
      <input
        className="search"
        placeholder="カード名 / ID で検索"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <p className="hint">
        {filtered.length} / {cards.length} 件 — PPLALE-web の main ブランチの内容です
      </p>
      <ul>
        {filtered.map((card) => (
          <li key={card.id}>
            <button type="button" onClick={() => onEdit(card)}>
              <span className="card-id">{card.id}</span>
              <span className="card-name">{card.name}</span>
              <span className="card-stats">
                {card.cost} / {card.attack} / {card.hp}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
