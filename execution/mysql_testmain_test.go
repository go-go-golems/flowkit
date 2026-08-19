package execution

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-go-golems/flowkit/internal/testsupport/mysqltest"
)

func TestMain(m *testing.M) {
	var instance *mysqltest.Instance
	if os.Getenv("FLOWKIT_MYSQL_TESTCONTAINERS") == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var err error
		instance, err = mysqltest.Start(ctx)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "start disposable flowkit test MySQL: %v\n", err)
			os.Exit(1)
		}
		if err := os.Setenv("FLOWKIT_MYSQL_CACHE_DSN", instance.AppDSN); err != nil {
			fmt.Fprintf(os.Stderr, "set flowkit MySQL application DSN: %v\n", err)
			os.Exit(1)
		}
		if err := os.Setenv("FLOWKIT_MYSQL_TEST_ADMIN_DSN", instance.AdminDSN); err != nil {
			fmt.Fprintf(os.Stderr, "set flowkit MySQL administrator DSN: %v\n", err)
			os.Exit(1)
		}
	}

	code := m.Run()
	if instance != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := instance.Close(ctx)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "terminate disposable flowkit test MySQL: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}
