module github.com/kartaladev/doger/events/watermill

go 1.27

replace github.com/kartaladev/doger => ../../

require (
	github.com/ThreeDotsLabs/watermill v1.5.3
	github.com/kartaladev/doger v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.12.1
	go.uber.org/goleak v1.3.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/lithammer/shortuuid/v3 v3.0.7 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/samber/go-type-to-string v1.8.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)
