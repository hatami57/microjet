module github.com/hatami57/microjet/examples/logging

go 1.26.2

require github.com/hatami57/microjet/core v0.29.2

replace (
	github.com/hatami57/microjet/aws => ../../aws
	github.com/hatami57/microjet/core => ../../core
	github.com/hatami57/microjet/gormx => ../../gormx
	github.com/hatami57/microjet/host => ../../host
	github.com/hatami57/microjet/httpx => ../../httpx
	github.com/hatami57/microjet/messaging => ../../messaging
)
