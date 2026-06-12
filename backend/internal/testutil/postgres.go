package testutil

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type PostgresInstance struct {
	DataDir    string
	Port       int
	ConnString string
	logFile    string
}

func StartPostgres(t testing.TB) *PostgresInstance {
	t.Helper()

	dataDir := filepath.Join(t.TempDir(), "pgdata")
	port := freePort(t)
	logFile := filepath.Join(filepath.Dir(dataDir), "postgres.log")

	socketDir := filepath.Dir(dataDir)
	runCmd(t, exec.Command("initdb", "-D", dataDir, "-U", "postgres", "-A", "trust", "--no-instructions"))
	// wal_level=logical lets WAL change-capture integration tests run against
	// the same harness; the overhead for unrelated tests is negligible.
	runCmd(t, exec.Command("pg_ctl", "-D", dataDir, "-l", logFile, "-o", fmt.Sprintf("-F -p %d -c listen_addresses=127.0.0.1 -c unix_socket_directories=%s -c wal_level=logical -c max_replication_slots=4 -c max_wal_senders=4", port, socketDir), "-w", "start"))

	inst := &PostgresInstance{
		DataDir:    dataDir,
		Port:       port,
		ConnString: fmt.Sprintf("postgres://postgres@127.0.0.1:%d/postgres?sslmode=disable", port),
		logFile:    logFile,
	}

	t.Cleanup(func() {
		cmd := exec.Command("pg_ctl", "-D", dataDir, "-m", "immediate", "-w", "stop")
		_ = cmd.Run()
	})

	return inst
}

func freePort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func runCmd(t testing.TB, cmd *exec.Cmd) {
	t.Helper()
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %q failed: %v\n%s", cmd.String(), err, string(out))
	}
}
