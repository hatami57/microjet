package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	App       *AppConfig       `mapstructure:"app"`
	Server    *ServerConfig    `mapstructure:"server"`
	Database  *DatabaseConfig  `mapstructure:"database"`
	Messaging *MessagingConfig `mapstructure:"messaging"`
	AWS       *AWSConfig       `mapstructure:"aws"`
	Log       *LogConfig       `mapstructure:"log"`
	Extra     map[string]any   `mapstructure:"extra"`
}

type MessagingConfig struct {
	URL     string `mapstructure:"url"`
	Source  string `mapstructure:"source"`
	Version int    `mapstructure:"version"`
}

const (
	EnvTest        = "test"
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

type AppConfig struct {
	Namespace   string `mapstructure:"namespace"`
	Environment string `mapstructure:"environment"`
	Name        string `mapstructure:"name"`
	Version     string `mapstructure:"version"`
	Debug       bool   `mapstructure:"debug"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type DatabaseConfig struct {
	Driver   string `mapstructure:"driver"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"sslMode"`
}

type AWSConfig struct {
	AccessKey           string  `mapstructure:"accessKey"`
	SecretKey           string  `mapstructure:"secretKey"`
	Region              string  `mapstructure:"region"`
	EndpointURL         *string `mapstructure:"endpointURL"`
	S3BucketName        *string `mapstructure:"s3BucketName"`
	SQSQueueURL         *string `mapstructure:"sqsQueueURL"`
	DynamoDBEndpointURL *string `mapstructure:"dynamoDBEndpointURL"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type LoaderOption func(any) error

var postLoadHooks []LoaderOption

func RegisterPostLoadHook(hook LoaderOption) {
	postLoadHooks = append(postLoadHooks, hook)
}

func LoadConfig() (*Config, error) {
	config := &Config{}
	return config, Load(config, "")
}

func (a *AppConfig) GetEnvironment() string {
	switch {
	case a.IsTest():
		return EnvTest
	case a.IsDevelopment():
		return EnvDevelopment
	default:
		return EnvProduction
	}
}

func (a *AppConfig) GetName() string    { return a.Name }
func (a *AppConfig) GetVersion() string { return a.Version }
func (a *AppConfig) GetDebug() bool     { return a.Debug }

func (a *AppConfig) IsProduction() bool {
	env := strings.ToLower(a.Environment)
	return env == "production" || env == "prod"
}

func (a *AppConfig) IsDevelopment() bool {
	env := strings.ToLower(a.Environment)
	return env == "development" || env == "dev"
}

func (a *AppConfig) IsTest() bool {
	return strings.ToLower(a.Environment) == "test"
}

func Load(dest any, envPrefix string) error {
	v := viper.New()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exeDir := path.Dir(exePath)
	dirs := []string{cwd, path.Join(cwd, "config"), exeDir, path.Join(exeDir, "config")}
	for _, dir := range dirs {
		v.AddConfigPath(dir)
	}
	v.SetConfigType("toml")

	v.SetDefault("app.namespace", "App")
	v.SetDefault("app.environment", "development")
	v.SetDefault("app.name", "App")
	v.SetDefault("app.version", "0.1.0")
	v.SetDefault("app.debug", true)
	v.SetDefault("server.host", "localhost")
	v.SetDefault("server.port", 8080)

	if envPrefix == "" {
		envPrefix = "APP"
	}
	v.SetEnvPrefix(strings.ToUpper(envPrefix))
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetConfigName("config")
	if err := v.ReadInConfig(); err != nil {
		// A missing config file is not fatal: defaults + env vars are enough to
		// boot. Any other read error (malformed TOML, permissions) is fatal.
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("reading config file: %w", err)
		}
	}

	v.SetConfigName("config.local")
	_ = v.MergeInConfig() // optional overlay; absence is fine

	if err := v.Unmarshal(dest); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	for _, hook := range postLoadHooks {
		if err := hook(dest); err != nil {
			return fmt.Errorf("error running post-load hook: %w", err)
		}
	}

	return nil
}

// Extra config helpers

func (c *Config) GetExtra(key string) (any, bool) {
	val, ok := c.Extra[key]
	return val, ok
}

func (c *Config) MustGetExtra(key string) any {
	val, ok := c.Extra[key]
	if !ok {
		panic(fmt.Sprintf("MustGetExtra: key %q not found", key))
	}
	return val
}

func (c *Config) MustGetExtraString(key string) string   { return MustGetExtraConfig[string](c, key) }
func (c *Config) MustGetExtraInt32(key string) int32     { return MustGetExtraConfig[int32](c, key) }
func (c *Config) MustGetExtraInt64(key string) int64     { return MustGetExtraConfig[int64](c, key) }
func (c *Config) MustGetExtraFloat32(key string) float32 { return MustGetExtraConfig[float32](c, key) }
func (c *Config) MustGetExtraFloat64(key string) float64 { return MustGetExtraConfig[float64](c, key) }
func (c *Config) MustGetExtraBool(key string) bool       { return MustGetExtraConfig[bool](c, key) }

func MustGetExtraConfig[T any](c *Config, key string) T {
	val, ok := c.Extra[key]
	if !ok {
		panic(fmt.Sprintf("MustGetExtraConfig: key %q not found", key))
	}
	result, err := convertTo[T](val)
	if err != nil {
		panic(fmt.Sprintf("MustGetExtra: key %q: %v", key, err))
	}
	return result
}

func GetExtra[T any](c *Config, key string) (T, error) {
	val, ok := c.Extra[key]
	if !ok {
		var zero T
		return zero, fmt.Errorf("key %q not found", key)
	}
	return convertTo[T](val)
}

func convertTo[T any](val any) (T, error) {
	var zero T
	if typed, ok := val.(T); ok {
		return typed, nil
	}
	src := reflect.ValueOf(val)
	dst := reflect.TypeOf(zero)
	isPtr := dst.Kind() == reflect.Pointer
	dstElem := dst
	if isPtr {
		dstElem = dst.Elem()
	}
	converted, err := reflectConvert(src, dstElem)
	if err == nil {
		if isPtr {
			ptr := reflect.New(dstElem)
			ptr.Elem().Set(converted)
			if result, ok := ptr.Interface().(T); ok {
				return result, nil
			}
		} else if result, ok := converted.Interface().(T); ok {
			return result, nil
		}
	}
	return jsonConvert[T](val)
}

func jsonConvert[T any](val any) (T, error) {
	var zero T
	data, err := json.Marshal(val)
	if err != nil {
		return zero, fmt.Errorf("json marshal failed: %w", err)
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return zero, fmt.Errorf("json unmarshal into %T failed: %w", zero, err)
	}
	return result, nil
}

func reflectConvert(src reflect.Value, dst reflect.Type) (reflect.Value, error) {
	for src.Kind() == reflect.Pointer || src.Kind() == reflect.Interface {
		if src.IsNil() {
			return reflect.Zero(dst), nil
		}
		src = src.Elem()
	}
	srcKind := src.Kind()
	dstKind := dst.Kind()
	switch {
	case src.Type().ConvertibleTo(dst):
		return src.Convert(dst), nil
	case dstKind == reflect.String:
		switch srcKind {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return reflect.ValueOf(strconv.FormatInt(src.Int(), 10)).Convert(dst), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return reflect.ValueOf(strconv.FormatUint(src.Uint(), 10)).Convert(dst), nil
		case reflect.Float32, reflect.Float64:
			return reflect.ValueOf(strconv.FormatFloat(src.Float(), 'f', -1, 64)).Convert(dst), nil
		case reflect.Bool:
			return reflect.ValueOf(strconv.FormatBool(src.Bool())).Convert(dst), nil
		}
	case srcKind == reflect.String:
		s := src.String()
		switch {
		case isInt(dstKind):
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(n).Convert(dst), nil
		case isUint(dstKind):
			n, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(n).Convert(dst), nil
		case isFloat(dstKind):
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(n).Convert(dst), nil
		case dstKind == reflect.Bool:
			b, err := strconv.ParseBool(s)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(b).Convert(dst), nil
		}
	}
	return reflect.Value{}, fmt.Errorf("unsupported conversion: %s → %s", src.Type(), dst)
}

func isInt(k reflect.Kind) bool   { return k >= reflect.Int && k <= reflect.Int64 }
func isUint(k reflect.Kind) bool  { return k >= reflect.Uint && k <= reflect.Uint64 }
func isFloat(k reflect.Kind) bool { return k == reflect.Float32 || k == reflect.Float64 }
