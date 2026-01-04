package db

import (
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func Connect() (*sqlx.DB, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		panic("DATABASE_URL environment variable is required")
	}
	
	// Add parameters to disable prepared statements for Neon pooler compatibility
	// Neon's connection pooler doesn't support prepared statements
	parsedURL, err := url.Parse(dbURL)
	if err == nil {
		query := parsedURL.Query()
		// Set prefer_simple_protocol=1 to use simple query protocol (no prepared statements)
		query.Set("prefer_simple_protocol", "1")
		// Also set binary_parameters=yes as an additional safeguard
		query.Set("binary_parameters", "yes")
		parsedURL.RawQuery = query.Encode()
		dbURL = parsedURL.String()
	} else {
		// Fallback: append parameters if URL parsing fails
		separator := "?"
		if strings.Contains(dbURL, "?") {
			separator = "&"
		}
		if !strings.Contains(dbURL, "prefer_simple_protocol") {
			dbURL = dbURL + separator + "prefer_simple_protocol=1"
			separator = "&"
		}
		if !strings.Contains(dbURL, "binary_parameters") {
			dbURL = dbURL + separator + "binary_parameters=yes"
		}
	}
	
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		return nil, err
	}
	
	// Configure connection pool for cloud databases (Neon, etc.)
	// Neon pooler works best with shorter connection lifetimes
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Second) // Shorter for Neon pooler compatibility
	db.SetConnMaxIdleTime(10 * time.Second)  // Shorter idle time for Neon
	
	return db, nil
}

