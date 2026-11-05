package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Version describes the default system library. Connector methods describe the
// library selected by WithLibrary/WithBundled instead.
func Version() (string, error) {
	c, err := NewConnector(":memory:")
	if err != nil {
		return "", err
	}
	return c.Version(), nil
}
func CompileOptions() ([]string, error) {
	c, err := NewConnector(":memory:")
	if err != nil {
		return nil, err
	}
	return c.CompileOptions(), nil
}
func HasFeature(name string) (bool, error) {
	c, err := NewConnector(":memory:")
	if err != nil {
		return false, err
	}
	return c.HasFeature(name)
}
func (c *Connector) Version() string     { return c.api.Libversion() }
func (c *Connector) SourceID() string    { return c.api.SourceID() }
func (c *Connector) VersionNumber() int  { return int(c.api.LibversionNumber()) }
func (c *Connector) LibraryPath() string { return c.api.Path }
func (c *Connector) CompileOptions() []string {
	var options []string
	for i := int32(0); ; i++ {
		option := c.api.CompileOptionGet(i)
		if option == "" {
			break
		}
		options = append(options, option)
	}
	return options
}

// HasFeature probes an isolated in-memory connection against this connector's
// native library. It never changes the application's database or runs init hooks.
func (c *Connector) HasFeature(name string) (bool, error) {
	var query string
	switch strings.ToLower(name) {
	case "fts3", "fts4", "fts5", "rtree", "geopoly":
		query = "CREATE VIRTUAL TABLE feature_probe USING " + strings.ToLower(name) + "(x"
		if strings.EqualFold(name, "rtree") {
			query += ", minX, maxX"
		}
		if strings.EqualFold(name, "geopoly") {
			query = "CREATE VIRTUAL TABLE feature_probe USING geopoly("
		}
		query += ")"
	case "json":
		query = `SELECT json_valid('{"x":1}')`
	case "jsonb":
		query = `SELECT jsonb('{"x":1}')`
	case "math":
		query = "SELECT sqrt(4)"
	case "dbstat":
		query = "SELECT * FROM dbstat LIMIT 0"
	case "extensions":
		return c.api.ConfigExtension != nil && c.api.LoadExtension != nil && c.api.EnableLoadExtension != nil, nil
	case "row_read_v1":
		return c.api.ReadRow != nil, nil
	case "exec_scalar_v1":
		return c.api.ExecScalar != nil, nil
	case "serialize":
		return c.api.Serialize != nil && c.api.Deserialize != nil, nil
	case "authorizer":
		return c.api.SetAuthorizer != nil, nil
	case "trace":
		return c.api.TraceV2 != nil, nil
	default:
		return false, fmt.Errorf("%w: unknown feature %q", ErrUnsupported, name)
	}
	probe := &Connector{dsn: ":memory:", api: c.api, cfg: config{txMode: "DEFERRED"}}
	dc, err := probe.Connect(context.Background())
	if err != nil {
		return false, err
	}
	defer dc.Close()
	_, err = dc.(*Conn).ExecContext(context.Background(), query, nil)
	if err == nil {
		return true, nil
	}
	// Only absence errors indicate unavailable features; other failures propagate.
	if e, ok := errors.AsType[*Error](err); ok && e.Code == 1 && (strings.Contains(e.Message, "no such module") || strings.Contains(e.Message, "no such function") || strings.Contains(e.Message, "no such table")) {
		return false, nil
	}
	return false, err
}
