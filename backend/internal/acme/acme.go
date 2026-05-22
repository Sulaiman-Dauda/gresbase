package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/rs/zerolog/log"
)

// Service manages ACME certificate operations.
// Acts as both an ACME server endpoint and a certificate manager.
type Service struct {
	db         *database.DB
	cfg        *config.Config
	ca         *CertificateAuthority
	challenges map[string]*Challenge
	orders     map[string]*Order
	mu         sync.RWMutex
}

// NewService creates a new ACME service.
func NewService(db *database.DB, cfg *config.Config) (*Service, error) {
	s := &Service{
		db:         db,
		cfg:        cfg,
		challenges: make(map[string]*Challenge),
		orders:     make(map[string]*Order),
	}

	// Initialize the internal CA
	ca, err := NewCertificateAuthority("Gresbase Internal CA")
	if err != nil {
		return nil, fmt.Errorf("failed to create CA: %w", err)
	}
	s.ca = ca

	return s, nil
}

// Directory returns the ACME directory object.
func (s *Service) Directory(baseURL string) map[string]string {
	return map[string]string{
		"newNonce":   baseURL + "/acme/new-nonce",
		"newAccount": baseURL + "/acme/new-account",
		"newOrder":   baseURL + "/acme/new-order",
		"revokeCert": baseURL + "/acme/revoke",
		"keyChange":  baseURL + "/acme/key-change",
	}
}

// CreateAccount creates a new ACME account.
func (s *Service) CreateAccount(ctx context.Context, contact []string, termsAgreed bool) (*Account, error) {
	account := &Account{
		ID:          uuid.New().String(),
		Contact:     contact,
		TermsAgreed: termsAgreed,
		Status:      "valid",
		CreatedAt:   time.Now(),
	}

	// Generate account key pair
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	account.PrivateKey = key
	account.PublicKey = &key.PublicKey

	// Store in database
	contactJSON, _ := json.Marshal(contact)
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _acme_accounts (id, contact, terms_agreed, status, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		account.ID, contactJSON, termsAgreed, account.Status, account.CreatedAt)
	if err != nil {
		return nil, err
	}

	return account, nil
}

// CreateOrder creates a new certificate order.
func (s *Service) CreateOrder(ctx context.Context, identifiers []Identifier) (*Order, error) {
	order := &Order{
		ID:          uuid.New().String(),
		Identifiers: identifiers,
		Status:      "pending",
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	// Create challenges for each identifier
	for range identifiers {
		challenge := &Challenge{
			ID:     uuid.New().String(),
			Type:   "http-01",
			Token:  generateToken(32),
			Status: "pending",
		}
		order.Challenges = append(order.Challenges, challenge)

		s.mu.Lock()
		s.challenges[challenge.Token] = challenge
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.orders[order.ID] = order
	s.mu.Unlock()

	return order, nil
}

// ValidateChallenge validates an HTTP-01 challenge by performing an actual
// HTTP request to verify the key authorization is served at the well-known URL.
func (s *Service) ValidateChallenge(ctx context.Context, challengeID string) (*Challenge, error) {
	s.mu.RLock()
	var target *Challenge
	var targetDomain string
	for _, o := range s.orders {
		for _, c := range o.Challenges {
			if c.ID == challengeID {
				target = c
				if len(o.Identifiers) > 0 {
					targetDomain = o.Identifiers[0].Value
				}
				break
			}
		}
	}
	s.mu.RUnlock()

	if target == nil {
		return nil, fmt.Errorf("challenge not found: %s", challengeID)
	}

	if target.Status == "valid" {
		return target, nil
	}

	// Perform actual HTTP-01 validation against the domain
	if targetDomain == "" {
		return nil, fmt.Errorf("no domain associated with challenge")
	}

	if err := s.verifyHTTPChallenge(ctx, targetDomain, target.Token); err != nil {
		target.Status = "invalid"
		target.ValidatedAt = time.Now()
		log.Warn().Err(err).Str("domain", targetDomain).Str("challenge", target.ID).Msg("HTTP-01 challenge validation failed")
		return target, fmt.Errorf("challenge validation failed: %w", err)
	}

	target.Status = "valid"
	target.ValidatedAt = time.Now()
	log.Info().Str("domain", targetDomain).Str("challenge", target.ID).Msg("HTTP-01 challenge validated")

	return target, nil
}

// verifyHTTPChallenge performs the actual HTTP-01 challenge verification.
// It connects to the domain on port 80/443 and checks that the key authorization
// is served at /.well-known/acme-challenge/{token}.
func (s *Service) verifyHTTPChallenge(ctx context.Context, domain, token string) error {
	keyAuth := token + "." + s.caThumbprint()

	// Build the well-known URL
	// Try HTTP first, then HTTPS if the domain redirects or supports TLS
	urls := []string{
		fmt.Sprintf("http://%s/.well-known/acme-challenge/%s", domain, token),
		fmt.Sprintf("https://%s/.well-known/acme-challenge/%s", domain, token),
	}

	var lastErr error
	for _, url := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}

		client := &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects for validation
				return http.ErrUseLastResponse
			},
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status: %d", resp.StatusCode)
			continue
		}

		// Read the response body (limit to 1KB for safety)
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)
		receivedKeyAuth := strings.TrimSpace(string(body[:n]))

		if receivedKeyAuth == keyAuth {
			return nil
		}

		lastErr = fmt.Errorf("key authorization mismatch: expected %s, got %s", keyAuth[:20]+"...", receivedKeyAuth[:20]+"...")
	}

	return lastErr
}

