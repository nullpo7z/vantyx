module github.com/nullpo7z/vantyx

go 1.26

toolchain go1.26.3

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/creack/pty v1.1.24
	github.com/go-chi/chi/v5 v5.3.0
	github.com/gorilla/websocket v1.5.3
	github.com/jlaffaye/ftp v0.2.0
	github.com/pin/tftp/v3 v3.2.0
	github.com/pkg/sftp v1.13.10
	golang.org/x/crypto v0.51.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.50.1
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

// 秘密鍵ファイルの aes256-gcm@openssh.com 復号対応のためパッチ済み crypto を使用
replace golang.org/x/crypto => ./patched_deps/crypto
