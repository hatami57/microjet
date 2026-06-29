module github.com/hatami57/microjet/examples/minimal

go 1.26.2

require github.com/hatami57/microjet/host v0.24.0

require (
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/hatami57/microjet/core v0.24.0 // indirect
	github.com/pelletier/go-toml/v2 v2.3.0 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/spf13/viper v1.21.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)

replace (
	github.com/hatami57/microjet/aws => ../../aws
	github.com/hatami57/microjet/core => ../../core
	github.com/hatami57/microjet/gormx => ../../gormx
	github.com/hatami57/microjet/host => ../../host
	github.com/hatami57/microjet/httpx => ../../httpx
	github.com/hatami57/microjet/messaging => ../../messaging
)
