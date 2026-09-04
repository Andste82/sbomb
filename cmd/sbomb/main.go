package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/cyclonedx"
)

func main() {
	code, out, errOut := execute(os.Args[1:])
	if out != "" {
		fmt.Print(out)
	}
	if errOut != "" {
		fmt.Fprint(os.Stderr, errOut)
	}
	os.Exit(code)
}

func execute(args []string) (int, string, string) {
	if len(args) == 0 {
		return 0, "sbomb 0.0.0-milestone01\n", ""
	}

	switch args[0] {
	case "version", "--version":
		return 0, "sbomb 0.0.0-milestone01\n", ""
	case "schema":
		return handleSchema(args[1:])
	case "generate":
		return handleGenerate(args[1:])
	default:
		return 1, "", "usage: sbomb [version|generate|schema]\n"
	}
}

func handleSchema(args []string) (int, string, string) {
	return 0, config.Schema() + "\n", ""
}

func handleGenerate(args []string) (int, string, string) {
	buildDir := ""
	output := ""
	repro := false
	cfgPath := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--build-dir":
			if i+1 >= len(args) { return 1, "", "missing value for --build-dir\n" }
			buildDir = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--build-dir="):
			buildDir = strings.TrimPrefix(args[i], "--build-dir=")
		case args[i] == "--output":
			if i+1 >= len(args) { return 1, "", "missing value for --output\n" }
			output = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--output="):
			output = strings.TrimPrefix(args[i], "--output=")
		case args[i] == "--reproducible":
			repro = true
		case args[i] == "--config":
			if i+1 >= len(args) { return 1, "", "missing value for --config\n" }
			cfgPath = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--config="):
			cfgPath = strings.TrimPrefix(args[i], "--config=")
		default:
			if strings.HasPrefix(args[i], "--") {
				return 1, "", fmt.Sprintf("unknown flag: %s\n", args[i])
			}
			return 1, "", fmt.Sprintf("unexpected argument: %s\n", args[i])
		}
	}
	if buildDir == "" {
		return 1, "", "--build-dir is required\n"
	}
	if cfgPath != "" {
		if _, err := config.Load(cfgPath); err != nil {
			return 1, "", err.Error() + "\n"
		}
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return 1, "", err.Error() + "\n"
	}
	if output == "" {
		output = "sbomb.cdx.json"
	}
	if err := cyclonedx.WriteEmpty(output, repro); err != nil {
		return 1, "", err.Error() + "\n"
	}
	return 0, "", ""
}
