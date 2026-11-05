package sqlite

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// parseDSN consumes only recognized driver parameters. Native URI and custom VFS
// parameters remain in the filename; explicit Options are applied afterwards.
func parseDSN(dsn string, cfg *config) (string, error) {
	fragment := ""
	if strings.HasPrefix(dsn, "file:") {
		if before, after, ok := strings.Cut(dsn, "#"); ok {
			dsn, fragment = before, "#"+after
		}
	}
	path, query, found := strings.Cut(dsn, "?")
	if !found {
		return dsn + fragment, nil
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return "", fmt.Errorf("sqlite: invalid DSN query: %w", err)
	}
	get := func(keys ...string) (string, bool, error) {
		var value string
		seen := false
		for _, key := range keys {
			if list, ok := values[key]; ok {
				if len(list) != 1 || seen {
					return "", false, fmt.Errorf("sqlite: repeated or conflicting DSN parameter %s", key)
				}
				value, seen = list[0], true
				delete(values, key)
			}
		}
		return value, seen, nil
	}
	apply := func(keys []string, fn func(string) error) error {
		value, ok, err := get(keys...)
		if err != nil || !ok {
			return err
		}
		if err := fn(value); err != nil {
			return fmt.Errorf("sqlite: DSN %s: %w", keys[0], err)
		}
		return nil
	}
	if err := apply([]string{"_busy_timeout", "_timeout"}, func(value string) error {
		milliseconds, err := strconv.ParseInt(value, 10, 32)
		if err != nil || milliseconds < 0 {
			return fmt.Errorf("expected nonnegative C int milliseconds")
		}
		cfg.busyMS = int32(milliseconds)
		return nil
	}); err != nil {
		return "", err
	}
	if err := apply([]string{"_txlock"}, func(value string) error { return WithTransactionMode(value)(cfg) }); err != nil {
		return "", err
	}
	if err := apply([]string{"_loc"}, func(value string) error {
		var location *time.Location
		var err error
		if value == "auto" {
			location = time.Local
		} else {
			location, err = time.LoadLocation(value)
		}
		if err != nil {
			return err
		}
		return WithTimeLocation(location)(cfg)
	}); err != nil {
		return "", err
	}
	if err := apply([]string{"_mutex"}, func(value string) error {
		if !strings.EqualFold(value, "full") {
			return fmt.Errorf("%w: connections require _mutex=full", ErrUnsupported)
		}
		return nil
	}); err != nil {
		return "", err
	}
	booleans := []struct {
		name    string
		aliases []string
	}{
		{"foreign_keys", []string{"_foreign_keys", "_fk"}},
		{"defer_foreign_keys", []string{"_defer_foreign_keys", "_defer_fk"}},
		{"ignore_check_constraints", []string{"_ignore_check_constraints"}},
		{"query_only", []string{"_query_only"}},
		{"recursive_triggers", []string{"_recursive_triggers", "_rt"}},
	}
	for _, parameter := range booleans {
		if err := apply(parameter.aliases, func(value string) error {
			value, err := dsnBool(value)
			if err == nil {
				cfg.pragmas = append(cfg.pragmas, "PRAGMA "+parameter.name+"="+value)
			}
			return err
		}); err != nil {
			return "", err
		}
	}
	enums := []struct {
		name             string
		aliases, allowed []string
	}{
		{"journal_mode", []string{"_journal_mode", "_journal"}, []string{"DELETE", "TRUNCATE", "PERSIST", "MEMORY", "WAL", "OFF"}},
		{"synchronous", []string{"_synchronous", "_sync"}, []string{"0", "1", "2", "3", "OFF", "NORMAL", "FULL", "EXTRA"}},
		{"locking_mode", []string{"_locking_mode", "_locking"}, []string{"NORMAL", "EXCLUSIVE"}},
		{"auto_vacuum", []string{"_auto_vacuum", "_vacuum"}, []string{"0", "1", "2", "NONE", "FULL", "INCREMENTAL"}},
	}
	for _, parameter := range enums {
		if err := apply(parameter.aliases, func(value string) error {
			value = strings.ToUpper(value)
			for _, allowed := range parameter.allowed {
				if value == allowed {
					if parameter.name == "journal_mode" {
						cfg.journal, cfg.wal = value, value == "WAL"
					} else {
						cfg.pragmas = append(cfg.pragmas, "PRAGMA "+parameter.name+"="+value)
					}
					return nil
				}
			}
			return fmt.Errorf("invalid %s value %q", parameter.name, value)
		}); err != nil {
			return "", err
		}
	}
	if err := apply([]string{"_cache_size"}, func(value string) error {
		number, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return err
		}
		cfg.pragmas = append(cfg.pragmas, "PRAGMA cache_size="+strconv.FormatInt(number, 10))
		return nil
	}); err != nil {
		return "", err
	}
	if err := apply([]string{"_secure_delete"}, func(value string) error {
		if strings.EqualFold(value, "fast") {
			cfg.pragmas = append(cfg.pragmas, "PRAGMA secure_delete=FAST")
			return nil
		}
		value, err := dsnBool(value)
		if err == nil {
			cfg.pragmas = append(cfg.pragmas, "PRAGMA secure_delete="+value)
		}
		return err
	}); err != nil {
		return "", err
	}
	for key := range values {
		if key == "_writable_schema" || key == "_case_sensitive_like" || key == "_cslike" || key == "_auth" || key == "_auth_user" || key == "_auth_pass" || key == "_auth_crypt" || key == "_auth_salt" {
			return "", fmt.Errorf("%w: DSN parameter %s", ErrUnsupported, key)
		}
	}
	if err := apply([]string{"_stmt_cache_size"}, func(value string) error {
		size, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return err
		}
		return WithStatementCache(int(size))(cfg)
	}); err != nil {
		return "", err
	}
	if !strings.HasPrefix(path, "file:") {
		return path, nil
	}
	if len(values) == 0 {
		return path + fragment, nil
	}
	return path + "?" + values.Encode() + fragment, nil
}

func dsnBool(value string) (string, error) {
	switch strings.ToLower(value) {
	case "1", "true", "on", "yes":
		return "1", nil
	case "0", "false", "off", "no":
		return "0", nil
	default:
		return "", fmt.Errorf("expected boolean, got %q", value)
	}
}
