package main

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectusdHelperProcess(t *testing.T) {
	if os.Getenv("EFFECTUSD_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"effectusd"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing process argument separator")
}

func TestDaemonCLIRequiresExplicitModeAndRejectsPositionals(t *testing.T) {
	for _, test := range []struct {
		args    []string
		code    int
		message string
	}{
		{[]string{"--help"}, 0, "mode"},
		{nil, 1, "serve mode requires exactly one"},
		{[]string{"extra"}, 1, "positional"},
		{[]string{"--mode=unknown"}, 1, "--mode must"},
		{[]string{"--bundle=absent", "extra"}, 1, "positional"},
		{[]string{"--mode=migrate", "--bundle=absent"}, 1, "migration mode does not accept"},
		{[]string{"--mode=migrate", "--migrate-only"}, 1, "not both"},
		{[]string{"--mode=migrate"}, 1, "postgres-dsn"},
		{[]string{"--migrate-only"}, 1, "postgres-dsn"},
		{[]string{"--database-migrations=wrong"}, 1, "--database-migrations must"},
		{[]string{"--unknown"}, 2, "flag provided but not defined"},
	} {
		command := exec.Command(os.Args[0], append([]string{"-test.run=^TestEffectusdHelperProcess$", "--"}, test.args...)...)
		// No CLI test may reach an operator's configured database.
		command.Env = append(os.Environ(), "EFFECTUSD_TEST_PROCESS=1", "EFFECTUS_POSTGRES_DSN=")
		output, err := command.CombinedOutput()
		if test.code == 0 {
			require.NoError(t, err, string(output))
		} else {
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit)
			require.Equal(t, test.code, exit.ExitCode(), string(output))
		}
		require.Contains(t, string(output), test.message)
	}
}
