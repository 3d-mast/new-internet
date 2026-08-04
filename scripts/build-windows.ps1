$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:CGO_ENABLED = "0"
go test ./...
go vet ./...
node --check internal/app/web/reef.js
go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist/reef.exe ./cmd/reef
go build -trimpath -ldflags "-s -w" -o dist/reef-console.exe ./cmd/reef
Copy-Item LICENSE, NOTICE -Destination dist -Force
Write-Host "Built dist/reef.exe and dist/reef-console.exe"
