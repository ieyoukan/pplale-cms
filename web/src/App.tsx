import { useCallback, useEffect, useState } from 'react';
import { ApiError, api } from './api';
import { CardForm, emptyValues, fromCard } from './components/CardForm';
import { CardList } from './components/CardList';
import { DraftQueue } from './components/DraftQueue';
import { Submissions } from './components/Submissions';
import { Users } from './components/Users';
import type { Card, CardFormValues, Draft, Kind, Me, Metadata, Submission, SubmitResult, User } from './types';

type Tab = 'cards' | 'history' | 'users';

export function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);
  const [metadata, setMetadata] = useState<Metadata | null>(null);
  const [kind, setKind] = useState<Kind>('yojo');
  const [cards, setCards] = useState<Card[]>([]);
  const [nextId, setNextId] = useState('');
  const [values, setValues] = useState<CardFormValues>(emptyValues('yojo'));
  const [editingCard, setEditingCard] = useState<Card | null>(null);
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const [submissions, setSubmissions] = useState<Submission[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [tab, setTab] = useState<Tab>('cards');
  const [banner, setBanner] = useState<SubmitResult | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    api
      .me()
      .then(setMe)
      .catch((err) => {
        if (!(err instanceof ApiError && err.status === 401)) {
          setError(String(err));
        }
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (!me) return;
    api.metadata().then(setMetadata).catch((err) => setError(String(err)));
  }, [me]);

  const loadCards = useCallback((target: Kind) => {
    api
      .cards(target)
      .then((data) => {
        setCards(data.cards);
        setNextId(data.nextId);
      })
      .catch((err) => setError(String(err)));
  }, []);

  useEffect(() => {
    if (!me) return;
    loadCards(kind);
  }, [me, kind, loadCards]);

  const loadDrafts = useCallback(() => {
    api.drafts().then((d) => setDrafts(d.drafts ?? [])).catch((err) => setError(String(err)));
  }, []);

  useEffect(() => {
    if (!me || !me.canSubmit) return;
    loadDrafts();
  }, [me, loadDrafts]);

  const loadSubmissions = useCallback(() => {
    api.submissions().then((d) => setSubmissions(d.submissions ?? [])).catch((err) => setError(String(err)));
  }, []);

  const loadUsers = useCallback(() => {
    api.users().then((d) => setUsers(d.users ?? [])).catch((err) => setError(String(err)));
  }, []);

  useEffect(() => {
    if (!me) return;
    if (tab === 'history') loadSubmissions();
    if (tab === 'users' && me.canManageUsers) loadUsers();
  }, [me, tab, loadSubmissions, loadUsers]);

  if (loading) {
    return <main className="app">読み込み中…</main>;
  }

  if (!me) {
    return (
      <main className="app login">
        <h1>PPLALE CMS</h1>
        <p>カードの追加・編集には Discord ログインが必要です。</p>
        <a className="login-button" href="/auth/login">
          Discord でログイン
        </a>
      </main>
    );
  }

  return (
    <main className="app">
      <header>
        <h1>PPLALE CMS</h1>
        <div className="who">
          <span>
            {me.displayName} {me.role && <em>({me.role})</em>}
          </span>
          <button type="button" onClick={() => api.logout().then(() => setMe(null))}>
            ログアウト
          </button>
        </div>
      </header>

      {error && <p className="error">{error}</p>}

      {!me.canSubmit && (
        <p className="notice">
          カードを提出する権限がありません。管理者に Discord ユーザーID <code>{me.discordId}</code> の許可を依頼してください。
        </p>
      )}

      <nav className="tabs">
        <button type="button" className={tab === 'cards' ? 'active' : ''} onClick={() => setTab('cards')}>
          カード
        </button>
        <button type="button" className={tab === 'history' ? 'active' : ''} onClick={() => setTab('history')}>
          提出履歴
        </button>
        {me.canManageUsers && (
          <button type="button" className={tab === 'users' ? 'active' : ''} onClick={() => setTab('users')}>
            許可リスト
          </button>
        )}
      </nav>

      {banner && (
        <p className="success">
          PR を作成しました:{' '}
          <a href={banner.prUrl} target="_blank" rel="noreferrer">
            #{banner.prNumber}
          </a>
          {banner.submission && (
            <>
              {' '}
              ({banner.submission.cards.map((c) => c.cardName).join(' / ')})
            </>
          )}
          <br />
          <small>{banner.files.join(' / ')}</small>
        </p>
      )}

      {tab === 'cards' && metadata && (
        <div className="cards-tab">
          <nav className="kinds">
            {metadata.datasets.map((ds) => (
              <button
                key={ds.kind}
                type="button"
                className={ds.kind === kind ? 'active' : ''}
                onClick={() => {
                  const next = ds.kind as Kind;
                  setKind(next);
                  setValues(emptyValues(next));
                  setEditingCard(null);
                }}
              >
                {ds.label}
              </button>
            ))}
          </nav>

          <div className="columns">
            <CardList
              cards={cards}
              onEdit={(card) => {
                setValues(fromCard(kind, card));
                setEditingCard(card);
              }}
            />
            <div className="editor-column">
              {values.id && (
                <button
                  type="button"
                  className="new-card"
                  onClick={() => {
                    setValues(emptyValues(kind));
                    setEditingCard(null);
                  }}
                >
                  新規カードに切り替える
                </button>
              )}
              {me.canSubmit && (
                <CardForm
                  metadata={metadata}
                  kind={kind}
                  nextId={nextId}
                  values={values}
                  currentImageUrl={editingCard?.imageDisplayUrl}
                  onChange={setValues}
                  onQueued={() => {
                    setValues(emptyValues(kind));
                    setEditingCard(null);
                    loadDrafts();
                  }}
                />
              )}
              {me.canSubmit && (
                <section className="drafts-section">
                  <h2>下書き{drafts.length > 0 ? `（${drafts.length}）` : ''}</h2>
                  <DraftQueue
                    drafts={drafts}
                    onChanged={loadDrafts}
                    onSubmitted={(result) => {
                      setBanner(result);
                      loadCards(kind);
                    }}
                  />
                </section>
              )}
            </div>
          </div>
        </div>
      )}

      {tab === 'history' && <Submissions submissions={submissions} />}

      {tab === 'users' && me.canManageUsers && (
        <Users users={users} currentDiscordId={me.discordId} onChanged={loadUsers} />
      )}
    </main>
  );
}
