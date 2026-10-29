package engine

import (
	"net/http"
	"path/filepath"

	"github.com/openloadbalancer/olb/internal/acme"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/logging"
)

// initACME creates the certificate manager for tls.acme. The certificate it
// obtains is added to the TLS manager, and becomes the default certificate
// when none was configured.
func (e *Engine) initACME(cfg *config.ACME, configPath string) error {
	storage := cfg.StorageDir
	if storage == "" {
		storage = filepath.Join(filepath.Dir(configPath), "acme")
	}
	m, err := acme.NewManager(acme.ManagerConfig{
		DirectoryURL: cfg.DirectoryURL,
		Email:        cfg.Email,
		Domains:      cfg.Domains,
		StorageDir:   storage,
		Install:      e.installACMECertificate,
		Log: func(msg string, err error) {
			if err != nil {
				e.logger.Warn(msg, logging.Error(err))
				return
			}
			e.logger.Info(msg)
		},
	})
	if err != nil {
		return err
	}
	e.acmeManager = m
	e.logger.Info("ACME certificate manager initialized",
		logging.Any("domains", cfg.Domains),
		logging.String("storage", storage),
	)
	return nil
}

func (e *Engine) installACMECertificate(certPEM, keyPEM []byte) error {
	cert, err := e.tlsManager.LoadCertificateFromPEM(certPEM, keyPEM)
	if err != nil {
		return err
	}
	e.tlsManager.AddCertificate(cert)
	e.tlsManager.SetDefaultCertificate(cert)
	return nil
}

// withACMEChallenge answers HTTP-01 challenges ahead of the proxy and its
// middleware (WAF, auth, redirects), which must not see them.
func (e *Engine) withACMEChallenge(h http.Handler) http.Handler {
	if e.acmeManager == nil {
		return h
	}
	return e.acmeManager.Wrap(h)
}
