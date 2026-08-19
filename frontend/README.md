これは [`create-next-app`](https://nextjs.org/docs/app/api-reference/cli/create-next-app) で作成した [Next.js](https://nextjs.org) プロジェクトです。

## セットアップ

開発サーバーを起動:

```bash
npm run dev
```

ブラウザで [http://localhost:3000](http://localhost:3000) を開いて確認してください。

`app/page.tsx` を編集するとページが自動更新されます。

## 環境変数

- `GO_API_URL` — Go バックエンド API のベース URL（例: `http://localhost:8080`）。本番環境では必須で、`NODE_ENV=production` の状態で未設定だと起動時（`next build`時を含む）にエラーになります。

このプロジェクトは [`next/font`](https://nextjs.org/docs/app/building-your-application/optimizing/fonts) を使い、Vercel のフォントファミリーである [Geist](https://vercel.com/font) を自動的に最適化・読み込みしています。

## 参考リンク

Next.js についてさらに学ぶには、以下のリソースを参照してください:

- [Next.js Documentation](https://nextjs.org/docs) — Next.js の機能や API について
- [Learn Next.js](https://nextjs.org/learn) — インタラクティブな Next.js チュートリアル

[Next.js の GitHub リポジトリ](https://github.com/vercel/next.js) もあわせてご覧ください。
