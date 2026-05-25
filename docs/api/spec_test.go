package api

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestOpenAPIYAML_IsValid は埋め込み OpenAPI 仕様が YAML としてパース可能で、
// かつ必須フィールド（openapi バージョン / paths）を持つことを確認する。
// description などに引用なしでコロンが混入したような壊れ方を CI / 単体テスト
// 時点で検出し、Swagger UI (/docs) を開いて初めて気付くという事故を防ぐ。
func TestOpenAPIYAML_IsValid(t *testing.T) {
	if len(OpenAPIYAML) == 0 {
		t.Fatal("OpenAPIYAML が空")
	}
	var spec map[string]any
	if err := yaml.Unmarshal(OpenAPIYAML, &spec); err != nil {
		t.Fatalf("openapi.yaml が YAML としてパースできません: %v", err)
	}
	version, ok := spec["openapi"].(string)
	if !ok || version == "" {
		t.Fatalf("openapi バージョンフィールドが見つかりません。Swagger UI で表示するには openapi: 3.x.x が必須です。spec=%v", spec)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Fatalf("paths が空または不正です。spec=%v", spec)
	}
}
