import { useState } from 'react';
import { api } from '../api';
import type { User } from '../types';

interface Props {
  users: User[];
  currentDiscordId: string;
  onChanged: () => void;
}

export function Users({ users, currentDiscordId, onChanged }: Props) {
  const [discordId, setDiscordId] = useState('');
  const [role, setRole] = useState('creator');
  const [error, setError] = useState('');

  async function add(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    try {
      await api.upsertUser(discordId.trim(), role);
      setDiscordId('');
      onChanged();
    } catch (err) {
      setError(String(err instanceof Error ? err.message : err));
    }
  }

  async function remove(target: User) {
    if (!confirm(`${target.displayName || target.discordId} を許可リストから削除しますか？`)) return;
    setError('');
    try {
      await api.deleteUser(target.discordId);
      onChanged();
    } catch (err) {
      setError(String(err instanceof Error ? err.message : err));
    }
  }

  return (
    <div className="users">
      <form onSubmit={add}>
        <h3>許可リストに追加 / 更新</h3>
        <p className="hint">表示名は本人がDiscordでログインすると自動で反映されます。</p>
        <label>
          Discord ユーザーID
          <input value={discordId} onChange={(e) => setDiscordId(e.target.value)} placeholder="123456789012345678" required />
        </label>
        <label>
          権限
          <select value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="creator">カード制作者 (creator)</option>
            <option value="admin">管理者 (admin)</option>
          </select>
        </label>
        <button type="submit">保存</button>
        {error && <p className="error">{error}</p>}
      </form>

      <table>
        <thead>
          <tr>
            <th>Discord ID</th>
            <th>Discord表示名</th>
            <th>権限</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {users.map((user) => (
            <tr key={user.discordId}>
              <td>{user.discordId}</td>
              <td>{user.displayName || '未ログイン'}</td>
              <td>{user.role}</td>
              <td>
                {user.discordId !== currentDiscordId && (
                  <button type="button" onClick={() => remove(user)}>
                    削除
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
