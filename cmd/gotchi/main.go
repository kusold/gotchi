package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kusold/gotchi/internal/scaffold"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "init":
		err = runInit(os.Args[2:])
	case "add":
		err = runAdd(os.Args[2:])
	case "doctor":
		err = runDoctor(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
		return
	default:
		err = fmt.Errorf("unknown command: %s", os.Args[1])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runInit(args []string) error {
	var (
		appName    string
		modulePath string
		outputDir  = "."
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "--module="):
			modulePath = strings.TrimPrefix(arg, "--module=")
		case arg == "--module":
			if i+1 >= len(args) {
				return errors.New("missing value for --module")
			}
			modulePath = args[i+1]
			i++
		case strings.HasPrefix(arg, "--output="):
			outputDir = strings.TrimPrefix(arg, "--output=")
		case arg == "--output":
			if i+1 >= len(args) {
				return errors.New("missing value for --output")
			}
			outputDir = args[i+1]
			i++
		case strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unknown init flag: %s", arg)
		default:
			if appName != "" {
				return fmt.Errorf("unexpected extra argument: %s", arg)
			}
			appName = arg
		}
	}

	if appName == "" {
		return errors.New("usage: gotchi init <app-name> [--module <module>] [--output <dir>]")
	}
	mod := strings.TrimSpace(modulePath)
	if mod == "" {
		mod = fmt.Sprintf("github.com/kusold/%s", appName)
	}

	files, err := scaffold.Generate(scaffold.Options{AppName: appName, ModulePath: mod})
	if err != nil {
		return err
	}

	target := filepath.Join(outputDir, appName)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := scaffold.Write(target, files); err != nil {
		return err
	}

	fmt.Println("created", target)
	fmt.Println("next steps:")
	fmt.Println("  1. cd", target)
	fmt.Println("  2. cp .env.sample .env")
	fmt.Println("  3. docker compose up -d db")
	fmt.Println("  4. go run ./cmd/server")
	return nil
}

func runAdd(args []string) error {
	if len(args) < 2 || args[0] != "feature" {
		return errors.New("usage: gotchi add feature <name>")
	}
	feature := strings.ToLower(args[1])
	switch feature {
	case "correlation-audit", "request-correlation", "audit":
		fmt.Println("feature correlation-audit is built-in and enabled by default via gotchi app middleware")
		return nil
	default:
		return fmt.Errorf("unsupported feature %q; supported: correlation-audit", feature)
	}
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	root := fs.String("root", ".", "Project root to inspect")
	if err := fs.Parse(args); err != nil {
		return err
	}

	goModPath := filepath.Join(*root, "go.mod")
	envPath := filepath.Join(*root, ".env.sample")

	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("reading go.mod: %w", err)
	}
	envSample, err := os.ReadFile(envPath)
	if err != nil {
		return fmt.Errorf("reading .env.sample: %w", err)
	}

	checks := []struct {
		name string
		ok   bool
	}{
		{name: "gotchi dependency present", ok: strings.Contains(string(goMod), "github.com/kusold/gotchi")},
		{name: "DATABASE_URL in .env.sample", ok: strings.Contains(string(envSample), "DATABASE_URL")},
		{name: "PORT in .env.sample", ok: strings.Contains(string(envSample), "PORT")},
	}

	failed := 0
	for _, check := range checks {
		status := "OK"
		if !check.ok {
			status = "FAIL"
			failed++
		}
		fmt.Printf("[%s] %s\n", status, check.name)
	}

	if failed > 0 {
		return fmt.Errorf("doctor found %d failing checks", failed)
	}
	return nil
}

func printUsage() {
	fmt.Println("gotchi commands:")
	fmt.Println("  gotchi init <app-name> [--module <module>] [--output <dir>]")
	fmt.Println("  gotchi add feature <name>")
	fmt.Println("  gotchi doctor [--root <project-dir>]")
}
