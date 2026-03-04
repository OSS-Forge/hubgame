package platform

import "os"

type Config struct {
	Addr      string
	SQLiteDSN string
	JWTSecret string
	Issuer    string
}

func LoadConfig() Config {
	cfg := Config{
		Addr:      envOr("HUBGAME_ADDR", ":8080"),
		SQLiteDSN: envOr("HUBGAME_SQLITE_DSN", "file:hubgame.db?_pragma=busy_timeout(5000)"),
		JWTSecret: envOr("HUBGAME_JWT_SECRET", "dev-secret-change-me"),
		Issuer:    envOr("HUBGAME_JWT_ISSUER", "hubgame-controller"),
	}
	return cfg
}

func envOr(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
