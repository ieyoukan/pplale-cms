import type {
  CardFormValues,
  CardsResponse,
  Me,
  Metadata,
  SubmissionsResponse,
  SubmitPayload,
  SubmitResult,
  UsersResponse,
} from './types';

export class ApiError extends Error {
  readonly status: number;
  readonly fields: Record<string, string>;

  constructor(status: number, message: string, fields: Record<string, string> = {}) {
    super(message);
    this.status = status;
    this.fields = fields;
  }
}

/** The CSRF token is mirrored into a readable cookie when the session is issued. */
export function csrfToken(cookie: string = document.cookie): string {
  const match = cookie.match(/(?:^|;\s*)pplale_cms_csrf=([^;]*)/);
  return match ? decodeURIComponent(match[1]) : '';
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.method && init.method !== 'GET') {
    headers.set('X-CSRF-Token', csrfToken());
  }

  const response = await fetch(path, { ...init, headers, credentials: 'same-origin' });
  const text = await response.text();
  const body = text ? JSON.parse(text) : {};

  if (!response.ok) {
    throw new ApiError(response.status, body.error ?? `リクエストに失敗しました (${response.status})`, body.fields ?? {});
  }
  return body as T;
}

export const api = {
  me: () => request<Me>('/api/me'),
  metadata: () => request<Metadata>('/api/datasets'),
  cards: (kind: string) => request<CardsResponse>(`/api/cards?kind=${encodeURIComponent(kind)}`),
  submissions: () => request<SubmissionsResponse>('/api/submissions'),
  users: () => request<UsersResponse>('/api/users'),

  logout: () => request<{ status: string }>('/auth/logout', { method: 'POST' }),

  upsertUser: (discordId: string, displayName: string, role: string) =>
    request<{ status: string }>(`/api/users/${encodeURIComponent(discordId)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ displayName, role }),
    }),

  deleteUser: (discordId: string) =>
    request<{ status: string }>(`/api/users/${encodeURIComponent(discordId)}`, { method: 'DELETE' }),

  submit: (values: CardFormValues, image: File | null) => {
    const form = new FormData();
    form.append('payload', JSON.stringify(toPayload(values)));
    if (image) {
      form.append('image', image);
    }
    return request<SubmitResult>('/api/submissions', { method: 'POST', body: form });
  },
};

/**
 * Optional fields are only sent when the dataset actually uses them, so the
 * generated JSON keeps the same shape as its neighbours in PPLALE-web.
 */
export function toPayload(values: CardFormValues): SubmitPayload {
  const payload: SubmitPayload = {
    kind: values.kind,
    id: values.id,
    name: values.name,
    fruit: values.fruit,
    description: values.description,
    cost: values.cost,
    hp: values.hp,
    attack: values.attack,
    effect: values.effect,
    imageSlug: values.imageSlug,
  };
  if (values.kind === 'yojo' || values.kind === 'tokenYojo') {
    payload.role = values.role;
  }
  if (values.kind === 'sweet') {
    payload.sweetType = values.sweetType;
  }
  if (values.kind === 'playable') {
    payload.version = values.version || 'normal';
  }
  return payload;
}
