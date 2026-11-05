package sqlite

import (
	"database/sql/driver"
	"strings"
	"time"
)

// Private immutable formats avoid races caused by global mutable format lists.
var timestampFormats = [...]string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05", "2006-01-02T15:04:05",
	"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02",
}

func isTimestamp(name string) bool {
	return name == "DATE" || name == "DATETIME" || name == "TIMESTAMP"
}

func (r *rows) convertValues(values []driver.Value) error {
	cfg := &r.s.c.cfg
	if !cfg.parseTime && !cfg.parseBool {
		return nil
	}
	r.s.loadDecltypes()
	for i, value := range values {
		name := strings.ToUpper(r.s.decltypes[i])
		if name == "BOOLEAN" && cfg.parseBool {
			if integer, ok := value.(int64); ok {
				values[i] = integer > 0
			}
			continue
		}
		if !isTimestamp(name) || !cfg.parseTime {
			continue
		}
		var parsed time.Time
		switch value := value.(type) {
		case int64:
			if value > 1e12 || value < -1e12 {
				parsed = time.UnixMilli(value).UTC()
			} else {
				parsed = time.Unix(value, 0).UTC()
			}
		case string:
			for _, format := range timestampFormats {
				if result, err := time.ParseInLocation(format, strings.TrimSuffix(value, "Z"), time.UTC); err == nil {
					parsed = result
					break
				}
			}
		default:
			continue
		}
		if cfg.location != nil {
			parsed = parsed.In(cfg.location)
		}
		values[i] = parsed
	}
	return nil
}
