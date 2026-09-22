import { useCallback, useEffect, useState } from 'react';
import { ApiError, api } from './api';
import { CardForm, emptyValues, fromCard } from './components/CardForm';
import { CardList } from './components/CardList';
import { DraftQueue } from './components/DraftQueue';
import { Submissions } from './components/Submissions';
import { Users } from './components/Users';
import type { Card, CardFormValues, Draft, Kind, Me, Metadata, Submission, SubmitResult, User } from './types';

type Tab = 'cards' | 'history' | 'users';

function handleTabKeyDown(event: React.KeyboardEvent<HTMLButtonElement>) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;

  const tabs = Array.from(
    event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]') ?? [],
  );
  const current = tabs.indexOf(event.currentTarget);
  if (current < 0) return;

  event.preventDefault();
  const next =
    event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? tabs.length - 1
        : (current + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
  tabs[next]?.focus();
  tabs[next]?.click();
}

export function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);
  const [metadata, setMetadata] = useState<Metadata | null>(null);
  const [kind, setKind] = useState<Kind>('yojo');
  const [cards, setCards] = useState<Card[]>([]);
  const [nextId, setNextId] = useState('');
  const [values, setValues] = useState<CardFormValues>(emptyValues('yojo'));
  const [editingCard, setEditingCard] = useState<Card | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
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
        <p>許可された Discord ユーザーのみログインできます。</p>
        <a className="login-button" href="/auth/login">
          Discord でログイン
        </a>
        <a className="login-guide-link" href="/guide">
          はじめての方へ：使い方を見る
        </a>
      </main>
    );
  }

  return (
    <main className="app">
      <header>
        <h1>PPLALE CMS</h1>
        <div className="who">
          <a className="guide-link" href="/guide">
            使い方
          </a>
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

      <nav className="tabs" role="tablist" aria-label="管理画面">
        <button
          id="tab-cards"
          type="button"
          role="tab"
          aria-selected={tab === 'cards'}
          aria-controls="panel-cards"
          tabIndex={tab === 'cards' ? 0 : -1}
          className={tab === 'cards' ? 'active' : ''}
          onKeyDown={handleTabKeyDown}
          onClick={() => setTab('cards')}
        >
          カード
        </button>
        <button
          id="tab-history"
          type="button"
          role="tab"
          aria-selected={tab === 'history'}
          aria-controls="panel-history"
          tabIndex={tab === 'history' ? 0 : -1}
          className={tab === 'history' ? 'active' : ''}
          onKeyDown={handleTabKeyDown}
          onClick={() => setTab('history')}
        >
          提出履歴
        </button>
        {me.canManageUsers && (
          <button
            id="tab-users"
            type="button"
            role="tab"
            aria-selected={tab === 'users'}
            aria-controls="panel-users"
            tabIndex={tab === 'users' ? 0 : -1}
            className={tab === 'users' ? 'active' : ''}
            onKeyDown={handleTabKeyDown}
            onClick={() => setTab('users')}
          >
            許可リスト
          </button>
        )}
      </nav>

      {banner && (
        <p className="success">
          実装担当者に送信しました
          {banner.submission && banner.submission.cards.length > 0 && (
            <>：{banner.submission.cards.map((c) => c.cardName).join(' / ')}</>
          )}
          <br />
          <small>
            確認が完了するとゲームに反映されます。
            <a href={banner.prUrl} target="_blank" rel="noreferrer">
              変更内容を見る
            </a>
          </small>
        </p>
      )}

      {tab === 'cards' && metadata && (
        <section id="panel-cards" className="cards-tab" role="tabpanel" aria-labelledby="tab-cards">
          <nav className="kinds" role="tablist" aria-label="カード種別">
            {metadata.datasets.map((ds) => (
              <button
                key={ds.kind}
                id={`kind-tab-${ds.kind}`}
                type="button"
                role="tab"
                aria-selected={ds.kind === kind}
                aria-controls="kind-panel"
                tabIndex={ds.kind === kind ? 0 : -1}
                className={ds.kind === kind ? 'active' : ''}
                onKeyDown={handleTabKeyDown}
                onClick={() => {
                  const next = ds.kind as Kind;
                  setKind(next);
                  setValues(emptyValues(next));
                  setEditingCard(null);
                  setEditorOpen(false);
                }}
              >
                {ds.label}
              </button>
            ))}
          </nav>

          <div id="kind-panel" className="columns" role="tabpanel" aria-labelledby={`kind-tab-${kind}`}>
            <CardList
              cards={cards}
              onEdit={(card) => {
                setValues(fromCard(kind, card));
                setEditingCard(card);
                setEditorOpen(true);
              }}
              onAdd={
                me.canSubmit
                  ? () => {
                      setValues(emptyValues(kind));
                      setEditingCard(null);
                      setEditorOpen(true);
                    }
                  : undefined
              }
            />
            <div className="editor-column">
              {editorOpen && me.canSubmit && (
                <>
                  <button type="button" className="close-editor" onClick={() => setEditorOpen(false)}>
                    閉じる
                  </button>
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
                      setEditorOpen(false);
                      loadDrafts();
                    }}
                  />
                </>
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
        </section>
      )}

      {tab === 'history' && (
        <section id="panel-history" className="tab-panel" role="tabpanel" aria-labelledby="tab-history">
          <Submissions submissions={submissions} />
        </section>
      )}

      {tab === 'users' && me.canManageUsers && (
        <section id="panel-users" className="tab-panel" role="tabpanel" aria-labelledby="tab-users">
          <Users users={users} currentDiscordId={me.discordId} onChanged={loadUsers} />
        </section>
      )}
    </main>
  );
}
