package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/ssubedir/dk/backend/internal/app"
)

func main() {
	envFile := flag.String("env-file", ".env", "dotenv file to load before starting the server")
	flag.Parse()
	explicitEnvFile := false
	flag.Visit(func(option *flag.Flag) {
		if option.Name == "env-file" {
			explicitEnvFile = true
		}
	})
	if err := loadEnvFile(*envFile, explicitEnvFile); err != nil {
		log.Fatal(err)
	}
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func loadEnvFile(path string, required bool) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("-env-file must not be empty")
	}
	if err := godotenv.Load(path); err != nil {
		if !required && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load %s: %w", path, err)
	}
	log.Printf("config env-file=%q", path)
	return nil
}
