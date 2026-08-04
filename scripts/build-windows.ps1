$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go test ./...
go build -trimpath -ldflags "-s -w" -o dist/mycelium-one.exe ./cmd/mycelium
Write-Host "Built dist/mycelium-one.exe"
