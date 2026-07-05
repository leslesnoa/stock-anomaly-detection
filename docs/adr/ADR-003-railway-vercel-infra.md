# ADR-003: インフラにRailway + Vercelを採用

**ステータス**: 承認済み  
**日付**: 2026-07-05

## コンテキスト

Go・Python・PostgreSQL・Redisをホストするバックエンドインフラと、Next.jsをホストするフロントエンドインフラが必要。コストを抑えつつ、GitHubと連携した自動デプロイを実現したい。

## 決定

- バックエンド（Go・Python・PostgreSQL・Redis）: Railway
- フロントエンド（Next.js）: Vercel

## 理由

- 月$5〜10のコストでマネージドDB（PostgreSQL・Redis）を含む全スタックをデプロイ可能
- GitHubリポジトリとの自動デプロイ連携
- PostgreSQL・RedisがRailwayのネイティブPluginとして提供されており、接続設定が簡単
- Vercelは Next.js の開発元であり、デプロイ・プレビューが最も手軽

## 不採用案

- **GCP + Terraform**: インフラ管理コストが高く、アプリケーションの設計価値を示すというポートフォリオ目的に対してオーバースペック。将来的な移行は検討余地あり。