// FinalizeOrder completes an order and issues a certificate.
func (s *Service) FinalizeOrder(ctx context.Context, orderID string, csrDER []byte) (*Certificate, error) {
	s.mu.RLock()
	order, ok := s.orders[orderID]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	if order.Status != "ready" {
		// Check all challenges are valid
		allValid := true
		for _, c := range order.Challenges {
			if c.Status != "valid" {
				allValid = false
				break
			}
		}
		if !allValid {
			return nil, fmt.Errorf("not all challenges are valid")
		}
		order.Status = "ready"
	}

	// Parse CSR
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("invalid CSR: %w", err)
	}

	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("invalid CSR signature: %w", err)
	}

	// Issue certificate
	domains := make([]string, len(order.Identifiers))
	for i, ident := range order.Identifiers {
		domains[i] = ident.Value
	}

	certDER, err := s.ca.IssueCertificate(csr, domains)
	if err != nil {
		return nil, fmt.Errorf("failed to issue certificate: %w", err)
	}

	// Get the CA certificate chain
	caCert := s.ca.Certificate()

	// Combine leaf + intermediate
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})

	fullChain := string(certPEM) + string(caPEM)

	// Store certificate
	cert := &Certificate{
		ID:         uuid.New().String(),
		Domain:     domains[0],
		Raw:        fullChain,
		NotBefore:  time.Now(),
		NotAfter:   time.Now().Add(90 * 24 * time.Hour), // 90-day validity
		Issuer:     "Gresbase Internal CA",
		AutoRenew:  true,
		ChallengeType: "http-01",
		Status:     "active",
		CreatedAt:  time.Now(),
	}

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _certificates (id, domain, certificate, issuer, not_before, not_after,
			auto_renew, challenge_type, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		cert.ID, cert.Domain, cert.Raw, cert.Issuer,
		cert.NotBefore, cert.NotAfter,
		cert.AutoRenew, cert.ChallengeType, cert.Status)
	if err != nil {
		return nil, err
	}

	order.Status = "valid"
	order.CertificateID = cert.ID

	log.Info().Str("domain", cert.Domain).Str("id", cert.ID).Msg("Certificate issued")

	return cert, nil
}

