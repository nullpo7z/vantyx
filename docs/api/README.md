# Vantyx API 仕様

- **OpenAPI 定義**: [openapi.yaml](./openapi.yaml) (OpenAPI 3.1)

## 管理者向け API リファレンス（アプリ内）

**admin** ユーザーでログイン後、ヘッダーの **「API リファレンス」** をクリックするか、ブラウザで **/docs** にアクセスすると、Swagger UI で API 仕様を表示できます。管理者（user_id が `admin` のユーザー）以外は 403 Forbidden になります。

- **GET /docs** … Swagger UI の HTML（管理者のみ）
- **GET /api/spec** … OpenAPI YAML の内容（管理者のみ）

## 仕様の表示方法（開発者向け）

### 1. Swagger UI（Docker）

```bash
# リポジトリルートで実行
docker run --rm -p 8081:8080 \
  -e SWAGGER_JSON=/spec/openapi.yaml \
  -v "$(pwd)/docs/api:/spec:ro" \
  swaggerapi/swagger-ui
```

ブラウザで **http://localhost:8081** を開く。

### 2. オンラインエディタ

- [Swagger Editor](https://editor.swagger.io) を開き、**File → Import file** で `openapi.yaml` をアップロードする。

### 3. VS Code

- 拡張機能 **「OpenAPI (Swagger) Editor」** をインストールし、`openapi.yaml` を開いてプレビューを表示する。
