import { useMemo, useState } from 'react';
import type { Card, ViewMode } from '../types';

interface Props {
  cards: Card[];
  onEdit: (card: Card) => void;
}

export function CardList({ cards, onEdit }: Props) {
  const [query, setQuery] = useState('');
  const [view, setView] = useState<ViewMode>('grid');

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return cards;
    return cards.filter(
      (c) => c.name.toLowerCase().includes(needle) || c.id.toLowerCase().includes(needle),
    );
  }, [cards, query]);

  return (
    <div className="card-list">
      <div className="card-list-toolbar">
        <input
          className="search"
          placeholder="カード名 / ID で検索"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="view-toggle" role="group" aria-label="表示形式">
          <button type="button" className={view === 'grid' ? 'active' : ''} onClick={() => setView('grid')}>
            グリッド
          </button>
          <button type="button" className={view === 'list' ? 'active' : ''} onClick={() => setView('list')}>
            リスト
          </button>
        </div>
      </div>
      <p className="hint">
        {filtered.length} / {cards.length} 件 — PPLALE-web の main ブランチの内容です
      </p>
      {view === 'grid' ? (
        <CardGrid cards={filtered} onEdit={onEdit} />
      ) : (
        <CardRows cards={filtered} onEdit={onEdit} />
      )}
    </div>
  );
}

function CardGrid({ cards, onEdit }: Props) {
  return (
    <ul className="card-grid">
      {cards.map((card) => (
        <li key={card.id}>
          <button type="button" onClick={() => onEdit(card)}>
            <img className="grid-thumb" src={card.imageDisplayUrl} alt="" loading="lazy" />
            <div className="grid-overlay">
              <span className="grid-name">{card.name}</span>
              <span className="grid-stats">
                {card.cost} / {card.attack} / {card.hp}
              </span>
            </div>
          </button>
        </li>
      ))}
    </ul>
  );
}

function CardRows({ cards, onEdit }: Props) {
  return (
    <ul className="card-rows">
      {cards.map((card) => (
        <li key={card.id}>
          <button type="button" onClick={() => onEdit(card)}>
            <img className="card-thumb" src={card.imageDisplayUrl} alt="" loading="lazy" />
            <span className="card-id">{card.id}</span>
            <span className="card-name">{card.name}</span>
            <span className="card-stats">
              {card.cost} / {card.attack} / {card.hp}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}
