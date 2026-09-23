import { useState } from 'react';
import { ApiError, api } from '../api';
import type { Draft, SubmitResult } from '../types';

interface Props {
  drafts: Draft[];
  onChanged: () => void;
  onSubmitted: (result: SubmitResult) => void;
}

const kindLabels: Record<string, string> = {
  yojo: '幼女',
  sweet: 'お菓子',
  playable: 'プレイアブル',
  tokenYojo: 'トークン幼女',
};

const fruitLabels: Record<string, string> = {
  all: '全種',
  strawberry: 'いちご',
  grape: 'ぶどう',
  melon: 'メロン',
  orange: 'オレンジ',
};

const roleLabels: Record<string, string> = {
  '': 'なし',
  assistant_manager: '副店長',
  manager: '店長',
};

const sweetLabels: Record<string, string> = {
  '': '分類なし',
  animal_soda: '動物さんソーダ',
  cafe: 'カフェ',
  float: 'フロート',
  doughnut: 'ドーナツ',
  cake: 'ケーキ',
  back_menu: '裏メニュー',
  chai: 'チャイ',
  ice_cream: 'アイスクリーム',
  pplale_soda: 'ぷぷりえソーダ',
  pplale_yaki: 'ぷぷりえ焼き',
  currency: '通貨',
};

type Change = {
  key: string;
  label: string;
  before: string;
  after: string;
  text?: boolean;
};

type TextPart = { type: 'same' | 'removed' | 'added'; text: string };

function display(value: string | number | undefined, labels?: Record<string, string>): string {
  const raw = value == null ? '' : String(value);
  return labels?.[raw] ?? (raw || 'なし');
}

function changesFor(draft: Draft): Change[] {
  const original = draft.original;
  if (!original) return [];

  const candidates: Change[] = [
    { key: 'fruit', label: 'フルーツ', before: display(original.fruit, fruitLabels), after: display(draft.fruit, fruitLabels) },
    { key: 'cost', label: 'コスト', before: display(original.cost), after: display(draft.cost) },
    { key: 'attack', label: '攻撃', before: display(original.attack), after: display(draft.attack) },
    { key: 'hp', label: '体力', before: display(original.hp), after: display(draft.hp) },
    { key: 'role', label: '役職', before: display(original.role, roleLabels), after: display(draft.role, roleLabels) },
    { key: 'sweetType', label: 'お菓子タイプ', before: display(original.sweetType, sweetLabels), after: display(draft.sweetType, sweetLabels) },
    { key: 'version', label: 'バージョン', before: display(original.version), after: display(draft.version) },
    { key: 'effect', label: '効果', before: original.effect ?? '', after: draft.effect ?? '', text: true },
    { key: 'description', label: '説明', before: original.description, after: draft.description, text: true },
  ];
  return candidates.filter((change) => change.before !== change.after);
}

