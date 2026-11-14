package common

import (
	"flag"
	"fmt"
	"path/filepath"

	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/logger"
)

var (
	Port         = flag.Int("port", 3000, "the listening port")
	Addr         = flag.String("addr", "", "HTTP listen address (default: :3000)")
	PrintVersion = flag.Bool("version", false, "print version and exit")
	PrintHelp    = flag.Bool("help", false, "print help and exit")
	LogDir       = flag.String("log-dir", "./logs", "specify the log directory")
	EnvFile      = flag.String("env", "", "specify the .env file path (supports relative path)")
	EnvFileLong  = flag.String("env-file", "", "dotenv file to load (default: .env)")
	ServeAction  = "start"
)

func printHelp() {
	fmt.Println("One Api Pro " + Version + " - All in one API service for OpenAI API.")
	fmt.Println("Copyright (C) 2026 Leon PanPan. All rights reserved.")
	fmt.Println("GitHub: https://github.com/modelbus/one-api-pro")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  one-api-pro serve [start] [--daemon] [options]")
	fmt.Println("  one-api-pro serve stop [options]")
	fmt.Println("  one-api-pro serve restart [--daemon] [options]")
	fmt.Println("  one-api-pro serve status [options]")
	fmt.Println("  one-api-pro install [client]")
	fmt.Println("")
	fmt.Println("Without --daemon, start and restart run in the foreground. The stop command")
	fmt.Println("controls a server previously started with --daemon.")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  --addr <address>        HTTP listen address (default: :3000)")
	fmt.Println("  --port <port>           the listening port (default: 3000)")
	fmt.Println("  --log-dir <directory>   specify the log directory (default: ./logs)")
	fmt.Println("  --env-file <path>       dotenv file to load (default: .env)")
	fmt.Println("  --env <path>            legacy alias for --env-file")
	fmt.Println("  --daemon                run the server in the background")
	fmt.Println("  --data-dir <path>       daemon state directory (default: ~/.one-api-pro)")
	fmt.Println("  --pid-file <path>       daemon PID file (default: <data-dir>/one-api-pro.pid)")
	fmt.Println("  --log-file <path>       daemon log file (default: <data-dir>/one-api-pro.log)")
	fmt.Println("  --version               print version and exit")
	fmt.Println("  --help                  print help and exit")
	fmt.Println("")
	fmt.Println("Client Setup:")
	fmt.Println("  install [codex]         configure an AI coding client (interactive when omitted)")
}

func Init() {
	loadVersionFromFile()
	if len(os.Args) > 1 && os.Args[1] == "install" {
		if err := RunClientInstall(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	args, action, err := parseServeCommand(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}
	ServeAction = action
	if err := flag.CommandLine.Parse(args); err != nil {
		os.Exit(2)
	}
	if *EnvFileLong != "" {
		*EnvFile = *EnvFileLong
	}
	if absolute, err := filepath.Abs(*DataDir); err == nil {
		*DataDir = absolute
	}
	if *PidFile != "" {
		if absolute, err := filepath.Abs(*PidFile); err == nil {
			*PidFile = absolute
		}
	}
	if *DaemonLogFile != "" {
		if absolute, err := filepath.Abs(*DaemonLogFile); err == nil {
			*DaemonLogFile = absolute
		}
	}

	if *PrintVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	if *PrintHelp {
		printHelp()
		os.Exit(0)
	}

	if *DaemonStop {
		ServeAction = "stop"
	}
	if *DaemonStatus {
		ServeAction = "status"
	}

	// Handle daemon lifecycle commands before initializing the application.
	if ServeAction == "stop" || ServeAction == "status" || ServeAction == "restart" {
		loadEnvFile()
		config.ReloadConfig()
		dm := NewDaemonManager(*PidFile, *DataDir)
		if ServeAction == "stop" || ServeAction == "restart" {
			if err := dm.StopDaemon(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if ServeAction == "stop" {
				os.Exit(0)
			}
		}
		if ServeAction == "status" {
			if err := dm.StatusDaemon(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	loadEnvFile()

	// Re-read config variables that depend on env values loaded from .env.
	// Package-level var initializers run before .env is loaded, so env-based
	// defaults (like MODEL_AUTO_ENABLED) would otherwise stay at their
	// zero values.
	config.ReloadConfig()

	if os.Getenv("SESSION_SECRET") != "" {
		if os.Getenv("SESSION_SECRET") == "random_string" {
			logger.SysError("SESSION_SECRET is set to an example value, please change it to a random string.")
		} else {
			config.SessionSecret = os.Getenv("SESSION_SECRET")
		}
	} else {
		// 只告警，不改默认值语义：SessionSecret 仍为启动时随机 UUID。
		// Warn only; the default (random UUID per startup) is unchanged.
		logger.SysWarn("SESSION_SECRET is not set: a random session secret is generated on every startup, so all user sessions become invalid after a restart. Set SESSION_SECRET to a fixed random string in production (e.g. in .env.secrets).")
	}
	if os.Getenv("SQLITE_PATH") != "" {
		SQLitePath = os.Getenv("SQLITE_PATH")
	}
	if *LogDir != "" {
		var err error
		*LogDir, err = filepath.Abs(*LogDir)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stat(*LogDir); os.IsNotExist(err) {
			err = os.Mkdir(*LogDir, 0777)
			if err != nil {
				log.Fatal(err)
			}
		}
		logger.LogDir = *LogDir
	}
}

func parseServeCommand(arguments []string) ([]string, string, error) {
	if len(arguments) == 0 || arguments[0] != "serve" {
		return arguments, "start", nil
	}
	action, offset := "start", 1
	if len(arguments) > 1 && !strings.HasPrefix(arguments[1], "-") {
		action, offset = arguments[1], 2
	}
	switch action {
	case "start", "stop", "restart", "status":
	default:
		return nil, "", fmt.Errorf("unknown serve command %q (expected start, stop, restart, or status)", action)
	}
	return append([]string(nil), arguments[offset:]...), action, nil
}

func loadEnvFile() {
	var envPath string
	if *EnvFile != "" {
		absPath, err := filepath.Abs(*EnvFile)
		if err != nil {
			log.Fatalf("failed to resolve env file path '%s': %s", *EnvFile, err.Error())
		}
		envPath = absPath
	} else {
		envPath = ".env"
	}

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if *EnvFile != "" {
			log.Fatalf("specified env file not found: %s", envPath)
		}
		return
	}

	if err := godotenv.Load(envPath); err != nil {
		if *EnvFile != "" {
			log.Fatalf("failed to load env file '%s': %s", envPath, err.Error())
		}
		logger.SysError(fmt.Sprintf("failed to load .env file: %s", err.Error()))
	}
}
