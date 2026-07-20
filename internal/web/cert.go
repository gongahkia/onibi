package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/web/transport"
)

const (
	localCAName     = "Onibi local CA"
	localServerName = "Onibi local cockpit"
)

var (
	validateCAReadbackFunc     = validateCAReadback
	validateServerReadbackFunc = validateServerReadback
)

type CertPaths struct {
	CACert       string
	CAKey        string
	ServerCert   string
	ServerKey    string
	MobileConfig string
}

func LocalCertPaths(certDir string) CertPaths {
	return CertPaths{
		CACert:       filepath.Join(certDir, "onibi-local-ca.crt"),
		CAKey:        filepath.Join(certDir, "onibi-local-ca.key"),
		ServerCert:   filepath.Join(certDir, "server.crt"),
		ServerKey:    filepath.Join(certDir, "server.key"),
		MobileConfig: filepath.Join(certDir, "onibi-local-ca.mobileconfig"),
	}
}

func GenerateOrLoadCert(certDir string) (tls.Certificate, error) {
	return GenerateOrLoadCertForHosts(certDir)
}

func GenerateOrLoadCertForHosts(certDir string, hosts ...string) (tls.Certificate, error) {
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("mkdir cert dir: %w", err)
	}
	paths := LocalCertPaths(certDir)
	now := time.Now()
	lanIPs := certIPs(hosts)
	caCert, caKey, err := loadOrCreateCA(paths, now)
	if err != nil {
		return tls.Certificate{}, err
	}
	if cert, ok := loadUsableServerCert(paths, caCert, lanIPs, now); ok {
		return cert, nil
	}
	return createServerCert(paths, caCert, caKey, lanIPs, now)
}

func certIPs(hosts []string) []net.IP {
	seen := map[string]bool{}
	var ips []net.IP
	for _, ip := range append(transport.DetectLANIPs(), explicitCertIPs(hosts)...) {
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
		}
		if !seen[ip.String()] {
			seen[ip.String()] = true
			ips = append(ips, append(net.IP(nil), ip...))
		}
	}
	return ips
}

func explicitCertIPs(hosts []string) []net.IP {
	var ips []net.IP
	for _, host := range hosts {
		host = strings.Trim(strings.TrimSpace(host), "[]")
		if ip := net.ParseIP(host); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

func loadOrCreateCA(paths CertPaths, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, certErr := os.ReadFile(paths.CACert)
	keyPEM, keyErr := os.ReadFile(paths.CAKey)
	if certErr == nil && keyErr == nil {
		cert, certOK := parseSingleCert(certPEM)
		key, keyOK := parseECPrivateKey(keyPEM)
		if certOK && keyOK && cert.IsCA && cert.NotAfter.After(now.AddDate(1, 0, 0)) {
			if err := writeMobileConfig(paths.MobileConfig, certPEM); err != nil {
				return nil, nil, err
			}
			return cert, key, nil
		}
	}
	return createCA(paths, now)
}

func createCA(paths CertPaths, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cert, key, err := createCAOnce(paths, now)
	if err == nil {
		return cert, key, nil
	}
	slog.Warn("local ca certificate validation failed; retrying", "err", err)
	removeCertPair(paths.CACert, paths.CAKey)
	cert, key, err = createCAOnce(paths, now)
	if err != nil {
		return nil, nil, fmt.Errorf("local CA certificate write validation failed after retry: %w; remove %s and %s, then rerun onibi up", err, paths.CACert, paths.CAKey)
	}
	return cert, key, nil
}

func createCAOnce(paths CertPaths, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ca key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: localCAName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create ca cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal ca key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(paths.CACert, certPEM, 0o600); err != nil {
		return nil, nil, fmt.Errorf("write ca cert: %w", err)
	}
	if err := os.WriteFile(paths.CAKey, keyPEM, 0o600); err != nil {
		return nil, nil, fmt.Errorf("write ca key: %w", err)
	}
	cert, key, err := validateCAReadbackFunc(paths, now)
	if err != nil {
		return nil, nil, err
	}
	if err := writeMobileConfig(paths.MobileConfig, certPEM); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func createServerCert(paths CertPaths, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, lanIPs []net.IP, now time.Time) (tls.Certificate, error) {
	cert, err := createServerCertOnce(paths, caCert, caKey, lanIPs, now)
	if err == nil {
		return cert, nil
	}
	slog.Warn("server certificate validation failed; retrying", "err", err)
	removeCertPair(paths.ServerCert, paths.ServerKey)
	cert, err = createServerCertOnce(paths, caCert, caKey, lanIPs, now)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("server certificate write validation failed after retry: %w; remove %s and %s, then rerun onibi up", err, paths.ServerCert, paths.ServerKey)
	}
	return cert, nil
}

func createServerCertOnce(paths CertPaths, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, lanIPs []net.IP, now time.Time) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate server key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: localServerName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
		BasicConstraintsValid: true,
	}
	tmpl.IPAddresses = append(tmpl.IPAddresses, lanIPs...)
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create server cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal server key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(paths.ServerCert, certPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write server cert: %w", err)
	}
	if err := os.WriteFile(paths.ServerKey, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write server key: %w", err)
	}
	return validateServerReadbackFunc(paths, caCert, now)
}

