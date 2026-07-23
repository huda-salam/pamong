package scheduler

import "github.com/huda-salam/pamong/core"

// ErrInvalidCron dipublikasikan saat ekspresi cron tidak valid — di-catch di pintu masuk
// (saat ParseCron), bukan saat runtime (HTTP 422).
func ErrInvalidCron(expr, reason string) error {
	return core.ErrValidation("cron_expr", "ekspresi cron "+expr+" tidak valid: "+reason)
}

// ErrJobKeyExists dipublikasikan saat mendaftarkan JobKey yang sudah terpakai (HTTP 409).
// Registry immutable setelah bootstrap — key ganda menandakan salah wiring.
func ErrJobKeyExists(key string) error {
	return core.ErrConflict("job key " + key + " sudah terdaftar")
}

// ErrJobKeyNotRegistered dipublikasikan saat schedule merujuk JobKey yang tak ada di
// Registry — analog strategy-key-must-be-registered (HTTP 404).
func ErrJobKeyNotRegistered(key string) error {
	return core.ErrNotFound("job handler", key)
}

// ErrNilJobFunc dipublikasikan saat handler nil didaftarkan (HTTP 422).
func ErrNilJobFunc(key string) error {
	return core.ErrValidation("job_func", "handler untuk job "+key+" tidak boleh nil")
}
