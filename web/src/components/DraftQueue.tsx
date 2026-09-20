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
        {drafts.map((d) => (
          <li key={d.id}>
            <img className="draft-thumb" src={d.imageDisplayUrl} alt="" loading="lazy" />
            <div className="draft-info">
              <span className="draft-kind">{kindLabels[d.kind] ?? d.kind}</span>
              <span className="draft-name">{d.name}</span>
              <span className="draft-action">{d.isEdit ? '更新' : '新規'}</span>
            </div>
            <button type="button" className="draft-remove" onClick={() => remove(d.id)} aria-label={`${d.name}を下書きから削除`}>
              ×
            </button>
          </li>
        ))}
      </ul>

      {error && <p className="error">{error}</p>}

      <button type="button" className="submit-all" disabled={busy} onClick={submitAll}>
        {busy ? '送信中…' : `まとめて送信する（${drafts.length}件）`}
      </button>
      <p className="hint">{drafts.length}件のカードがまとめて実装担当者に送られ、確認され次第ゲームに反映されます。</p>
    </div>
  );
}
