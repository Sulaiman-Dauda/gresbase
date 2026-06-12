package app

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// backupAlertMinInterval throttles backup failure alert emails: with a tight
// backup_cron and a persistent failure (full disk, dead S3 bucket, ...) at
// most one alert per hour is sent instead of spamming every superuser inbox.
const backupAlertMinInterval = time.Hour

// backupFailureAlerter emails every superuser when a SCHEDULED backup fails.
// Manual backup failures never alert: the human who triggered them sees the
// error in the API response. All dependencies are injected as closures so the
// alerter is unit testable without a database or an SMTP server.
type backupFailureAlerter struct {
	enabled    func() bool                                   // mailer configured?
	listEmails func(ctx context.Context) ([]string, error)   // superuser enumeration (_admins)
	send       func(to, errMsg, when, instance string) error // one email per recipient
	instance   string                                        // public address of this instance
	interval   time.Duration                                 // throttle window
	now        func() time.Time                              // clock (overridable in tests)

	mu        sync.Mutex
	lastAlert time.Time // in-memory "last alert sent"; resets on restart
}

// newBackupFailureAlerter wires the alerter against the app's real services.
func newBackupFailureAlerter(app *App) *backupFailureAlerter {
	return &backupFailureAlerter{
		enabled:    app.mailerSvc.Enabled,
		listEmails: app.authService.ListAdminEmails,
		send:       app.mailerSvc.SendBackupFailedAlert,
		instance:   app.mailerSvc.BaseURL(),
		interval:   backupAlertMinInterval,
		now:        time.Now,
	}
}

// notify emails all superusers about a failed scheduled backup. Guards:
// it is a no-op when the mailer is not configured, and repeated failures
// within the throttle window are suppressed.
func (a *backupFailureAlerter) notify(ctx context.Context, backupErr error) {
	if a == nil || backupErr == nil {
		return
	}
	if a.enabled != nil && !a.enabled() {
		log.Debug().Msg("Backup failure alert skipped: mailer not configured")
		return
	}

	now := a.now()
	a.mu.Lock()
	if !a.lastAlert.IsZero() && now.Sub(a.lastAlert) < a.interval {
		a.mu.Unlock()
		log.Debug().
			Time("last_alert", a.lastAlert).
			Dur("throttle", a.interval).
			Msg("Backup failure alert suppressed (throttled)")
		return
	}
	a.lastAlert = now
	a.mu.Unlock()

	emails, err := a.listEmails(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Backup failure alert: cannot enumerate superusers")
		return
	}
	if len(emails) == 0 {
		log.Warn().Msg("Backup failure alert: no superusers to notify")
		return
	}

	when := now.UTC().Format(time.RFC3339)
	sent := 0
	for _, email := range emails {
		if err := a.send(email, backupErr.Error(), when, a.instance); err != nil {
			log.Error().Err(err).Str("to", email).Msg("Backup failure alert: send failed")
			continue
		}
		sent++
	}
	log.Info().
		Int("recipients", sent).
		Str("instance", a.instance).
		Msg("Backup failure alert sent to superusers")
}
