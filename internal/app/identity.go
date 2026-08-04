package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type identityFile struct {
	PrivateKey string `json:"private_key"`
}

type Identity struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	id      string
	cert    tls.Certificate
}

func LoadOrCreateIdentity(path string) (*Identity, error) {
	if raw, err := os.ReadFile(path); err == nil {
		var file identityFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, err
		}
		key, err := base64.RawStdEncoding.DecodeString(file.PrivateKey)
		if err != nil || len(key) != ed25519.PrivateKeySize {
			return nil, errors.New("invalid identity key")
		}
		return newIdentity(ed25519.PrivateKey(key))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	raw, _ := json.MarshalIndent(identityFile{PrivateKey: base64.RawStdEncoding.EncodeToString(private)}, "", "  ")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, err
	}
	return newIdentity(private)
}

func newIdentity(private ed25519.PrivateKey) (*Identity, error) {
	public := private.Public().(ed25519.PublicKey)
	cert, err := makeCertificate(private, public)
	if err != nil {
		return nil, err
	}
	return &Identity{private: private, public: public, id: nodeID(public), cert: cert}, nil
}

func nodeID(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:16]))
}

func makeCertificate(private ed25519.PrivateKey, public ed25519.PublicKey) (tls.Certificate, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: nodeID(public)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, public, private)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	return tls.X509KeyPair(certPEM, keyPEM)
}

func (i *Identity) ID() string                  { return i.id }
func (i *Identity) PublicKeyString() string     { return base64.RawStdEncoding.EncodeToString(i.public) }
func (i *Identity) Certificate() tls.Certificate { return i.cert }
func (i *Identity) Sign(data []byte) string     { return base64.RawStdEncoding.EncodeToString(ed25519.Sign(i.private, data)) }

func parsePublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("invalid public key")
	}
	return ed25519.PublicKey(raw), nil
}

func verifySignature(publicKey string, data []byte, signature string) bool {
	pub, err := parsePublicKey(publicKey)
	if err != nil {
		return false
	}
	sig, err := base64.RawStdEncoding.DecodeString(signature)
	return err == nil && ed25519.Verify(pub, data, sig)
}
