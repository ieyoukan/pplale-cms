import { describe, expect, it } from 'vitest';
import { csrfToken, toPayload } from './api';
import { emptyValues } from './components/CardForm';

describe('csrfToken', () => {
  it('reads the token the server mirrors into a readable cookie', () => {
    expect(csrfToken('pplale_cms_csrf=abc123')).toBe('abc123');
    expect(csrfToken('other=1; pplale_cms_csrf=abc123; more=2')).toBe('abc123');
    expect(csrfToken('pplale_cms_csrf=a%2Fb')).toBe('a/b');
  });

  it('returns an empty string when the cookie is absent', () => {
    expect(csrfToken('')).toBe('');
    expect(csrfToken('session=xyz')).toBe('');
  });

  // The session cookie is HttpOnly and must not be reachable here.
  it('does not pick up the session cookie', () => {
    expect(csrfToken('pplale_cms_session=secret')).toBe('');
  });
});

describe('toPayload', () => {
  it('sends role only for yojo datasets', () => {
    const yojo = toPayload({ ...emptyValues('yojo'), role: 'manager' });
    expect(yojo.role).toBe('manager');
    expect(yojo.sweetType).toBeUndefined();
    expect(yojo.version).toBeUndefined();

    const token = toPayload({ ...emptyValues('tokenYojo'), role: '' });
    expect(token.role).toBe('');
  });

  it('sends sweetType only for sweet datasets', () => {
    const sweet = toPayload({ ...emptyValues('sweet'), sweetType: 'cake' });
    expect(sweet.sweetType).toBe('cake');
    expect(sweet.role).toBeUndefined();
  });

  it('defaults playable cards to the normal version', () => {
    const playable = toPayload({ ...emptyValues('playable'), version: '' });
    expect(playable.version).toBe('normal');
    expect(playable.role).toBeUndefined();
    expect(playable.sweetType).toBeUndefined();
  });

  it('always carries the identifying fields', () => {
    const payload = toPayload({
      ...emptyValues('yojo'),
      id: 'y_3',
      name: 'かがり',
      fruit: 'strawberry',
      cost: 2,
      hp: 3,
      attack: 1,
    });
    expect(payload).toMatchObject({
      kind: 'yojo',
      id: 'y_3',
      name: 'かがり',
      fruit: 'strawberry',
      cost: 2,
      hp: 3,
      attack: 1,
    });
  });
});
