module github.com/bottlerocket-os/bottlerocket-ecs-updater

go 1.19

require (
	github.com/aws/aws-sdk-go v1.51.20
	github.com/stretchr/testify v1.8.1
)

replace golang.org/x/net => golang.org/x/net v0.8.0

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
