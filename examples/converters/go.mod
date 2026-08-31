module github.com/hatami57/microjet/examples/converters

go 1.26.2

require github.com/hatami57/microjet/core v0.38.0

require (
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/sys v0.45.0 // indirect
)

replace (
	github.com/hatami57/microjet/aws => ../../aws
	github.com/hatami57/microjet/core => ../../core
	github.com/hatami57/microjet/gormx => ../../gormx
	github.com/hatami57/microjet/host => ../../host
	github.com/hatami57/microjet/httpx => ../../httpx
	github.com/hatami57/microjet/messaging => ../../messaging
)
