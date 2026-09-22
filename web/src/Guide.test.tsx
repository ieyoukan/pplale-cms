import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Guide } from './Guide';

describe('Guide', () => {
  it('explains the update request flow in non-technical language', () => {
    render(<Guide />);

    expect(screen.getByRole('heading', { name: /カードの変更を/ })).toBeTruthy();
    expect(screen.getByText('カードを選ぶ')).toBeTruthy();
    expect(screen.getByText('内容を入力する')).toBeTruthy();
    expect(screen.getByText('下書きで確認する')).toBeTruthy();
    expect(screen.getByText('更新を依頼する')).toBeTruthy();
    expect(screen.getByText(/プログラムやファイルの知識は必要ありません/)).toBeTruthy();
  });

  it('links to login and back to the CMS', () => {
    render(<Guide />);

    expect(screen.getByRole('link', { name: /Discordでログイン/ }).getAttribute('href')).toBe('/auth/login');
    expect(screen.getByRole('link', { name: 'CMSへ戻る' }).getAttribute('href')).toBe('/');
  });
});
