package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Port        string `mapstructure:"PORT"`
	PSQLURL     string `mapstructure:"PSQL_URL"`
	PSQLDevURL  string `mapstructure:"PSQL_DEV_URL"`
	PSQLProdURL string `mapstructure:"PSQL_PROD_URL"`
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	for _, k := range []string{"PORT", "PSQL_URL", "PSQL_DEV_URL", "PSQL_PROD_URL"} {
		_ = v.BindEnv(k)
	}

	v.SetDefault("PORT", "8080")

	if err := v.ReadInConfig(); err != nil {
		if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.PSQLURL == "" {
		return nil, fmt.Errorf("PSQL_URL is required")
	}
	return &cfg, nil
}
