package gormx

// Config is the database connection configuration. Individual [database] and
// [database.<name>] sections are loaded by the host's databaseService, not here.
type Config struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"sslMode"`
}