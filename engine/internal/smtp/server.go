package smtp

import (
	"fmt"
	"log/slog"
	"time"

	gosmtp "github.com/emersion/go-smtp"

	"github.com/bangsmackpow/pocketarchive/engine/internal/config"
)

func NewServer(cfg config.Config, backend *Backend, log *slog.Logger) (*gosmtp.Server, error) {
	tlsCfg, generated, err := BuildTLSConfig(cfg.SMTPTLSCertFile, cfg.SMTPTLSKeyFile, cfg.SMTPHostname)
	if err != nil {
		return nil, err
	}
	if generated && log != nil {
		log.Warn("SMTP is using an auto-generated self-signed certificate; mount a trusted certificate for Office 365 journaling", "hostname", cfg.SMTPHostname)
	}
	srv := gosmtp.NewServer(backend)
	srv.Addr = cfg.SMTPAddr
	srv.Domain = cfg.SMTPHostname
	srv.TLSConfig = tlsCfg
	srv.MaxMessageBytes = cfg.SMTPMaxBytes
	srv.MaxRecipients = 100
	srv.ReadTimeout = 5 * time.Minute
	srv.WriteTimeout = 5 * time.Minute
	srv.AllowInsecureAuth = false
	srv.ErrorLog = slogAdapter{log: log}
	if log != nil {
		log.Info("SMTP journal listener configured", "addr", cfg.SMTPAddr, "hostname", cfg.SMTPHostname, "tenant", backend.tenant.Slug)
	}
	return srv, nil
}

type slogAdapter struct{ log *slog.Logger }

func (s slogAdapter) Printf(format string, v ...interface{}) {
	if s.log != nil {
		s.log.Warn(fmt.Sprintf(format, v...))
	}
}

func (s slogAdapter) Println(v ...interface{}) {
	if s.log != nil {
		s.log.Warn(fmt.Sprint(v...))
	}
}
