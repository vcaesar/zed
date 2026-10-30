module github.com/vcaesar/zedsearch/dist

go 1.26.9

replace github.com/vcaesar/zedsearch => ../..

require (
	github.com/hashicorp/raft v1.8.0
	github.com/hashicorp/raft-boltdb/v2 v2.4.2
	github.com/stretchr/testify v1.12.1
	github.com/vcaesar/zedsearch v0.0.0-00010101000000-000000000000
	go.etcd.io/bbolt v1.5.0
	go.yaml.in/yaml/v3 v3.0.5
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/goccy/go-json v0.12.0 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-metrics v0.7.0 // indirect
	github.com/hashicorp/go-msgpack/v2 v2.1.5 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261026165753-2ffab32bce7e // indirect
)
