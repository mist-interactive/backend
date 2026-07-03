package models_test

import (
	"dbBackend/internal/testutil"
	"os"
	"testing"

	"github.com/uptrace/bun"
)

var testDB *bun.DB

func TestMain(m *testing.M) {
	var cleanup func()
	testDB, cleanup = testutil.SetupTestDB()
	exitCode := m.Run()
	cleanup()
	os.Exit(exitCode)
}
