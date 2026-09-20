/**
 * API の型は internal/api から生成する (`mise run gen-types`)。
 * ここに書いてよいのは、サーバーに存在しないフロント固有の型だけ。
 */
export type {
  Card,
  CardsResponse,
  Dataset,
  Draft,
  DraftsResponse,
  ErrorResponse,
  Me,
  Metadata,
  Submission,
  SubmissionCard,
  SubmissionStatus,
  SubmitDraftsRequest,
  SubmitPayload,
  SubmissionsResponse,
  UsersResponse,
  SubmitResult,
  Role,
  User,
} from './types.generated';

/** データセットの分岐に使う UI 側の型。値は /api/datasets が返すものと同じ。 */
export type Kind = 'yojo' | 'sweet' | 'playable' | 'tokenYojo';

/** カード一覧の表示形式。 */
export type ViewMode = 'grid' | 'list';

/** フォームの編集中の状態。送信時に SubmitPayload へ変換される。 */
export interface CardFormValues {
  kind: Kind;
  id: string;
  name: string;
  fruit: string;
  description: string;
  cost: number;
  hp: number;
  attack: number;
  effect: string;
  role: string;
  sweetType: string;
  version: string;
  imageSlug: string;
}
