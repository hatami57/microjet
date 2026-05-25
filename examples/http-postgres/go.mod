module github.com/hatami57/microjet/examples/http-postgres

go 1.26.2

require (
	github.com/gin-gonic/gin v1.12.0
	github.com/hatami57/microjet/core v0.0.0-00010101000000-000000000000
	github.com/hatami57/microjet/host v0.0.0-00010101000000-000000000000
	github.com/hatami57/microjet/http v0.0.0-00010101000000-000000000000
	github.com/hatami57/microjet/postgres v0.0.0-00010101000000-000000000000
)

replace (
	github.com/hatami57/microjet/aws => ../../aws
	github.com/hatami57/microjet/core => ../../core
	github.com/hatami57/microjet/host => ../../host
	github.com/hatami57/microjet/http => ../../http
	github.com/hatami57/microjet/messaging => ../../messaging
	github.com/hatami57/microjet/postgres => ../../postgres
	github.com/hatami57/microjet/types => ../../types
	github.com/hatami57/microjet/utils => ../../utils
)
