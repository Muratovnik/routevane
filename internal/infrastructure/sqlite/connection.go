package sqlite

import (
	"net/url"
	"path/filepath"
	"strings"
)

var connectionPragmas = [...]string{
	"foreign_keys = ON",
	"busy_timeout = 5000",
	"synchronous = FULL",
	"trusted_schema = OFF",
	// Renames must update dependent foreign keys, triggers and indexes.
	"legacy_alter_table = OFF",
}

// sqliteDSN configures every physical connection, including one replacing a
// connection the driver discarded after cancellation. WAL is persistent and
// remains part of the verified startup sequence in configureConnection.
func sqliteDSN(path string) string {
	query := make(url.Values)
	for _, pragma := range connectionPragmas {
		query.Add("_pragma", pragma)
	}
	return sqliteFileURI(path) + "?" + query.Encode()
}

// sqliteFileURI preserves a private absolute filename as a URI path. In
// particular, filename punctuation cannot become SQLite query parameters.
// A Windows drive needs a leading slash; a UNC path keeps both leading slashes
// in Path, with no URI authority that SQLite could interpret as a hostname.
func sqliteFileURI(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
