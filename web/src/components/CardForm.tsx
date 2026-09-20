import { useMemo, useState } from 'react';
import { ApiError, api } from '../api';
import type { Card, CardFormValues, Kind, Metadata, SubmitResult } from '../types';

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
    fruit: 'all',
    description: '',
    cost: 0,
    hp: 0,
    attack: 0,
    effect: '',
    role: '',
    sweetType: '',
    version: 'normal',
    imageSlug: '',
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
    imageSlug: '',
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
  onSubmitted: (result: SubmitResult) => void;
}

export function CardForm({ metadata, kind, nextId, values, currentImageUrl, onChange, onSubmitted }: Props) {
  const [image, setImage] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  const isEdit = values.id !== '';
  const preview = useMemo(() => (image ? URL.createObjectURL(image) : ''), [image]);

  const set = <K extends keyof CardFormValues>(key: K, value: CardFormValues[K]) =>
    onChange({ ...values, [key]: value });

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError('');
    setFieldErrors({});
    try {
      const result = await api.submit({ ...values, kind }, image);
      setImage(null);
      onSubmitted(result);
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
      <h2>{isEdit ? `カードを編集 (${values.id})` : `新規カード (${nextId} として登録されます)`}</h2>

      <label>
        カード名
        <input value={values.name} onChange={(e) => set('name', e.target.value)} required />
        <FieldError message={fieldErrors.name} />
      </label>

      <label>
        フルーツ
        <select value={values.fruit} onChange={(e) => set('fruit', e.target.value)}>
          {metadata.fruits.map((fruit) => (
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

      <label>
        説明 (description)
        <textarea rows={2} value={values.description} onChange={(e) => set('description', e.target.value)} />
      </label>

      <fieldset>
        <legend>画像</legend>
        <p className="hint">
          アップロードした画像は幅800pxのWebP (quality 80) と、OGP用の幅240px PNG の2枚に変換され、同じPRに含まれます。
        </p>
        <input
          type="file"
          accept="image/png,image/jpeg,image/webp"
          onChange={(e) => setImage(e.target.files?.[0] ?? null)}
        />
        <label>
          画像ファイル名 (半角英数字・ハイフン・アンダースコア)
          <input
            value={values.imageSlug}
            onChange={(e) => set('imageSlug', e.target.value)}
            placeholder="ichigo_kagari"
            required={!isEdit}
          />
          <FieldError message={fieldErrors.imageUrl} />
        </label>
        {preview && <img className="preview" src={preview} alt="アップロード画像のプレビュー" />}
        {!preview && isEdit && currentImageUrl && (
          <>
            <img className="preview" src={currentImageUrl} alt="現在の画像" />
            <p className="hint">画像を選ばない場合は現在の画像がそのまま使われます。</p>
          </>
        )}
      </fieldset>

      {error && <p className="error">{error}</p>}

      <button type="submit" disabled={busy}>
        {busy ? 'PR を作成中…' : 'PR を作成する'}
      </button>
      <p className="hint">
        送信すると PPLALE-web に Pull Request が作成されます。main への直接反映は行われず、レビュー後にマージされます。
      </p>
    </form>
  );
}

function FieldError({ message }: { message?: string }) {
  if (!message) return null;
  return <span className="field-error">{message}</span>;
}
