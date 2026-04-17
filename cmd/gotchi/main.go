package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kusold/gotchi/internal/scaffold"
	"github.com/kusold/gotchi/migrations"
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
	case "migrations":
		err = runMigrations(os.Args[2:])
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
	fmt.Println("  gotchi migrations export [--output <dir>] [--force|--skip] [--component core|auth|all]")
}

func runMigrations(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: gotchi migrations <subcommand>\n\nsubcommands:\n  export  Copy embedded migration SQL files to the local filesystem")
	}
	switch args[0] {
	case "export":
		return runMigrationsExport(args[1:])
	default:
		return fmt.Errorf("unknown migrations subcommand: %s", args[0])
	}
}

func runMigrationsExport(args []string) error {
	flags := flag.NewFlagSet("migrations export", flag.ContinueOnError)
	outputDir := flags.String("output", "migrations/gotchi", "Target directory for exported migration files")
	force := flags.Bool("force", false, "Overwrite existing files without prompting")
	skip := flags.Bool("skip", false, "Skip existing files without prompting")
	component := flags.String("component", "all", "Which migrations to export: core, auth, or all")
	flags.StringVar(outputDir, "o", *outputDir, "Shorthand for --output")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *force && *skip {
		return errors.New("cannot use both --force and --skip")
	}

	switch *component {
	case "core", "auth", "all":
		// valid
	default:
		return fmt.Errorf("invalid --component value %q; must be core, auth, or all", *component)
	}

	type migrationSource struct {
		name string
		fsys fs.FS
	}
	var sources []migrationSource
	if *component == "all" || *component == "core" {
		sources = append(sources, migrationSource{"core", migrations.Core()})
	}
	if *component == "all" || *component == "auth" {
		sources = append(sources, migrationSource{"auth", migrations.Auth()})
	}

	var copied, skipped, unchanged int

	for _, src := range sources {
		entries, err := fs.ReadDir(src.fsys, ".")
		if err != nil {
			return fmt.Errorf("reading embedded %s migrations: %w", src.name, err)
		}

		targetDir := filepath.Join(*outputDir, src.name)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", targetDir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			embeddedContent, err := fs.ReadFile(src.fsys, entry.Name())
			if err != nil {
				return fmt.Errorf("reading embedded file %s/%s: %w", src.name, entry.Name(), err)
			}

			targetPath := filepath.Join(targetDir, entry.Name())

			existingContent, err := os.ReadFile(targetPath)
			if err == nil {
				// File already exists
				if sha256.Sum256(existingContent) == sha256.Sum256(embeddedContent) {
					unchanged++
					continue
				}

				if *skip {
					skipped++
					continue
				}

				if !*force {
					return fmt.Errorf("file %s already exists with different content (use --force to overwrite or --skip to skip)", targetPath)
				}
				// --force: fall through to overwrite
			}

			if err := os.WriteFile(targetPath, embeddedContent, 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", targetPath, err)
			}
			copied++
		}
	}

	fmt.Printf("migrations export complete: %d copied, %d skipped, %d unchanged\n", copied, skipped, unchanged)
	return nil
}
