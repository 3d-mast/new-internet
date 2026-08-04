$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:CGO_ENABLED = "0"
go test ./...
go vet ./...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist/lichen.exe ./cmd/lichen
Write-Host "Built dist/lichen.exe"
