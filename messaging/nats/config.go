package nats

// Config is the messaging broker configuration, read from the [messaging]
// section of the application config (with APP_MESSAGING_* env overrides).
type Config struct {
	URL     string `mapstructure:"url"`
	Source  string `mapstructure:"source"`
	Version int    `mapstructure:"version"`
}
