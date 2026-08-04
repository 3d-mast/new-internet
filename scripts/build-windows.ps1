$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:CGO_ENABLED = "0"
go test ./...
go vet ./...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist/rhizome.exe ./cmd/rhizome
Write-Host "Built dist/rhizome.exe"