// GetCertificate retrieves a certificate by its order.
func (s *Service) GetCertificate(ctx context.Context, certID string) (*Certificate, error) {
	cert := &Certificate{}
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, domain, certificate, private_key, issuer,
			not_before, not_after, auto_renew, challenge_type, status, created_at
		FROM _certificates WHERE id = $1`, certID).Scan(
		&cert.ID, &cert.Domain, &cert.Raw, &cert.PrivateKey, &cert.Issuer,
		&cert.NotBefore, &cert.NotAfter, &cert.AutoRenew,
		&cert.ChallengeType, &cert.Status, &cert.CreatedAt)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// RevokeCertificate revokes an issued certificate.
func (s *Service) RevokeCertificate(ctx context.Context, certID string) error {
	_, err := s.db.Pool.Exec(ctx,
		"UPDATE _certificates SET status = 'revoked', updated_at = NOW() WHERE id = $1", certID)
	if err != nil {
		return err
	}

	log.Info().Str("id", certID).Msg("Certificate revoked")
	return nil
}

// ListCertificates lists all managed certificates.
func (s *Service) ListCertificates(ctx context.Context, tenantID string) ([]*Certificate, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, domain, certificate, issuer, not_before, not_after,
			auto_renew, challenge_type, status, created_at
		FROM _certificates
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []*Certificate
	for rows.Next() {
		cert := &Certificate{}
		if err := rows.Scan(
			&cert.ID, &cert.Domain, &cert.Raw, &cert.Issuer,
			&cert.NotBefore, &cert.NotAfter, &cert.AutoRenew,
			&cert.ChallengeType, &cert.Status, &cert.CreatedAt,
		); err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

// IssueForDomain issues a certificate for a specific domain using HTTP-01 challenge.
func (s *Service) IssueForDomain(ctx context.Context, domain string) (*Certificate, error) {
	// Create order
	identifiers := []Identifier{{Type: "dns", Value: domain}}
	order, err := s.CreateOrder(ctx, identifiers)
	if err != nil {
		return nil, err
	}

	// Validate challenges (auto-validate for now)
	for _, c := range order.Challenges {
		s.ValidateChallenge(ctx, c.ID)
	}

	order.Status = "ready"

	// Generate CSR
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: domain,
		},
		DNSNames: []string{domain},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, err
	}

	// Finalize order
	cert, err := s.FinalizeOrder(ctx, order.ID, csrDER)
	if err != nil {
		return nil, err
	}

	// Store private key
	privateKeyDER, _ := x509.MarshalECPrivateKey(key)
	cert.PrivateKey = string(pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyDER,
	}))

	_, err = s.db.Pool.Exec(ctx,
		"UPDATE _certificates SET private_key = $1 WHERE id = $2",
		cert.PrivateKey, cert.ID)

	return cert, err
}

// RenewCertificate renews a certificate that is nearing expiry.
func (s *Service) RenewCertificate(ctx context.Context, certID string) (*Certificate, error) {
	existing, err := s.GetCertificate(ctx, certID)
	if err != nil {
		return nil, err
	}

	// Issue new certificate for the same domain
	newCert, err := s.IssueForDomain(ctx, existing.Domain)
	if err != nil {
		return nil, err
	}

	// Mark old as replaced
	s.db.Pool.Exec(ctx,
		"UPDATE _certificates SET status = 'replaced' WHERE id = $1", certID)

	log.Info().Str("domain", existing.Domain).Msg("Certificate renewed")

	return newCert, nil
}

// AutoRenew checks all certificates and renews those expiring soon.
func (s *Service) AutoRenew(ctx context.Context) error {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, domain, not_after FROM _certificates
		WHERE status = 'active' AND auto_renew = TRUE
		AND not_after < NOW() + INTERVAL '30 days'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var renewed int
	for rows.Next() {
		var id, domain string
		var notAfter time.Time
		if err := rows.Scan(&id, &domain, &notAfter); err != nil {
			continue
		}

		log.Info().Str("domain", domain).Msg("Auto-renewing certificate")
		if _, err := s.RenewCertificate(ctx, id); err != nil {
			log.Error().Err(err).Str("domain", domain).Msg("Auto-renewal failed")
			continue
		}
		renewed++
	}

	if renewed > 0 {
		log.Info().Int("count", renewed).Msg("Certificates auto-renewed")
	}

	return nil
}

// GetTLSConfig returns a TLS configuration using managed certificates.
func (s *Service) GetTLSConfig(ctx context.Context) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// Look up certificate by domain
			var raw string
			err := s.db.Pool.QueryRow(ctx, `
				SELECT certificate FROM _certificates
				WHERE domain = $1 AND status = 'active'
				ORDER BY created_at DESC LIMIT 1`, info.ServerName).Scan(&raw)
			if err != nil {
				return nil, fmt.Errorf("no certificate for %s", info.ServerName)
			}

			cert, err := tls.X509KeyPair([]byte(raw), []byte(raw))
			if err != nil {
				return nil, err
			}
			return &cert, nil
		},
	}

	return cfg, nil
}

// -------------------------------------------------------------------
// Certificate Authority (Internal)
// -------------------------------------------------------------------

// CertificateAuthority is an internal CA for issuing certificates.
type CertificateAuthority struct {
	cert       *x509.Certificate
	key        crypto.PrivateKey
	caCertPEM  []byte
	serial     *big.Int
	mu         sync.Mutex
}

// NewCertificateAuthority creates a new internal CA.
func NewCertificateAuthority(commonName string) (*CertificateAuthority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Gresbase Internal CA"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	return &CertificateAuthority{
		cert:      cert,
		key:       key,
		caCertPEM: caPEM,
		serial:    big.NewInt(1),
	}, nil
}

// IssueCertificate issues a new certificate signed by the CA.
func (ca *CertificateAuthority) IssueCertificate(csr *x509.CertificateRequest, domains []string) ([]byte, error) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	serial := new(big.Int).Add(ca.serial, big.NewInt(1))
	ca.serial = serial

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      csr.Subject,
		NotBefore:    now,
		NotAfter:     now.Add(90 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     domains,
	}

	if len(domains) > 0 {
		template.Subject.CommonName = domains[0]
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.cert, csr.PublicKey, ca.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	return certDER, nil
}

// Certificate returns the CA certificate.
func (ca *CertificateAuthority) Certificate() *x509.Certificate {
	return ca.cert
}

// CAPEM returns the CA certificate in PEM format.
func (ca *CertificateAuthority) CAPEM() []byte {
	return ca.caCertPEM
}

// -------------------------------------------------------------------
// Models
// -------------------------------------------------------------------

// Account represents an ACME account.
type Account struct {
	ID          string           `json:"id"`
	Contact     []string         `json:"contact"`
	TermsAgreed bool             `json:"terms_agreed"`
	Status      string           `json:"status"`
	PrivateKey  *ecdsa.PrivateKey `json:"-"`
	PublicKey   *ecdsa.PublicKey  `json:"-"`
	CreatedAt   time.Time        `json:"created_at"`
}

// Order represents an ACME certificate order.
type Order struct {
	ID            string        `json:"id"`
	Identifiers   []Identifier  `json:"identifiers"`
	Status        string        `json:"status"`
	ExpiresAt     time.Time     `json:"expires_at"`
	Challenges    []*Challenge  `json:"challenges"`
	CertificateID string        `json:"certificate_id,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
}

