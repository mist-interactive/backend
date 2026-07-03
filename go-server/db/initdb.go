package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func InitDB() (*bun.DB, error) {
	_ = godotenv.Load("../.env")

	user := os.Getenv("DB_USER")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	dbName := os.Getenv("DB_NAME")
	sslMode := os.Getenv("DB_SSLMODE")
	password := getDBPassword()

	if password == "" {
		return nil, fmt.Errorf("No DB password set")
	}

	db_string := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, password, host, port, dbName, sslMode)

	if sslMode != "disable" {
		db_string += "&sslrootcert=/certs/ca.crt"
	}

	sqlDB := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(db_string)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, err
	}
	fmt.Println("Successfully connected to the database!", dbName)
	return bun.NewDB(sqlDB, pgdialect.New()), nil
}

func getDBPassword() string {
	if password := os.Getenv("DB_PASSWORD"); password != "" { //for local testing
		return password
	}
	if path := os.Getenv("POSTGRES_PASSWORD_FILE"); path != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("Failed to read database password file: %v", err)
		}
		return strings.TrimSpace(string(content))
	}
	return ""
}
