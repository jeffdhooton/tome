package schema

import (
	"fmt"
	"strings"
)

// Introspector connects to a database and extracts its full schema.
type Introspector interface {
	Connect(dsn string) error
	Introspect() (*SchemaSnapshot, error)
	Close() error
}

// ParseDSN parses a DSN string and returns (dbType, driverDSN, error).
// Accepts URI format (mysql://..., postgres://...) and driver-native formats.
func ParseDSN(dsn string) (dbType string, driverDSN string, err error) {
	if strings.HasPrefix(dsn, "mysql://") {
		return "mysql", parseMySQLURI(dsn), nil
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return "postgres", dsn, nil
	}
	// Try to detect from format. go-sql-driver/mysql uses user:pass@tcp(host)/db
	if strings.Contains(dsn, "tcp(") || strings.Contains(dsn, "@/") {
		return "mysql", dsn, nil
	}
	// If it contains host= or sslmode= it's likely postgres
	if strings.Contains(dsn, "host=") || strings.Contains(dsn, "sslmode=") {
		return "postgres", dsn, nil
	}
	return "", "", fmt.Errorf("cannot determine database type from DSN — use mysql:// or postgres:// prefix")
}

// parseMySQLURI converts mysql://user:pass@host:port/db to go-sql-driver format.
func parseMySQLURI(uri string) string {
	// mysql://user:pass@host:port/db?params -> user:pass@tcp(host:port)/db?params
	rest := strings.TrimPrefix(uri, "mysql://")

	// Split on ? first to preserve query params
	var params string
	if idx := strings.Index(rest, "?"); idx >= 0 {
		params = rest[idx:]
		rest = rest[:idx]
	}

	// Split user info from host
	var userInfo, hostPath string
	if idx := strings.LastIndex(rest, "@"); idx >= 0 {
		userInfo = rest[:idx]
		hostPath = rest[idx+1:]
	} else {
		hostPath = rest
	}

	// Split host from path
	var host, dbName string
	if idx := strings.Index(hostPath, "/"); idx >= 0 {
		host = hostPath[:idx]
		dbName = hostPath[idx+1:]
	} else {
		host = hostPath
	}

	// Default port
	if host != "" && !strings.Contains(host, ":") {
		host += ":3306"
	}

	result := ""
	if userInfo != "" {
		result = userInfo + "@"
	}
	if host != "" {
		result += "tcp(" + host + ")"
	}
	result += "/" + dbName + params

	return result
}
