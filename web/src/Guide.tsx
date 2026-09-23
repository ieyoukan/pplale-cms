const steps = [
  {
    number: '1',
    title: 'カードを選ぶ',
    body: '直したいカードを一覧から選びます。新しいカードを作るときは「カードを追加」を選びます。',
  },
  {
    number: '2',
    title: '内容を入力する',
    body: '名前、説明、画像、数値などを編集します。新しいフルーツ分類やお菓子タイプも、この画面から追加できます。',
  },
  {
    number: '3',
    title: '下書きで確認する',
    body: '変更前と変更後を見比べます。ほかのカードも続けて編集し、まとめて下書きに置けます。',
  },
  {
    number: '4',
    title: '更新を依頼する',
    body: '「まとめて送信する」を押すと依頼は完了です。確認が終わるとゲームへ反映されます。',
  },
];

export function Guide() {
  return (
    <main className="guide-page">
      <header className="guide-header">
        <a className="guide-brand" href="/">
          PPLALE CMS
        </a>
        <a className="guide-back" href="/">
          CMSへ戻る
        </a>
      </header>

      <section className="guide-intro">
        <div>
          <p className="guide-eyebrow">はじめての方へ</p>
          <h1>カードの変更を、画面からかんたんに依頼できます</h1>
          <p className="guide-lead">
            PPLALE CMSでは、一覧からカードを選んで内容を書き換えるだけで更新を依頼できます。
            プログラムやファイルの知識は必要ありません。
          </p>
          <a className="guide-primary-link" href="/auth/login">
            Discordでログインして始める
          </a>
        </div>
        <aside className="guide-access-note">
          <strong>ログインする前に</strong>
          <span>管理者にDiscordユーザーIDを伝え、利用者として追加してもらってください。</span>
        </aside>
      </section>

      <section className="guide-section" aria-labelledby="guide-flow-title">
        <div className="guide-section-heading">
          <p className="guide-eyebrow">基本の流れ</p>
          <h2 id="guide-flow-title">4つの手順で更新を依頼</h2>
        </div>
        <ol className="guide-steps">
          {steps.map((step) => (
            <li key={step.number}>
              <span className="guide-step-number">{step.number}</span>
              <div>
                <h3>{step.title}</h3>
                <p>{step.body}</p>
              </div>
            </li>
          ))}
        </ol>
      </section>

      <section className="guide-section" aria-labelledby="guide-screen-title">
        <div className="guide-section-heading">
          <p className="guide-eyebrow">画面イメージ</p>
          <h2 id="guide-screen-title">一覧と編集欄を見ながら進めます</h2>
          <p>実際の画面では、左でカードを選び、右で内容と下書きを確認します。</p>
        </div>

        <figure className="guide-figure">
          <div className="guide-preview-scroll">
            <div className="guide-preview" aria-label="カード編集画面の見本">
              <div className="preview-topbar">
                <strong>PPLALE CMS</strong>
                <span>あなたの名前</span>
              </div>
              <div className="preview-main-tabs">
                <span className="selected">カード</span>
                <span>提出履歴</span>
              </div>
              <div className="preview-kind-tabs">
                <span className="selected">幼女</span>
                <span>お菓子</span>
                <span>プレイアブル</span>
              </div>
              <div className="preview-columns">
                <div className="preview-card-area">
                  <div className="preview-toolbar">
                    <span>カード名 / IDで検索</span>
                    <span className="preview-toggle">グリッド&nbsp;&nbsp; リスト</span>
                  </div>
                  <div className="preview-card-grid">
                    <div className="preview-add-card">
                      <b>＋</b>
                      <span>カードを追加</span>
                    </div>
                    <div className="preview-card preview-card-pink">
                      <span>かがり</span>
                      <small>1 / 1 / 1</small>
                    </div>
                    <div className="preview-card preview-card-blue">
                      <span>とここ</span>
                      <small>2 / 2 / 2</small>
                    </div>
                    <div className="preview-card preview-card-gold">
                      <span>カード名</span>
                      <small>3 / 2 / 4</small>
                    </div>
                  </div>
                  <div className="guide-marker marker-one">1</div>
                </div>

                <div className="preview-editor">
                  <div className="preview-editor-heading">
                    <strong>カードを編集</strong>
                    <span>閉じる</span>
                  </div>
                  <div className="preview-form-layout">
                    <div className="preview-image">画像を選ぶ</div>
                    <div className="preview-fields">
                      <label>
                        カード名
                        <span className="preview-input">かがり</span>
                      </label>
                      <label>
                        説明
                        <span className="preview-input preview-textarea">カードの説明を入力</span>
                      </label>
                      <div className="preview-stat-fields">
                        <span>コスト&nbsp; 1</span>
                        <span>攻撃&nbsp; 1</span>
                        <span>体力&nbsp; 1</span>
                      </div>
                    </div>
                  </div>
                  <span className="preview-primary-button">下書きに追加</span>
                  <div className="guide-marker marker-two">2</div>

                  <div className="preview-drafts">
                    <strong>下書き（1）</strong>
                    <div className="preview-draft-row">
                      <span className="preview-draft-thumb" />
                      <span>
                        <small>幼女</small>
                        <b>かがり</b>
                      </span>
                      <em>変更あり</em>
                    </div>
                    <span className="preview-submit-button">まとめて送信する（1件）</span>
                    <div className="guide-marker marker-three">3</div>
                    <div className="guide-marker marker-four">4</div>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <figcaption>説明用の画面イメージです。表示される入力項目はカードの種類によって変わります。</figcaption>
        </figure>
      </section>

      <section className="guide-section guide-after-submit" aria-labelledby="guide-after-title">
        <div>
          <p className="guide-eyebrow">送信したあと</p>
          <h2 id="guide-after-title">依頼内容は確認後に反映されます</h2>
        </div>
        <div className="guide-after-flow" aria-label="送信後の流れ">
          <span>あなたが更新を依頼</span>
          <b aria-hidden="true">→</b>
          <span>実装担当者が確認</span>
          <b aria-hidden="true">→</b>
          <span>ゲームに反映</span>
        </div>
        <p>
          送信した時点では、まだゲームのカードは変わりません。提出履歴から確認状況を見られます。
        </p>
      </section>
    </main>
  );
}
