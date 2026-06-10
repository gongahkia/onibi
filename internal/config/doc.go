// Package config loads config.yaml + optional .env (godotenv) and validates
// against a strict schema. Secrets never come from config.yaml — they come
// from internal/secrets (Keychain). Phase 1.
package config
