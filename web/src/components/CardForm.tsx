import { useMemo, useState } from 'react';
import { ApiError, api } from '../api';
import type { Card, CardFormValues, Draft, Kind, Metadata } from '../types';

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

export function emptyValues(kind: Kind): CardFormValues {
  return {
    kind,
    id: '',
    name: '',
    // 「全種(all)」はプレイアブルカードだけで使う値。幼女・お菓子・トークン幼女は
    // 必ず特定のフルーツに属する。
    fruit: kind === 'playable' ? 'all' : 'strawberry',
    description: '',
    cost: 0,
    hp: 0,
    attack: 0,
    effect: '',
    role: '',
    sweetType: '',
    version: 'normal',
  };
}

export function fromCard(kind: Kind, card: Card): CardFormValues {
  return {
    kind,
    id: card.id,
    name: card.name,
    fruit: card.fruit,
    description: card.description,
    cost: card.cost,
    hp: card.hp,
    attack: card.attack,
    effect: card.effect ?? '',
    role: card.role ?? '',
    sweetType: card.sweetType ?? '',
    version: card.version ?? 'normal',
  };
}

interface Props {
  metadata: Metadata;
  kind: Kind;
  nextId: string;
  values: CardFormValues;
  /** 編集中カードの現在の画像。新規アップロードを選ぶまではこれを表示する。 */
  currentImageUrl?: string;
  onChange: (values: CardFormValues) => void;
  onQueued: (draft: Draft) => void;
}

export function CardForm({ metadata, kind, nextId, values, currentImageUrl, onChange, onQueued }: Props) {
  const [image, setImage] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  const isEdit = values.id !== '';
  const preview = useMemo(() => (image ? URL.createObjectURL(image) : ''), [image]);
  const displayImage = preview || (isEdit ? currentImageUrl : '');
  // 「全種(all)」は実データ上プレイアブルカードにしか存在しない。他のカードは
  // 必ずどれかのフルーツに属するので、選択肢自体から外す。
  const fruitOptions = kind === 'playable' ? metadata.fruits : metadata.fruits.filter((f) => f !== 'all');

  const set = <K extends keyof CardFormValues>(key: K, value: CardFormValues[K]) =>
    onChange({ ...values, [key]: value });

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError('');
    setFieldErrors({});
    try {
      const draft = await api.createDraft({ ...values, kind }, image);
      setImage(null);
      onQueued(draft);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
        setFieldErrors(err.fields);
      } else {
        setError(String(err));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card-form" onSubmit={handleSubmit}>
      {!isEdit && <p className="hint">{nextId} として登録されます。</p>}

      <label className="image-picker">
        <input
          type="file"
          className="image-picker-input"
          accept="image/png,image/jpeg,image/webp"
          onChange={(e) => setImage(e.target.files?.[0] ?? null)}
        />
        {displayImage ? (
          <img className="image-picker-preview" src={displayImage} alt="" />
        ) : (
          <div className="image-picker-placeholder">画像を追加</div>
        )}
        <div className="image-picker-overlay">{displayImage ? '画像を変更' : '画像を追加'}</div>
      </label>
      <FieldError message={fieldErrors.imageUrl} />
      <p className="hint">
        {isEdit
          ? '画像にカーソルを合わせると変更できます。選ばない場合は今の画像のままです。'
          : '画像にカーソルを合わせて追加してください。'}
        {' '}
        幅800pxのWebPに自動で変換されます。
      </p>

      <label>
        カード名
        <input value={values.name} onChange={(e) => set('name', e.target.value)} required />
        <FieldError message={fieldErrors.name} />
      </label>

      <label>
        フルーツ
        <select value={values.fruit} onChange={(e) => set('fruit', e.target.value)}>
          {fruitOptions.map((fruit) => (
            <option key={fruit} value={fruit}>
              {fruitLabels[fruit] ?? fruit}
            </option>
          ))}
        </select>
        <FieldError message={fieldErrors.fruit} />
      </label>

      <div className="stat-row">
        <label>
          コスト
          <input type="number" min={0} value={values.cost} onChange={(e) => set('cost', Number(e.target.value))} />
          <FieldError message={fieldErrors.cost} />
        </label>
        <label>
          攻撃
          <input type="number" min={0} value={values.attack} onChange={(e) => set('attack', Number(e.target.value))} />
          <FieldError message={fieldErrors.attack} />
        </label>
        <label>
          体力
          <input type="number" min={0} value={values.hp} onChange={(e) => set('hp', Number(e.target.value))} />
          <FieldError message={fieldErrors.hp} />
        </label>
      </div>

      {(kind === 'yojo' || kind === 'tokenYojo') && (
        <label>
          役職
          <select value={values.role} onChange={(e) => set('role', e.target.value)}>
            {metadata.roles.map((role) => (
              <option key={role} value={role}>
                {roleLabels[role] ?? role}
              </option>
            ))}
          </select>
        </label>
      )}

      {kind === 'sweet' && (
        <label>
          お菓子タイプ
          <select value={values.sweetType} onChange={(e) => set('sweetType', e.target.value)}>
            {metadata.sweetTypes.map((type) => (
              <option key={type} value={type}>
                {sweetLabels[type] ?? type}
              </option>
            ))}
          </select>
          <FieldError message={fieldErrors.sweetType} />
        </label>
      )}

      {kind === 'playable' && (
        <label>
          バージョン
          <select value={values.version} onChange={(e) => set('version', e.target.value)}>
            {metadata.versions.map((version) => (
              <option key={version} value={version}>
                {version}
              </option>
            ))}
          </select>
        </label>
      )}

      <label>
        効果
        <textarea rows={4} value={values.effect} onChange={(e) => set('effect', e.target.value)} />
      </label>

      {kind === 'playable' && (
        <label>
          説明
          <textarea rows={2} value={values.description} onChange={(e) => set('description', e.target.value)} />
        </label>
      )}

      {error && <p className="error">{error}</p>}

      <button type="submit" disabled={busy}>
        {busy ? '追加中…' : '下書きに追加'}
      </button>
      <p className="hint">
        下書きに追加されるだけで、まだ PPLALE-web には送られません。下書き一覧から「まとめて送信」を押すと、
        たまっているカードをまとめて実装担当者に送ります。
      </p>
    </form>
  );
}

function FieldError({ message }: { message?: string }) {
  if (!message) return null;
  return <span className="field-error">{message}</span>;
}