// Character-level LCS keeps Japanese effect text readable and highlights each
// changed fragment. Fall back to one replacement for unexpectedly long text.
function diffText(before: string, after: string): TextPart[] {
  const left = Array.from(before);
  const right = Array.from(after);
  if (left.length * right.length > 100_000) {
    return [
      ...(before ? [{ type: 'removed' as const, text: before }] : []),
      ...(after ? [{ type: 'added' as const, text: after }] : []),
    ];
  }

  const table = Array.from({ length: left.length + 1 }, () => new Uint16Array(right.length + 1));
  for (let i = left.length - 1; i >= 0; i -= 1) {
    for (let j = right.length - 1; j >= 0; j -= 1) {
      table[i][j] = left[i] === right[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }

  const parts: TextPart[] = [];
  const append = (type: TextPart['type'], text: string) => {
    const last = parts.at(-1);
    if (last?.type === type) last.text += text;
    else parts.push({ type, text });
  };
  let i = 0;
  let j = 0;
  while (i < left.length || j < right.length) {
    if (i < left.length && j < right.length && left[i] === right[j]) {
      append('same', left[i]);
      i += 1;
      j += 1;
    } else if (j < right.length && (i === left.length || table[i][j + 1] >= table[i + 1][j])) {
      append('added', right[j]);
      j += 1;
    } else {
      append('removed', left[i]);
      i += 1;
    }
  }
  return parts;
}

function TextDiff({ before, after }: { before: string; after: string }) {
  const parts = diffText(before, after);
  const beforeParts = parts.filter((part) => part.type !== 'added');
  const afterParts = parts.filter((part) => part.type !== 'removed');
  return (
    <div className="draft-text-diff">
      <div className="draft-text-before">
        <span className="diff-side-label">変更前</span>
        <span>{beforeParts.length === 0 ? 'なし' : beforeParts.map((part, index) =>
          part.type === 'removed' ? <del key={index}>{part.text}</del> : <span key={index}>{part.text}</span>,
        )}</span>
      </div>
      <div className="draft-text-after">
        <span className="diff-side-label">変更後</span>
        <span>{afterParts.length === 0 ? 'なし' : afterParts.map((part, index) =>
          part.type === 'added' ? <ins key={index}>{part.text}</ins> : <span key={index}>{part.text}</span>,
        )}</span>
      </div>
    </div>
  );
}

function DraftChanges({ draft }: { draft: Draft }) {
  const original = draft.original;
  if (!original) return null;
  const changes = changesFor(draft);
  const nameChanged = original.name !== draft.name;

  return (
    <div className="draft-changes">
      {draft.hasNewImage && (
        <div className="draft-image-diff" aria-label="画像の変更">
          <figure>
            <img src={original.imageDisplayUrl} alt={`${original.name}の現在の画像`} loading="lazy" />
            <figcaption>現在</figcaption>
          </figure>
          <span className="diff-arrow" aria-hidden="true">→</span>
          <figure>
            <img src={draft.imageDisplayUrl} alt={`${draft.name}の変更後の画像`} loading="lazy" />
            <figcaption>変更後</figcaption>
          </figure>
        </div>
      )}
      {changes.map((change) => (
        <div className="draft-change" key={change.key}>
          <span className="draft-change-label">{change.label}</span>
          {change.text ? (
            <TextDiff before={change.before} after={change.after} />
          ) : (
            <div className="draft-value-diff">
              <del>{change.before}</del>
              <span className="diff-arrow" aria-hidden="true">→</span>
              <ins>{change.after}</ins>
            </div>
          )}
        </div>
      ))}
      {!nameChanged && changes.length === 0 && !draft.hasNewImage && <span className="hint">変更はありません。</span>}
    </div>
  );
}

export function DraftQueue({ drafts, onChanged, onSubmitted }: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  if (drafts.length === 0) {
    return (
      <div className="draft-queue empty">
        <p className="hint">下書きはまだありません。カードを入力して「下書きに追加」を押すとここに溜まります。</p>
      </div>
    );
  }

  async function remove(id: number) {
    setError('');
    try {
      await api.deleteDraft(id);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    }
  }

  async function submitAll() {
    setBusy(true);
    setError('');
    try {
      const result = await api.submitDrafts();
      onSubmitted(result);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="draft-queue">
      <ul>
        {drafts.map((d) => {
          const nameChanged = d.original && d.original.name !== d.name;
          return (
            <li key={d.id}>
              <div className="draft-summary">
                {!(d.original && d.hasNewImage) && <img className="draft-thumb" src={d.imageDisplayUrl} alt="" loading="lazy" />}
                <div className="draft-info">
                  <span className="draft-kind">{kindLabels[d.kind] ?? d.kind}</span>
                  <span className="draft-name">
                    {nameChanged ? (
                      <><del>{d.original?.name}</del><span className="diff-arrow" aria-hidden="true">→</span><ins>{d.name}</ins></>
                    ) : d.name}
                  </span>
                  {!d.isEdit && <span className="draft-action">新規カード</span>}
                  {d.newFruit && <span className="draft-taxonomy">新しいフルーツ：{d.newFruit.label}</span>}
                  {d.newSweetType && <span className="draft-taxonomy">新しいお菓子タイプ：{d.newSweetType.label}</span>}
                </div>
                <button type="button" className="draft-remove" onClick={() => remove(d.id)} aria-label={`${d.name}を下書きから削除`}>
                  ×
                </button>
              </div>
              {d.isEdit && <DraftChanges draft={d} />}
            </li>
          );
        })}
      </ul>

      {error && <p className="error">{error}</p>}

      <button type="button" className="submit-all" disabled={busy} onClick={submitAll}>
        {busy ? '送信中…' : `まとめて送信する（${drafts.length}件）`}
      </button>
      <p className="hint">{drafts.length}件のカードがまとめて実装担当者に送られ、確認され次第ゲームに反映されます。</p>
    </div>
  );
}
