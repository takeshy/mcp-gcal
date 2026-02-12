# mcp-gcal

Google カレンダーと Gmail に対応したスタンドアロン MCP (Model Context Protocol) サーバー。2つのモードをサポート:

- **Stdio モード** (シングルユーザー): stdin/stdout 経由の JSON-RPC による MCP クライアント連携
- **HTTP モード** (マルチユーザー): ユーザーごとの Google OAuth 認証付き HTTP サーバー

[English README](README.md)

## セットアップ

### 1. Google Cloud 認証情報の作成

1. [Google Cloud Console](https://console.cloud.google.com/) にアクセス
2. 新しいプロジェクトを作成 (または既存のものを使用)
3. **Google Calendar API** と **Gmail API** を有効化
4. **認証情報** → **認証情報を作成** → **OAuth 2.0 クライアント ID** へ進む
5. アプリケーションの種類として **デスクトップアプリ** (stdio) または **ウェブアプリケーション** (HTTP) を選択
6. HTTP モードの場合、コールバック URI (デフォルト: `http://localhost:8080/auth/callback`) を承認済みリダイレクト URI に追加
7. 認証情報の JSON ファイルをダウンロード

### 2. 認証情報の配置

```bash
mkdir -p ~/.config/mcp-gcal
cp ~/Downloads/client_secret_*.json ~/.config/mcp-gcal/credentials.json
```

### 3. ビルド

```bash
make build
```

## Stdio モード (シングルユーザー)

### 認証

```bash
./mcp-gcal auth
```

ブラウザで Google OAuth ログイン画面が開きます。トークンは SQLite に保存されます。

### 実行

```bash
./mcp-gcal
```

stdin から JSON-RPC 2.0 を読み取り、stdout に書き出します。MCP クライアント内から `authenticate` ツールで再認証も可能です。

### Claude Desktop 設定

```json
{
  "mcpServers": {
    "google-calendar": {
      "command": "/path/to/mcp-gcal"
    }
  }
}
```

## HTTP モード (マルチユーザー)

### 実行

```bash
./mcp-gcal --mode=http --addr=:8080
```

### 認証フロー

1. ユーザーが `http://localhost:8080/auth/login` にアクセス
2. Google OAuth 同意画面にリダイレクト (`calendar` + `gmail.modify` + `userinfo.email` スコープ)
3. 認証後、`/auth/callback` にリダイレクト
4. Google メールアドレスでユーザーを識別し、トークンを SQLite に保存
5. API キーがコールバックページに表示される
6. ログインのたびに新しい API キーが発行される

### MCP リクエスト

API キーを Bearer トークンとして指定:

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer gcal_xxxx..." \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

### エンドポイント

| エンドポイント | メソッド | 説明 |
|---|---|---|
| `/auth/login` | GET | Google OAuth フロー開始 |
| `/auth/callback` | GET | OAuth コールバック (自動) |
| `/health` | GET | ヘルスチェック |
| `/mcp` | POST | MCP JSON-RPC (Bearer トークン必須) |

## CLI フラグ

```
--db=PATH               SQLite データベースパス (デフォルト: ~/.config/mcp-gcal/mcp-gcal.db)
--credentials-file=PATH OAuth2 認証情報 JSON (デフォルト: ~/.config/mcp-gcal/credentials.json)
--mode=stdio|http       サーバーモード (デフォルト: stdio)
--addr=:8080            HTTP リッスンアドレス (HTTP モードのみ)
--base-url=URL          OAuth コールバック用公開ベース URL (HTTP モード; デフォルトは --addr から導出)
```

## ツール

### カレンダーツール

| ツール | 説明 | 必須パラメータ |
|---|---|---|
| `authenticate` | Google OAuth2 ログイン開始 (stdio のみ) | (なし) |
| `list-calendars` | アクセス可能な全カレンダーを一覧 | (なし) |
| `list-events` | 予定の一覧 | (なし) |
| `get-event` | イベント詳細の取得 | `event_id` |
| `search-events` | テキストでイベント検索 | `query` |
| `create-event` | 新しいイベントの作成 | `summary`, `start`, `end` |
| `update-event` | 既存イベントの更新 | `event_id` |
| `delete-event` | イベントの削除 | `event_id` |
| `respond-to-event` | 招待への応答 | `event_id`, `response` |
| `show-calendar` | インタラクティブカレンダー UI (MCP Apps) | (なし) |

### Gmail ツール

| ツール | 説明 | 必須パラメータ |
|---|---|---|
| `search-emails` | Gmail クエリ構文でメール検索 | `query` |
| `read-email` | メールの全文を読む | `message_id` |
| `send-email` | メールを送信 | `to`, `subject`, `body` |
| `draft-email` | 下書きメールを作成 | `to`, `subject`, `body` |
| `modify-email` | メールのラベルを追加・削除 | `message_id` |
| `delete-email` | メールをゴミ箱に移動 | `message_id` |
| `list-email-labels` | Gmail ラベルの一覧 | (なし) |

### MCP Apps UI

`show-calendar` ツールは [MCP Apps](https://github.com/anthropics/mcp-apps) UI に対応しています。対応する MCP クライアントで使用すると、イベントの閲覧・追加・削除が可能なインタラクティブカレンダーが表示されます。

## デプロイ (GCP)

Google Cloud Run に HTTPS ロードバランサー付きでデプロイし、CI/CD で自動デプロイを構成できます。

### 前提条件

- [Terraform](https://www.terraform.io/) インストール済み
- `gcloud` CLI 認証済み
- OAuth 認証情報 JSON ファイル (ウェブアプリケーション タイプ)

### 初期セットアップ

```bash
cd terraform
cp terraform.tfvars.example terraform.tfvars
# terraform.tfvars をプロジェクト設定に合わせて編集
cp /path/to/your/credentials.json credentials.json

terraform init
terraform apply
```

初回の Docker イメージをビルド・プッシュ:

```bash
cd ..
gcloud builds submit \
  --tag=asia-northeast1-docker.pkg.dev/YOUR_PROJECT_ID/mcp-gcal/mcp-gcal:latest \
  --project=YOUR_PROJECT_ID
```

その後、再度 apply して Cloud Run サービスを作成:

```bash
cd terraform
terraform apply
```

### CI/CD

Cloud Build トリガーにより、`main` への push で自動ビルド・デプロイが実行されます:

1. `SHORT_SHA` + `latest` タグで Docker イメージをビルド
2. Artifact Registry にプッシュ
3. Cloud Run に新リビジョンをデプロイ

GitHub リポジトリを Cloud Build に接続してトリガーを設定:

```bash
# GitHub 接続を作成 (ブラウザでの OAuth 認可が必要)
gcloud builds connections create github CONNECTION_NAME \
  --region=asia-northeast1 --project=YOUR_PROJECT_ID

# リポジトリをリンク
gcloud builds repositories create mcp-gcal \
  --connection=CONNECTION_NAME \
  --remote-uri=https://github.com/OWNER/mcp-gcal.git \
  --region=asia-northeast1 --project=YOUR_PROJECT_ID

# トリガーを作成
gcloud builds triggers create github \
  --name=mcp-gcal-deploy \
  --repository=projects/YOUR_PROJECT_ID/locations/asia-northeast1/connections/CONNECTION_NAME/repositories/mcp-gcal \
  --branch-pattern='^main$' \
  --build-config=cloudbuild.yaml \
  --region=asia-northeast1 \
  --project=YOUR_PROJECT_ID \
  --service-account=projects/YOUR_PROJECT_ID/serviceAccounts/YOUR_BUILD_SA
```

### インフラ構成

| リソース | 説明 |
|---|---|
| Cloud Run | アプリケーションサーバー (SQLite のため最大1インスタンス) |
| Artifact Registry | Docker イメージリポジトリ |
| Secret Manager | OAuth 認証情報の保管 |
| グローバル HTTPS LB | Google マネージド SSL 証明書付きロードバランサー |
| Cloud DNS | カスタムドメインの A レコード |
| Cloud Build | CI/CD パイプライン |

## アーキテクチャ

```
Stdio モード (シングルユーザー):
  stdin (JSON-RPC) → Server → Tool Dispatch → Google Calendar API
                                             → Gmail API
                       ↓
                    SQLite (oauth_tokens テーブル)

HTTP モード (マルチユーザー):
  ブラウザ → /auth/login → Google OAuth → /auth/callback → ユーザー + トークン保存
  クライアント → POST /mcp (Bearer トークン) → 認証ミドルウェア → ユーザーごとの Calendar/Gmail API
                                                  ↓
                                               SQLite (users テーブル)

GCP デプロイ:
  GitHub push → Cloud Build → Artifact Registry → Cloud Run
  クライアント → Cloud DNS → HTTPS LB (Google マネージド SSL) → Cloud Run
```

### ファイル構成

- **main.go** - エントリーポイント、モード選択
- **server.go** - Stdio MCP サーバー (シングルユーザー)
- **http.go** - HTTP MCP サーバー (マルチユーザー、OAuth ログイン)
- **tools.go** - ツール定義、共通ディスパッチロジック
- **auth.go** - OAuth2 フロー、トークン管理
- **calendar.go** - Google Calendar API 操作
- **gmail.go** - Gmail API 操作
- **ui.go** - MCP Apps UI リソース処理
- **db.go** - SQLite ストレージ (シングルユーザートークン + マルチユーザーテーブル)
- **templates/calendar.html** - インタラクティブカレンダー UI テンプレート
- **Dockerfile** - マルチステージ Docker ビルド
- **cloudbuild.yaml** - Cloud Build パイプライン (ビルド、プッシュ、デプロイ)
- **terraform/** - GCP インフラのコード管理
