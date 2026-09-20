import type { Submission } from '../types';

const statusLabels: Record<Submission['status'], string> = {
  pr_open: 'レビュー待ち',
  merged: 'マージ済み',
  closed: 'クローズ',
};

export function Submissions({ submissions }: { submissions: Submission[] }) {
  if (submissions.length === 0) {
    return <p className="hint">まだ提出履歴はありません。</p>;
  }
  return (
    <table className="submissions">
      <thead>
        <tr>
          <th>カード</th>
          <th>提出者</th>
          <th>状態</th>
          <th>PR</th>
        </tr>
      </thead>
      <tbody>
        {submissions.map((s) => (
          <tr key={s.id}>
            <td>
              {s.cards.map((c) => (
                <div key={`${c.kind}-${c.cardId}`}>
                  {c.cardId} {c.cardName}
                  {c.isEdit && <span className="hint"> (更新)</span>}
                </div>
              ))}
            </td>
            <td>{s.displayName}</td>
            <td>
              <span className={`status status-${s.status}`}>{statusLabels[s.status] ?? s.status}</span>
            </td>
            <td>
              <a href={s.prUrl} target="_blank" rel="noreferrer">
                #{s.prNumber}
              </a>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
