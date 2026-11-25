package smtp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

// BackendConfig is the subset of engine configuration needed by the SMTP
// receiver.
type BackendConfig struct {
	SpoolRoot      string
	Tenant         store.Tenant
	AllowedDomains []string
	AllowedCIDRs   []string
	AuthUsername   string
	AuthPassword   string
	MaxBytes       int64
}

type Backend struct {
	store            *store.Store
	spoolRoot        string
	tenant           store.Tenant
	allowedDomains   map[string]struct{}
	allowedNets      []*net.IPNet
	authUsername     string
	authPassword     string
	maxBytes         int64
	log              *slog.Logger
	policyConfigured bool
}

func NewBackend(ctx context.Context, st *store.Store, cfg BackendConfig, log *slog.Logger) (*Backend, error) {
	if cfg.Tenant.ID == uuid.Nil {
		return nil, errors.New("SMTP tenant is not configured")
	}
	b := &Backend{
		store:          st,
		spoolRoot:      cfg.SpoolRoot,
		tenant:         cfg.Tenant,
		allowedDomains: make(map[string]struct{}, len(cfg.AllowedDomains)),
		authUsername:   cfg.AuthUsername,
		authPassword:   cfg.AuthPassword,
		maxBytes:       cfg.MaxBytes,
		log:            log,
	}
	for _, d := range cfg.AllowedDomains {
		b.allowedDomains[strings.ToLower(strings.TrimSpace(d))] = struct{}{}
	}
	for _, c := range cfg.AllowedCIDRs {
		_, network, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("invalid SMTP_ALLOWED_IPS entry %q: %w", c, err)
		}
		b.allowedNets = append(b.allowedNets, network)
	}
	if b.maxBytes <= 0 {
		b.maxBytes = 100 * 1024 * 1024
	}
	if len(b.allowedDomains) == 0 && len(b.allowedNets) == 0 && b.authUsername == "" {
		log.Warn("SMTP has no sender policy configured; all mail will be rejected until SMTP_ALLOWED_DOMAINS, SMTP_ALLOWED_IPS or SMTP_AUTH is set")
	} else {
		b.policyConfigured = true
	}
	return b, nil
}

func (b *Backend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	return &session{backend: b, conn: c}, nil
}

type session struct {
	backend       *Backend
	conn          *gosmtp.Conn
	from          string
	recipients    []string
	authenticated bool
}

func (s *session) Reset() {
	s.from = ""
	s.recipients = nil
}

func (s *session) Logout() error { return nil }

func (s *session) Mail(from string, _ *gosmtp.MailOptions) error {
	if !s.authorized(from) {
		return &gosmtp.SMTPError{
			Code:         550,
			EnhancedCode: gosmtp.EnhancedCode{5, 7, 1},
			Message:      "sender is not authorized for journaling",
		}
	}
	s.from = from
	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	if len(s.recipients) >= 100 {
		return &gosmtp.SMTPError{Code: 452, Message: "too many recipients"}
	}
	s.recipients = append(s.recipients, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	if s.from == "" && !s.authenticated {
		return &gosmtp.SMTPError{Code: 550, EnhancedCode: gosmtp.EnhancedCode{5, 7, 1}, Message: "MAIL FROM required"}
	}
	dir := filepath.Join(s.backend.spoolRoot, "journal", s.backend.tenant.ID.String())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return &gosmtp.SMTPError{Code: 451, Message: "cannot create spool directory"}
	}
	tmp, err := os.CreateTemp(dir, ".incoming-*.eml")
	if err != nil {
		return &gosmtp.SMTPError{Code: 451, Message: "cannot create spool file"}
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o640); err != nil {
		cleanup()
		return &gosmtp.SMTPError{Code: 451, Message: "cannot secure spool file"}
	}
	written, err := io.Copy(tmp, io.LimitReader(r, s.backend.maxBytes+1))
	if err != nil {
		cleanup()
		return &gosmtp.SMTPError{Code: 451, Message: "failed to receive message"}
	}
	if written > s.backend.maxBytes {
		cleanup()
		return &gosmtp.SMTPError{
			Code:         552,
			EnhancedCode: gosmtp.EnhancedCode{5, 3, 4},
			Message:      "message exceeds maximum size",
		}
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return &gosmtp.SMTPError{Code: 451, Message: "failed to flush spool file"}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return &gosmtp.SMTPError{Code: 451, Message: "failed to close spool file"}
	}
	finalPath := filepath.Join(dir, uuid.NewString()+".eml")
	if err := os.Rename(tmpName, finalPath); err != nil {
		_ = os.Remove(tmpName)
		return &gosmtp.SMTPError{Code: 451, Message: "failed to commit spool file"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	remote := remoteIP(s.conn)
	if err := s.backend.store.EnqueueJournal(ctx, s.backend.tenant.ID, finalPath, s.from, remote); err != nil {
		_ = os.Remove(finalPath)
		if s.backend.log != nil {
			s.backend.log.Error("failed to enqueue journal message", "error", err)
		}
		return &gosmtp.SMTPError{Code: 451, Message: "temporary archive failure; retry later"}
	}
	if s.backend.log != nil {
		s.backend.log.Info("journal message spooled", "from", s.from, "bytes", written, "path", finalPath, "remote", remote)
	}
	s.Reset()
	return nil
}

func (s *session) AuthMechanisms() []string {
	if s.backend.authUsername == "" {
		return nil
	}
	return []string{sasl.Plain}
}

func (s *session) Auth(mech string) (sasl.Server, error) {
	if s.backend.authUsername == "" || !strings.EqualFold(mech, sasl.Plain) {
		return nil, gosmtp.ErrAuthUnknownMechanism
	}
	return sasl.NewPlainServer(func(_, username, password string) error {
		userOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.backend.authUsername)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(password), []byte(s.backend.authPassword)) == 1
		if !userOK || !passOK {
			return gosmtp.ErrAuthFailed
		}
		s.authenticated = true
		return nil
	}), nil
}

func (s *session) authorized(from string) bool {
	if s.authenticated {
		return true
	}
	b := s.backend
	if !b.policyConfigured {
		return false
	}
	ipMatch := false
	if remote := net.ParseIP(remoteIP(s.conn)); remote != nil {
		for _, n := range b.allowedNets {
			if n.Contains(remote) {
				ipMatch = true
				break
			}
		}
	}

	if b.authUsername != "" {
		// When SMTP AUTH is configured, unauthenticated clients are only
		// accepted from an explicitly trusted CIDR.
		return len(b.allowedNets) > 0 && ipMatch
	}

	ipOK := len(b.allowedNets) == 0 || ipMatch
	domainOK := len(b.allowedDomains) == 0
	if at := strings.LastIndex(from, "@"); at >= 0 {
		_, domainOK = b.allowedDomains[strings.ToLower(from[at+1:])]
	}
	return ipOK && domainOK
}

func remoteIP(c *gosmtp.Conn) string {
	if c == nil || c.Conn() == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(c.Conn().RemoteAddr().String())
	if err == nil {
		return host
	}
	return c.Conn().RemoteAddr().String()
}
