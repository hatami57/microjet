package gormx

// Config is the database connection configuration. Individual [database] and
// [database.<name>] sections are loaded by Service.LoadConfig.
type Config struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"sslMode"`
	// LogLevel optionally overrides the SQL-logging level for this database,
	// independent of the global log level. Leave empty to derive it from the
	// host logger (the default). Set it to "warn" or "error" to silence
	// per-query SQL traces while the app runs at debug. Valid values: debug
	// (or info), warn, error, silent.
	LogLevel string `mapstructure:"logLevel"`
}
