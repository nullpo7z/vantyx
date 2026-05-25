# Vantyx API specification

- **OpenAPI definition**: [openapi.yaml](./openapi.yaml) (OpenAPI 3.1)

## In-app reference for administrators

After logging in as the **admin** user, click **"API reference"** in the
header or browse to **`/docs`** to view the OpenAPI document in Swagger
UI. Non-admin users get `403 Forbidden`.

- **`GET /docs`** — Swagger UI HTML (admin only).
- **`GET /api/spec`** — OpenAPI YAML payload (admin only).

## Browsing the spec outside the app

### 1. Swagger UI in Docker

```bash
# Run from the repository root.
docker run --rm -p 8081:8080 \
  -e SWAGGER_JSON=/spec/openapi.yaml \
  -v "$(pwd)/docs/api:/spec:ro" \
  swaggerapi/swagger-ui
```

Open <http://localhost:8081>.

### 2. Online editor

Use the [Swagger Editor](https://editor.swagger.io). Choose
**File → Import file** and upload `openapi.yaml`.

### 3. VS Code

Install the **"OpenAPI (Swagger) Editor"** extension and open
`openapi.yaml` for live preview.
