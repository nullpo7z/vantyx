#!/bin/sh
# Vantyx は Dockerfile の `USER nonroot:nonroot` で nonroot として起動する。
# `cap_drop: ALL` の環境では `setgroups()` が EPERM となり su-exec が
# 動作しないため、コンテナ起動前にユーザーを切り替える方式は採らない。
#
# バインドマウント (/app/certs, /app/data, /app/recordings) はホスト側で
# UID/GID 65532 に書き込み可能な権限を付与しておく必要がある。下の chown
# は Docker named volume を使う場合のセーフティネット。nonroot で実行する
# ため、すでに root 所有のディレクトリでは黙ってスキップする。
for d in /app/certs /app/data /app/recordings; do
  [ -d "$d" ] && chown nonroot:nonroot "$d" 2>/dev/null || true
done

exec /app/vantyx