func loadUsableServerCert(paths CertPaths, caCert *x509.Certificate, lanIPs []net.IP, now time.Time) (tls.Certificate, bool) {
	cert, err := tls.LoadX509KeyPair(paths.ServerCert, paths.ServerKey)
	if err != nil || len(cert.Certificate) == 0 {
		return tls.Certificate{}, false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, false
	}
	if leaf.IsCA || leaf.NotAfter.Before(now.Add(24*time.Hour)) || leaf.CheckSignatureFrom(caCert) != nil {
		return tls.Certificate{}, false
	}
	if !containsDNS(leaf.DNSNames, "localhost") || !containsIP(leaf.IPAddresses, net.ParseIP("127.0.0.1")) || !containsIP(leaf.IPAddresses, net.ParseIP("::1")) {
		return tls.Certificate{}, false
	}
	for _, ip := range lanIPs {
		if !containsIP(leaf.IPAddresses, ip) {
			return tls.Certificate{}, false
		}
	}
	return cert, true
}

func randomSerial() (*big.Int, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}

func parseSingleCert(certPEM []byte) (*x509.Certificate, bool) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	return cert, err == nil
}

func parseECPrivateKey(keyPEM []byte) (*ecdsa.PrivateKey, bool) {
	block, _ := pem.Decode(keyPEM)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, false
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	return key, err == nil
}

func validateCAReadback(paths CertPaths, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(paths.CACert)
	if err != nil {
		return nil, nil, fmt.Errorf("read back ca cert: %w", err)
	}
	keyPEM, err := os.ReadFile(paths.CAKey)
	if err != nil {
		return nil, nil, fmt.Errorf("read back ca key: %w", err)
	}
	cert, ok := parseSingleCert(certPEM)
	if !ok {
		return nil, nil, fmt.Errorf("read back ca cert: parse failed")
	}
	key, ok := parseECPrivateKey(keyPEM)
	if !ok {
		return nil, nil, fmt.Errorf("read back ca key: parse failed")
	}
	if !cert.IsCA {
		return nil, nil, fmt.Errorf("read back ca cert: not a CA")
	}
	if err := validateCertDate("ca cert", cert, now); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func validateServerReadback(paths CertPaths, caCert *x509.Certificate, now time.Time) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(paths.ServerCert, paths.ServerKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read back server key pair: %w", err)
	}
	if len(cert.Certificate) == 0 {
		return tls.Certificate{}, fmt.Errorf("read back server key pair: missing certificate DER")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read back server cert: parse failed: %w", err)
	}
	if leaf.IsCA {
		return tls.Certificate{}, fmt.Errorf("read back server cert: unexpectedly a CA")
	}
	if err := validateCertDate("server cert", leaf, now); err != nil {
		return tls.Certificate{}, err
	}
	if err := leaf.CheckSignatureFrom(caCert); err != nil {
		return tls.Certificate{}, fmt.Errorf("read back server cert: ca signature check failed: %w", err)
	}
	return cert, nil
}

func validateCertDate(name string, cert *x509.Certificate, now time.Time) error {
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("read back %s: not valid before %s", name, cert.NotBefore.Format(time.RFC3339))
	}
	if !now.Before(cert.NotAfter) {
		return fmt.Errorf("read back %s: expired at %s", name, cert.NotAfter.Format(time.RFC3339))
	}
	return nil
}

func removeCertPair(certPath, keyPath string) {
	_ = os.Remove(certPath)
	_ = os.Remove(keyPath)
}

func writeMobileConfig(path string, certPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("write mobileconfig: invalid ca cert")
	}
	content := base64.StdEncoding.EncodeToString(block.Bytes)
	profile := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
<key>PayloadContent</key>
<array>
<dict>
<key>PayloadCertificateFileName</key>
<string>onibi-local-ca.crt</string>
<key>PayloadContent</key>
<data>%s</data>
<key>PayloadDescription</key>
<string>Installs the Onibi local CA certificate.</string>
<key>PayloadDisplayName</key>
<string>Onibi local CA</string>
<key>PayloadIdentifier</key>
<string>dev.onibi.local.ca</string>
<key>PayloadType</key>
<string>com.apple.security.root</string>
<key>PayloadUUID</key>
<string>8C9D316B-3D17-49A3-A673-458A8392BC5A</string>
<key>PayloadVersion</key>
<integer>1</integer>
</dict>
</array>
<key>PayloadDescription</key>
<string>Trust profile for Onibi's local HTTPS cockpit.</string>
<key>PayloadDisplayName</key>
<string>Onibi Local CA</string>
<key>PayloadIdentifier</key>
<string>dev.onibi.local.profile</string>
<key>PayloadOrganization</key>
<string>Onibi</string>
<key>PayloadRemovalDisallowed</key>
<false/>
<key>PayloadType</key>
<string>Configuration</string>
<key>PayloadUUID</key>
<string>216C3E3B-0B87-47B2-91D4-6C8F4B33D482</string>
<key>PayloadVersion</key>
<integer>1</integer>
</dict>
</plist>
`, content)
	if err := os.WriteFile(path, []byte(profile), 0o600); err != nil {
		return fmt.Errorf("write mobileconfig: %w", err)
	}
	return nil
}

func containsDNS(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}

func containsIP(vals []net.IP, want net.IP) bool {
	for _, v := range vals {
		if v.Equal(want) {
			return true
		}
	}
	return false
}
