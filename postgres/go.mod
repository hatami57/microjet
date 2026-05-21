module github.com/hatami57/microjet/postgres

go 1.26.2

require (
	github.com/hatami57/microjet/types v0.0.0-00010101000000-000000000000
	gorm.io/gorm v1.31.1
)

require (
	github.com/hatami57/microjet/utils v0.0.0-00010101000000-000000000000 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/stretchr/testify v1.8.1 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)

replace (
	github.com/hatami57/microjet/types => ../types
	github.com/hatami57/microjet/utils => ../utils
)
