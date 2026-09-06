package exec

import "os"

func defaultExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// pathEnv is the only environment variable a probed program inherits, so that
// its behaviour cannot be steered by the surrounding shell.
func pathEnv() string {
	if value := os.Getenv("PATH"); value != "" {
		return value
	}
	return "/usr/local/bin:/usr/bin:/bin"
}