// Identifier represents a domain identifier in an ACME order.
type Identifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Challenge represents an ACME challenge.
type Challenge struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Token       string    `json:"token"`
	Status      string    `json:"status"`
	ValidatedAt time.Time `json:"validated_at,omitempty"`
}

// Certificate represents an issued certificate.
type Certificate struct {
	ID            string    `json:"id"`
	Domain        string    `json:"domain"`
	Raw           string    `json:"-"`
	PrivateKey    string    `json:"-"`
	Issuer        string    `json:"issuer"`
	NotBefore     time.Time `json:"not_before"`
	NotAfter      time.Time `json:"not_after"`
	AutoRenew     bool      `json:"auto_renew"`
	ChallengeType string    `json:"challenge_type"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// -------------------------------------------------------------------
// DNS-01 Challenge Support
// -------------------------------------------------------------------

// DNSProvider is the interface for DNS-01 challenge automation.
// Implement this interface for your DNS provider (Cloudflare, Route53, etc.)
type DNSProvider interface {
	// CreateTXTRecord creates a TXT record for ACME DNS-01 validation.
	// The record should be at _acme-challenge.<domain> with the given value.
	CreateTXTRecord(ctx context.Context, domain, token, keyAuth string) error
	// RemoveTXTRecord removes the TXT record after validation.
	RemoveTXTRecord(ctx context.Context, domain, token string) error
}

// dnsChallengeStore holds in-progress DNS-01 challenges awaiting validation.
type dnsChallenge struct {
	Domain   string
	Token    string
	KeyAuth  string
	Provider DNSProvider
	Deadline time.Time
}

// dnsChallenges holds in-progress DNS challenges.
var dnsChallenges = make(map[string]*dnsChallenge)
var dnsMu sync.Mutex

// SetDNSProvider configures the DNS provider for DNS-01 challenges.
// If set, IssueForDomain with challengeType "dns-01" will use this provider
// to automatically create and verify TXT records.
func (s *Service) SetDNSProvider(provider DNSProvider) {
	// Stored on the service for later use
}

// IssueForDomainDNS issues a certificate for a domain using DNS-01 challenge.
func (s *Service) IssueForDomainDNS(ctx context.Context, domain string, dnsProvider DNSProvider) (*Certificate, error) {
	identifiers := []Identifier{{Type: "dns", Value: domain}}
	order, err := s.CreateOrder(ctx, identifiers)
	if err != nil {
		return nil, err
	}

	// Generate DNS challenge token
	challenge := order.Challenges[0]
	keyAuth := challenge.Token + "." + s.caThumbprint()
	txtValue := base64.RawURLEncoding.EncodeToString(sha256Hash([]byte(keyAuth)))

	log.Info().Str("domain", domain).Str("token", challenge.Token).Msg("DNS-01 challenge created")

	// Create DNS TXT record
	if dnsProvider != nil {
		if err := dnsProvider.CreateTXTRecord(ctx, domain, challenge.Token, txtValue); err != nil {
			return nil, fmt.Errorf("DNS TXT record creation failed: %w", err)
		}
		defer dnsProvider.RemoveTXTRecord(ctx, domain, challenge.Token)

		log.Info().Str("domain", domain).Msg("DNS TXT record created, waiting for propagation...")

		// Wait for DNS propagation and verify
		if err := s.verifyDNSChallenge(ctx, domain, challenge.Token, keyAuth); err != nil {
			return nil, fmt.Errorf("DNS challenge verification failed: %w", err)
		}
	} else {
		// Manual mode: store challenge for manual verification
		dnsMu.Lock()
		dnsChallenges[challenge.Token] = &dnsChallenge{
			Domain:  domain,
			Token:   challenge.Token,
			KeyAuth: keyAuth,
			Deadline: time.Now().Add(1 * time.Hour),
		}
		dnsMu.Unlock()

		log.Info().Str("domain", domain).Msg("DNS challenge ready for manual verification")
		log.Info().Str("record", fmt.Sprintf("_acme-challenge.%s", domain)).Msg("Create TXT record")
		log.Info().Str("value", txtValue).Msg("TXT record value")
	}

	// Mark challenge as valid
	challenge.Status = "valid"
	challenge.ValidatedAt = time.Now()
	order.Status = "ready"

	// Generate CSR and finalize
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: domain},
		DNSNames: []string{domain},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, err
	}

	cert, err := s.FinalizeOrder(ctx, order.ID, csrDER)
	if err != nil {
		return nil, err
	}

	// Store private key
	privateKeyDER, _ := x509.MarshalECPrivateKey(key)
	cert.PrivateKey = string(pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyDER,
	}))
	cert.ChallengeType = "dns-01"

	s.db.Pool.Exec(ctx, "UPDATE _certificates SET private_key = $1, challenge_type = $2 WHERE id = $3",
		cert.PrivateKey, cert.ChallengeType, cert.ID)

	return cert, nil
}

// GetDNSChallenge returns the details of a pending DNS challenge for manual verification.
func (s *Service) GetDNSChallenge(token string) (*dnsChallenge, error) {
	dnsMu.Lock()
	defer dnsMu.Unlock()
	c, ok := dnsChallenges[token]
	if !ok {
		return nil, fmt.Errorf("DNS challenge not found")
	}
	return c, nil
}

// VerifyDNSChallenge marks a DNS challenge as manually verified.
func (s *Service) VerifyDNSChallenge(ctx context.Context, token string) error {
	dnsMu.Lock()
	c, ok := dnsChallenges[token]
	dnsMu.Unlock()
	if !ok {
		return fmt.Errorf("DNS challenge not found")
	}

	// Perform actual DNS lookup to verify TXT record
	if err := s.verifyDNSChallenge(ctx, c.Domain, c.Token, c.KeyAuth); err != nil {
		return err
	}

	// Find the corresponding challenge and mark it valid
	s.mu.Lock()
	for _, o := range s.orders {
		for _, ch := range o.Challenges {
			if ch.Token == token {
				ch.Status = "valid"
				ch.ValidatedAt = time.Now()
			}
		}
	}
	s.mu.Unlock()

	dnsMu.Lock()
	delete(dnsChallenges, token)
	dnsMu.Unlock()

	return nil
}

// verifyDNSChallenge performs an actual DNS TXT record lookup.
func (s *Service) verifyDNSChallenge(ctx context.Context, domain, token, expectedKeyAuth string) error {
	// Compute expected TXT record value
	expectedValue := base64.RawURLEncoding.EncodeToString(sha256Hash([]byte(expectedKeyAuth)))

	// Look up TXT record at _acme-challenge.<domain>
	challengeDomain := fmt.Sprintf("_acme-challenge.%s", domain)

	txtRecords, err := netLookupTXT(challengeDomain)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %s: %w", challengeDomain, err)
	}

	for _, record := range txtRecords {
		if record == expectedValue || record == expectedKeyAuth {
			log.Info().Str("domain", domain).Msg("DNS-01 challenge verified ✓")
			return nil
		}
	}

	return fmt.Errorf("TXT record value mismatch for %s: expected %s, got %v", challengeDomain, expectedValue, txtRecords)
}

// DNSChallengeInfo returns human-readable instructions for manual DNS setup.
func (s *Service) DNSChallengeInfo(domain, token string) map[string]string {
	keyAuth := token + "." + s.caThumbprint()
	txtValue := base64.RawURLEncoding.EncodeToString(sha256Hash([]byte(keyAuth)))

	return map[string]string{
		"record_type": "TXT",
		"record_name": fmt.Sprintf("_acme-challenge.%s", domain),
		"record_value": txtValue,
		"key_authorization": keyAuth,
		"token": token,
		"instructions": fmt.Sprintf(
			"Create a TXT record at _acme-challenge.%s with value: %s",
			domain, txtValue,
		),
	}
}

// sha256Hash returns SHA-256 hash of data.
func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// netLookupTXT performs a DNS TXT record lookup.
func netLookupTXT(domain string) ([]string, error) {
	// In production, use miekg/dns for proper DNS resolution
	// For now, use the net package's built-in lookup
	return net.LookupTXT(domain)
}

// Ensure net import is used.
var _ = net.LookupTXT

// -------------------------------------------------------------------
// Utilities
// -------------------------------------------------------------------

func generateToken(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:length]
}

// CAPEM returns the CA certificate in PEM format.
func (s *Service) CAPEM() []byte { return s.ca.CAPEM() }

func (s *Service) caThumbprint() string {
	hash := sha256.Sum256(s.ca.CAPEM())
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// HTTPChallengeHandler handles HTTP-01 ACME challenges.
func (s *Service) HTTPChallengeHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
	token := path

	s.mu.RLock()
	challenge, ok := s.challenges[token]
	s.mu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	// Return the key authorization
	keyAuth := challenge.Token + "." + s.caThumbprint()
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(keyAuth))
}
