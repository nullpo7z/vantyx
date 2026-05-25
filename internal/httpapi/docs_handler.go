package httpapi

import (
	"net/http"

	api "github.com/nullpo7z/vantyx/docs/api"
)

// handleAPISpec serves the OpenAPI YAML at /api/spec. Admin only.
func (a *App) handleAPISpec(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(api.OpenAPIYAML)
}

// swaggerUIHTML is the static Swagger UI shell served at /docs. The
// surrounding notice is in Japanese because operators interact with
// this page directly.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="UTF-8">
  <title>Vantyx API リファレンス</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    .vantyx-notice { padding: 10px 16px; margin: 0; background: #fef3c7; border-bottom: 1px solid #f59e0b; color: #92400e; font-size: 14px; }
    .vantyx-notice strong { font-weight: 600; }
  </style>
</head>
<body>
  <p class="vantyx-notice">
    <strong>Try it out で NetworkError が出る場合:</strong> 自己署名証明書を使っているときは、先にこのサイトの証明書を信頼してください。
    <a href="/" target="_blank" rel="noopener">トップを新しいタブで開き</a>、「詳細」→「安全な接続を続行」などで例外を許可してから、このページを再読み込みして再度 Try it out を実行してください。
  </p>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: window.location.origin + '/api/spec',
        dom_id: '#swagger-ui',
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIBundle.SwaggerUIStandalonePreset
        ],
        requestInterceptor: function(req) { req.credentials = 'same-origin'; return req; }
      });
    };
  </script>
</body>
</html>
`

// handleDocs serves the Swagger UI HTML at /docs. Admin only.
func (a *App) handleDocs(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerUIHTML))
}
